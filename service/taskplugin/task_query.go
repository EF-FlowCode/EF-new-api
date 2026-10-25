package taskplugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"

	"eflow.local/new-api/constant"
	"eflow.local/new-api/model"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
	"gorm.io/gorm"
)

// TaskStore is the persistence boundary for immutable task identity. The SQL
// implementation shares its writer lock with control-plane deletion.
type TaskStore interface {
	CreatePluginTask(context.Context, model.Task, model.TaskPluginSourceSnapshot) (*model.Task, error)
	ReadPluginTasks(context.Context, []int64) (model.TaskPluginTaskRead, error)
}

var ErrTaskQueryHook = errors.New("task plugin query hook failed")

type TaskVersion struct {
	Key     string
	Version string
	Factory bool
	// SourceHash pins the source actually used by a production submission.
	SourceHash string
}

func (m *Manager) taskStore() (TaskStore, error) {
	s, ok := m.store.(TaskStore)
	if !ok {
		return nil, ErrUnavailable
	}
	return s, nil
}

func taskStoreError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	for _, known := range []error{model.ErrTaskPluginIdentity, model.ErrTaskPluginMissing, model.ErrTaskPluginDisabled, model.ErrTaskPluginReferenced} {
		if errors.Is(err, known) {
			return known
		}
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	return ErrUnavailable
}

// CreatePluginTask persists an internally supplied accepted task and source.
// It does not submit to a provider or reserve/settle money. Production callers
// supply SourceHash to verify the source used by the accepted submission.
func (m *Manager) CreatePluginTask(ctx context.Context, task model.Task, version TaskVersion) (*model.Task, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if task.PrivateData.Plugin != nil || version.Key == "" || version.Version == "" {
		return nil, model.ErrTaskPluginIdentity
	}
	s, err := m.taskStore()
	if err != nil {
		return nil, err
	}
	var source string
	if version.Factory {
		source = m.factories[version.Key]
	} else {
		state, err := m.read(ctx)
		if err != nil {
			return nil, err
		}
		for _, p := range state.Plugins {
			if p.Key == version.Key && p.Version == version.Version {
				source = p.Source
				break
			}
		}
	}
	if source == "" {
		return nil, model.ErrTaskPluginMissing
	}
	h, err := host.Compile(source)
	if err != nil {
		return nil, model.ErrTaskPluginIdentity
	}
	id := h.Identity()
	if id.Key != version.Key || id.Version != version.Version || (version.SourceHash != "" && id.SourceHash != version.SourceHash) {
		return nil, model.ErrTaskPluginIdentity
	}
	task.PrivateData.Plugin = &model.TaskPluginIdentity{IdentityVersion: 1, APIVersion: h.Meta().APIVersion, Key: id.Key, Version: id.Version, SourceHash: id.SourceHash, Factory: version.Factory}
	saved, err := s.CreatePluginTask(ctx, task, model.TaskPluginSourceSnapshot{Key: id.Key, Version: id.Version, SourceHash: id.SourceHash, APIVersion: h.Meta().APIVersion, Source: source})
	return saved, taskStoreError(err)
}

// TaskQueryEnvironment contains only host-prepared transport/auth context.
// Model, upstream ID, action and credential always come from the persisted task.
// These are internal values; never accept this struct from an untrusted request.
type TaskQueryEnvironment struct {
	BaseURL     string
	AuthHeader  string
	Auth        map[string]any
	UserSetting map[string]any
}

type TaskQueryPlan struct {
	LegacyTaskIDs []int64
	Queries       []*TaskQuery
}

type taskQueryMember struct {
	id        int64
	identity  model.TaskPluginIdentity
	channelID int
	userID    int
	input     map[string]any
}

// TaskQuery has no executor or mutable Task references. Descriptor and Parse
// return copies/candidates only; callers cannot commit a task through it.
type TaskQuery struct {
	manager     *Manager
	handle      *host.Handle
	members     []taskQueryMember
	environment TaskQueryEnvironment
	descriptor  host.Descriptor
	batch       bool
}

func queryInput(task model.Task, env TaskQueryEnvironment) map[string]any {
	input := map[string]any{"baseUrl": env.BaseURL, "authHeader": env.AuthHeader, "auth": env.Auth, "userSetting": env.UserSetting, "apiKey": task.PrivateData.Key, "taskId": task.PrivateData.UpstreamTaskID, "publicTaskId": task.TaskID, "action": task.Action, "model": task.Properties.OriginModelName, "upstreamModel": task.Properties.UpstreamModelName}
	// Legacy actions remain unchanged in SQL; only the script input is normalized.
	switch task.Action {
	case constant.TaskActionGenerate:
		input["action"] = "image_to_video"
	case constant.TaskActionTextGenerate:
		input["action"] = "text_to_video"
	case constant.TaskActionFirstTailGenerate:
		input["action"] = "first_tail_to_video"
	case constant.TaskActionRemix:
		input["action"] = "remix"
	}
	if task.PrivateData.PluginContext != nil {
		input["state"] = task.PrivateData.PluginContext.State
	}
	if len(task.Data) > 0 {
		var data any
		if json.Unmarshal(task.Data, &data) == nil {
			input["data"] = data
		}
	}
	return input
}

