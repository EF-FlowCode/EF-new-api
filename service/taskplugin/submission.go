package taskplugin

import (
	"context"
	"errors"

	"eflow.local/new-api/model"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
)

// ProbeRecovery checks a supplied ID with the pinned plugin and original
// credential. Existence does not establish request ownership; Root still must
// attest the association in the review evidence.
func (m *Manager) ProbeRecovery(ctx context.Context, id int64, upstream string, env TaskQueryEnvironment, executor host.Executor) error {
	h, task, err := m.StoredSubmitHandle(ctx, id)
	if err != nil {
		return err
	}
	task.PrivateData.UpstreamTaskID = upstream
	input := queryInput(*task, env)
	hook := "buildQueryRequest"
	var extra []any
	if h.Meta().FetchMode == "batch" {
		hook = "buildBatchQueryRequest"
		extra = []any{[]any{input}}
	}
	descriptor, err := h.Build(ctx, hook, input, extra...)
	if err != nil {
		return err
	}
	response, err := executor.Execute(ctx, h, env.BaseURL, descriptor, nil)
	if err != nil {
		return err
	}
	if response.Status < 200 || response.Status >= 300 {
		return errors.New("cannot verify recovered provider task")
	}
	var rows []map[string]any
	if h.Meta().FetchMode == "batch" {
		rows, err = h.ParseBatch(ctx, input, response)
	} else {
		var row map[string]any
		row, err = h.ParseTask(ctx, input, response)
		rows = []map[string]any{row}
	}
	if err != nil {
		return err
	}
	if len(rows) != 1 {
		return errors.New("recovered task identity mismatch")
	}
	if returned, _ := rows[0]["taskId"].(string); returned != "" && returned != upstream {
		return errors.New("recovered task identity mismatch")
	}
	status, _ := rows[0]["status"].(string)
	if status == "" || status == "UNKNOWN" {
		return errors.New("provider has not confirmed recovered task")
	}
	return nil
}

type submissionStore interface {
	BeginPluginSubmission(context.Context, model.Task, model.TaskPluginSourceSnapshot, model.TaskPluginSubmission) (*model.Task, bool, error)
}

func (m *Manager) BeginSubmission(ctx context.Context, task model.Task, p *ProductionPrepared, sub model.TaskPluginSubmission) (*model.Task, bool, error) {
	store, ok := m.store.(submissionStore)
	if !ok {
		return nil, false, ErrUnavailable
	}
	source := ""
	if p.Binding.Factory {
		source = m.factories[p.Identity.Key]
	} else {
		s, err := m.read(ctx)
		if err != nil {
			return nil, false, err
		}
		for _, v := range s.Plugins {
			if v.Key == p.Identity.Key && v.Version == p.Identity.Version {
				source = v.Source
				break
			}
		}
	}
	if sourceHash(source) != p.Identity.SourceHash {
		return nil, false, model.ErrTaskPluginIdentity
	}
	task.PrivateData.Plugin = &model.TaskPluginIdentity{IdentityVersion: 1, APIVersion: p.Handle.Meta().APIVersion, Key: p.Identity.Key, Version: p.Identity.Version, SourceHash: p.Identity.SourceHash, Factory: p.Binding.Factory}
	return store.BeginPluginSubmission(ctx, task, model.TaskPluginSourceSnapshot{Source: source, Key: p.Identity.Key, Version: p.Identity.Version, SourceHash: p.Identity.SourceHash, APIVersion: p.Handle.Meta().APIVersion}, sub)
}

// StoredSubmitHandle uses the task's durable source rather than today's factory.
func (m *Manager) StoredSubmitHandle(ctx context.Context, id int64) (*host.Handle, *model.Task, error) {
	store, err := m.taskStore()
	if err != nil {
		return nil, nil, err
	}
	read, err := store.ReadPluginTasks(ctx, []int64{id})
	if err != nil {
		return nil, nil, err
	}
	if len(read.Tasks) != 1 || read.Tasks[0].Task.PrivateData.Plugin == nil {
		return nil, nil, model.ErrTaskPluginIdentity
	}
	row := read.Tasks[0]
	if err := model.CheckPluginTaskEnabled(read.Control, &row.Task); err != nil {
		return nil, nil, err
	}
	h, err := host.Compile(row.Source.Source)
	return h, &row.Task, err
}

func (p *ProductionPrepared) ValidateExecution(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s, err := p.manager.read(ctx)
	if err != nil {
		return err
	}
	b, found := productionBindingFor(s, p.Binding.ChannelID)
	if !found || b != p.Binding {
		return model.ErrTaskPluginDisabled
	}
	return productionEnabled(s, b)
}