// PrepareTaskQueries partitions legacy Go tasks and groups batch queries only
// within the same identity, channel, user, credential, action and model.
// Nothing is sent; a failure returns no partially prepared plan.
func (m *Manager) PrepareTaskQueries(ctx context.Context, ids []int64, environments map[int]TaskQueryEnvironment) (*TaskQueryPlan, error) {
	s, err := m.taskStore()
	if err != nil {
		return nil, err
	}
	read, err := s.ReadPluginTasks(ctx, ids)
	if err != nil {
		return nil, taskStoreError(err)
	}
	plan := &TaskQueryPlan{}
	groups := map[string]*TaskQuery{}
	handles := map[string]*host.Handle{}
	for _, row := range read.Tasks {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		task := row.Task
		if task.PrivateData.Plugin == nil {
			plan.LegacyTaskIDs = append(plan.LegacyTaskIDs, task.ID)
			continue
		}
		identity := *task.PrivateData.Plugin
		if !row.Source.Matches(identity) || task.PrivateData.UpstreamTaskID == "" {
			return nil, model.ErrTaskPluginIdentity
		}
		if err := model.CheckTaskPluginEnabled(read.Control, identity); err != nil {
			return nil, err
		}
		env, exists := environments[task.ChannelId]
		if !exists {
			return nil, fmt.Errorf("query environment is missing for channel %d", task.ChannelId)
		}
		// Bound and detach caller-provided auth maps before storing them.
		raw, err := json.Marshal(env)
		if err != nil || len(raw) > 64<<10 {
			return nil, model.ErrTaskPluginIdentity
		}
		var detached TaskQueryEnvironment
		if err := json.Unmarshal(raw, &detached); err != nil {
			return nil, model.ErrTaskPluginIdentity
		}
		env = detached
		h := handles[identity.SourceHash]
		if h == nil {
			h, err = host.Compile(row.Source.Source)
			if err != nil || h.Identity() != (host.Identity{Key: identity.Key, Version: identity.Version, SourceHash: identity.SourceHash}) || h.Meta().APIVersion != identity.APIVersion {
				return nil, model.ErrTaskPluginIdentity
			}
			handles[identity.SourceHash] = h
		}
		batch := h.Meta().FetchMode == "batch"
		input := queryInput(task, env)
		groupBytes, _ := json.Marshal([]any{identity, task.ChannelId, task.UserId, task.PrivateData.Key, task.Action, task.Properties.OriginModelName, task.Properties.UpstreamModelName})
		group := string(groupBytes)
		q := groups[group]
		if !batch || q == nil {
			q = &TaskQuery{manager: m, handle: h, environment: env, batch: batch}
			plan.Queries = append(plan.Queries, q)
			groups[group] = q
		}
		for _, member := range q.members {
			if member.input["taskId"] == input["taskId"] {
				return nil, model.ErrTaskPluginIdentity
			}
		}
		q.members = append(q.members, taskQueryMember{id: task.ID, identity: identity, channelID: task.ChannelId, userID: task.UserId, input: input})
	}
	for _, q := range plan.Queries {
		var err error
		if q.batch {
			tasks := make([]any, len(q.members))
			for i, member := range q.members {
				tasks[i] = member.input
			}
			q.descriptor, err = q.handle.Build(ctx, "buildBatchQueryRequest", q.members[0].input, tasks)
		} else {
			q.descriptor, err = q.handle.Build(ctx, "buildQueryRequest", q.members[0].input)
		}
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, ErrTaskQueryHook
		}
	}
	return plan, nil
}

func (q *TaskQuery) TaskIDs() []int64 {
	ids := make([]int64, len(q.members))
	for i, member := range q.members {
		ids[i] = member.id
	}
	return ids
}

func (q *TaskQuery) Descriptor() host.Descriptor {
	raw, _ := json.Marshal(q.descriptor)
	var copy host.Descriptor
	_ = json.Unmarshal(raw, &copy)
	return copy
}

func (q *TaskQuery) OpenContent(ctx context.Context, executor host.Executor, artifactKey ...string) (*http.Response, error) {
	if len(q.members) != 1 {
		return nil, ErrTaskQueryHook
	}
	if err := q.validateCurrent(ctx, true); err != nil {
		return nil, err
	}
	input := cloneJSONMap(q.members[0].input)
	input["artifactKey"] = "video"
	if len(artifactKey) > 0 {
		input["artifactKey"] = artifactKey[0]
	}
	input["upstreamTaskId"] = input["taskId"]
	input["clientRequest"] = map[string]any{"method": "GET"}
	descriptor, err := q.handle.Build(ctx, "buildContentRequest", input)
	if err != nil {
		return nil, err
	}
	if descriptor.Method != "GET" {
		return nil, ErrTaskQueryHook
	}
	return executor.Open(ctx, q.handle, q.environment.BaseURL, descriptor, nil)
}

// ValidateContent also protects locally stored artifacts, which require no HTTP.
func (q *TaskQuery) ValidateContent(ctx context.Context) error {
	return q.validateCurrent(ctx, true)
}

func (q *TaskQuery) Execute(ctx context.Context, executor host.Executor, base string, files map[string]host.File) (host.Response, error) {
	if q == nil || q.handle == nil || len(q.members) == 0 {
		return host.Response{}, ErrTaskQueryHook
	}
	if err := q.validateCurrent(ctx, true); err != nil {
		return host.Response{}, err
	}
	return executor.Execute(ctx, q.handle, base, q.descriptor, files)
}

type TaskQueryCandidate struct {
	TaskID                 int64
	Identity               model.TaskPluginIdentity
	Result                 map[string]any
	CompletionUsage        map[string]any
	CompletionUsageInvalid bool
}

// Parse rechecks persisted identity and current database switches before JS.
// An unknown batch ID never becomes a candidate for another user's task.
func (q *TaskQuery) validateCurrent(ctx context.Context, production bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s, err := q.manager.taskStore()
	if err != nil {
		return err
	}
	read, err := s.ReadPluginTasks(ctx, q.TaskIDs())
	if err != nil {
		return taskStoreError(err)
	}
	if len(read.Tasks) != len(q.members) {
		return model.ErrTaskPluginIdentity
	}
	for i, row := range read.Tasks {
		task, member := row.Task, q.members[i]
		if task.ID != member.id || task.PrivateData.Plugin == nil || *task.PrivateData.Plugin != member.identity || task.ChannelId != member.channelID || task.UserId != member.userID || !reflect.DeepEqual(queryInput(task, q.environment), member.input) {
			return model.ErrTaskPluginIdentity
		}
		if err := model.CheckTaskPluginEnabled(read.Control, member.identity); err != nil {
			return err
		}
		if production {
			binding, found := productionBindingFor(read.Control, task.ChannelId)
			// Rebinding a channel affects new tasks only. Existing tasks keep
			// their own source/version while the channel's switch stays on.
			if !found || !binding.Enabled || binding.Key != member.identity.Key {
				return model.ErrTaskPluginDisabled
			}
		}
	}
	return nil
}

func (q *TaskQuery) Parse(ctx context.Context, response host.Response) ([]TaskQueryCandidate, error) {
	return q.parse(ctx, response, false)
}

func (q *TaskQuery) ParseProduction(ctx context.Context, response host.Response) ([]TaskQueryCandidate, error) {
	return q.parse(ctx, response, true)
}

func (q *TaskQuery) parse(ctx context.Context, response host.Response, production bool) ([]TaskQueryCandidate, error) {
	if err := q.validateCurrent(ctx, production); err != nil {
		return nil, err
	}
	var err error
	var rows []map[string]any
	if q.batch {
		rows, err = q.handle.ParseBatch(ctx, q.members[0].input, response)
	} else {
		var result map[string]any
		result, err = q.handle.ParseTask(ctx, q.members[0].input, response)
		rows = []map[string]any{result}
	}
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrTaskQueryHook
	}
	var results []TaskQueryCandidate
	for _, row := range rows {
		member := q.members[0]
		if q.batch {
			found := false
			for _, candidate := range q.members {
				if candidate.input["taskId"] == row["taskId"] {
					member = candidate
					found = true
					break
				}
			}
			if !found {
				return nil, ErrTaskQueryHook
			}
		}
		candidate := TaskQueryCandidate{TaskID: member.id, Identity: member.identity, Result: row}
		if row["status"] == "SUCCESS" {
			// Preserve the observed success even when its usage is missing or
			// malformed. The host decides whether that contract can settle.
			body := response.Body
			if q.batch {
				// Batch results expose the payload belonging to this member.
				body = row["data"]
			}
			candidate.CompletionUsage, err = q.handle.Usage(ctx, stringValue(member.input["upstreamModel"]), "extractUsageOnComplete", member.input, row, body)
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if err != nil {
				candidate.CompletionUsage = nil
				candidate.CompletionUsageInvalid = true
			}
		}
		results = append(results, candidate)
	}
	return results, nil
}
