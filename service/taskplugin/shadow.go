package taskplugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"sync/atomic"
	"time"

	"eflow.local/new-api/constant"
	"eflow.local/new-api/model"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
)

type shadowBinding struct {
	binding  model.TaskPluginShadowBinding
	handle   *host.Handle
	sequence atomic.Uint64
}

func (m *Manager) buildView(s model.TaskPluginSnapshot) (*runtimeView, error) {
	r, err := m.build(s)
	if err != nil {
		return nil, err
	}
	v := &runtimeView{registry: r, shadow: map[int]*shadowBinding{}, production: map[int]*productionBinding{}}
	if !s.Enabled {
		m.fenceBindings(v, s)
		return v, nil
	}
	if len(s.Bindings) > 64 {
		return nil, errors.New("shadow bindings exceed 64")
	}
	for _, b := range s.Bindings {
		if !b.Enabled {
			continue
		}
		if b.Key != "alibaba" || b.SampleEvery < 1 || b.SampleEvery > 10000 {
			return nil, errors.New("invalid shadow binding")
		}
		// A disabled active override acts as the plugin kill switch even when
		// this observer pins an older version or the embedded factory.
		disabled := false
		for _, p := range s.Plugins {
			if p.Key == b.Key && p.Active && !p.Enabled {
				disabled = true
			}
		}
		if disabled {
			continue
		}
		source := ""
		if b.Factory {
			if !s.FactoryEnabled {
				continue
			}
			for _, k := range s.DisabledFactory {
				if k == b.Key {
					disabled = true
				}
			}
			if disabled {
				continue
			}
			source = b.FactorySource
		} else {
			for _, p := range s.Plugins {
				if p.Key == b.Key && p.Version == b.Version && p.SourceHash == b.SourceHash {
					if p.Enabled {
						source = p.Source
					}
					break
				}
			}
		}
		if source == "" {
			continue
		}
		h, err := host.Compile(source)
		if err != nil {
			return nil, err
		}
		if h.Identity() != (host.Identity{Key: b.Key, Version: b.Version, SourceHash: b.SourceHash}) {
			return nil, errors.New("shadow plugin identity mismatch")
		}
		entry := &shadowBinding{binding: b, handle: h}
		if old := m.view.Load().shadow[b.ChannelID]; old != nil && old.binding == b {
			entry.sequence.Store(old.sequence.Load())
		}
		v.shadow[b.ChannelID] = entry
	}
	for _, b := range s.Production {
		if !b.Enabled {
			continue
		}
		if model.TaskPluginChannelType(b.Key) == 0 {
			return nil, errors.New("invalid production binding")
		}
		source := ""
		if b.Factory {
			source = m.factories[b.Key]
		} else {
			for _, p := range s.Plugins {
				if p.Key == b.Key && p.Version == b.Version && p.SourceHash == b.SourceHash && p.Enabled {
					source = p.Source
					break
				}
			}
		}
		if source == "" {
			continue
		}
		h, err := host.Compile(source)
		if err != nil {
			return nil, err
		}
		if h.Identity() != (host.Identity{Key: b.Key, Version: b.Version, SourceHash: b.SourceHash}) {
			return nil, errors.New("production plugin identity mismatch")
		}
		v.production[b.ChannelID] = &productionBinding{binding: b, handle: h}
	}
	m.fenceBindings(v, s)
	return v, nil
}

type ShadowBindingRequest struct {
	Key         string `json:"key"`
	Version     string `json:"version"`
	Factory     bool   `json:"factory"`
	Enabled     bool   `json:"enabled"`
	SampleEvery uint32 `json:"sample_every"`
}

func (m *Manager) BindShadow(ctx context.Context, channelID int, req ShadowBindingRequest) error {
	if channelID <= 0 || req.Key != "alibaba" || req.Version == "" || req.SampleEvery < 1 || req.SampleEvery > 10000 {
		return errors.New("shadow binding requires channel, alibaba key, version and sample_every between 1 and 10000")
	}
	return m.update(ctx, func(s *model.TaskPluginSnapshot) error {
		b := model.TaskPluginShadowBinding{ChannelID: channelID, Key: req.Key, Version: req.Version, Factory: req.Factory, Enabled: req.Enabled, SampleEvery: req.SampleEvery}
		if req.Factory {
			source, ok := m.factories[req.Key]
			if !ok {
				return ErrNotFound
			}
			h, err := host.Compile(source)
			if err != nil {
				return err
			}
			if h.Identity().Version != req.Version {
				return ErrNotFound
			}
			b.FactorySource = source
			b.SourceHash = h.Identity().SourceHash
		} else {
			for _, p := range s.Plugins {
				if p.Key == req.Key && p.Version == req.Version {
					b.SourceHash = p.SourceHash
					break
				}
			}
			if b.SourceHash == "" {
				return ErrNotFound
			}
		}
		for i, old := range s.Bindings {
			if old.ChannelID == channelID {
				s.Bindings[i] = b
				return nil
			}
		}
		if len(s.Bindings) >= 64 {
			return fmt.Errorf("%w: at most 64 shadow bindings", ErrConflict)
		}
		s.Bindings = append(s.Bindings, b)
		sort.Slice(s.Bindings, func(i, j int) bool { return s.Bindings[i].ChannelID < s.Bindings[j].ChannelID })
		return nil
	})
}
func (m *Manager) UnbindShadow(ctx context.Context, channelID int) error {
	return m.update(ctx, func(s *model.TaskPluginSnapshot) error {
		for i, b := range s.Bindings {
			if b.ChannelID == channelID {
				s.Bindings = append(s.Bindings[:i], s.Bindings[i+1:]...)
				return nil
			}
		}
		return ErrNotFound
	})
}
func (m *Manager) ShadowBindings(ctx context.Context) ([]model.TaskPluginShadowBinding, error) {
	s, err := m.read(ctx)
	if err != nil {
		return nil, err
	}
	return s.Bindings, nil
}

// Ticket is acquired once per real operation and contains no network or quota
// capability. It pins the JS version before the real request is sent.
type Ticket struct {
	manager *Manager
	entry   *shadowBinding
}

func (m *Manager) BeginShadow(channelID, channelType int) *Ticket {
	if channelType != constant.ChannelTypeAli || !m.shadowRunning.Load() {
		return nil
	}
	b := m.view.Load().shadow[channelID]
	if b == nil {
		return nil
	}
	if (b.sequence.Add(1)-1)%uint64(b.binding.SampleEvery) != 0 {
		return nil
	}
	return &Ticket{manager: m, entry: b}
}

const maxObservationBytes = 256 << 10

type observation struct {
	Input         map[string]any `json:"input"`
	Expected      any            `json:"expected"`
	Response      *host.Response `json:"response,omitempty"`
	ExpectedUsage map[string]any `json:"expected_usage,omitempty"`
}
type shadowJob struct {
	ticket  *Ticket
	stage   string
	payload []byte
}
type ShadowReport struct {
	ChannelID    int           `json:"channel_id"`
	Plugin       host.Identity `json:"plugin"`
	Stage        string        `json:"stage"`
	Outcome      string        `json:"outcome"`
	Paths        []string      `json:"paths,omitempty"`
	UsageFields  []string      `json:"usage_fields,omitempty"`
	UsageOutcome string        `json:"usage_outcome,omitempty"`
	UsagePaths   []string      `json:"usage_paths,omitempty"`
	CreatedAt    time.Time     `json:"created_at"`
}

// Observe makes a bounded copy. A full queue drops diagnostic work immediately.
// Expected is a projection of the existing Go result, never the mutable Task.
func (t *Ticket) Observe(stage string, input map[string]any, expected any, response *host.Response) {
	t.ObserveWithUsage(stage, input, expected, response, nil)
}

func (t *Ticket) ObserveWithUsage(stage string, input map[string]any, expected any, response *host.Response, usage map[string]any) {
	if t == nil {
		return
	}
	m := t.manager
	if !m.shadowRunning.Load() || !t.enabled() {
		return
	}
	if stage != "request" && stage != "submit" && stage != "query" {
		return
	}
	data, err := json.Marshal(observation{Input: input, Expected: expected, Response: response, ExpectedUsage: usage})
	if err != nil || len(data) > maxObservationBytes {
		m.shadowDropped.Add(1)
		return
	}
	select {
	case m.shadowQueue <- shadowJob{ticket: t, stage: stage, payload: data}:
	default:
		m.shadowDropped.Add(1)
	}
}
func (t *Ticket) enabled() bool {
	current := t.manager.view.Load().shadow[t.entry.binding.ChannelID]
	return current != nil && current.binding == t.entry.binding
}

func (m *Manager) startShadow(parent context.Context) func() {
	if !m.shadowRunning.CompareAndSwap(false, true) {
		return func() {}
	}
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer m.shadowRunning.Store(false)
		for {
			select {
			case <-ctx.Done():
				for {
					select {
					case <-m.shadowQueue:
					default:
						return
					}
				}
			case job := <-m.shadowQueue:
				if !job.ticket.enabled() {
					continue
				}
				m.recordShadow(m.compareShadow(ctx, job))
			}
		}
	}()
	return func() { cancel(); <-done }
}
func (m *Manager) recordShadow(report ShadowReport) {
	m.shadowReportsMu.Lock()
	defer m.shadowReportsMu.Unlock()
	if len(m.shadowReports) == 100 {
		copy(m.shadowReports, m.shadowReports[1:])
		m.shadowReports = m.shadowReports[:99]
	}
	m.shadowReports = append(m.shadowReports, report)
}
func (m *Manager) ShadowReports() map[string]any {
	m.shadowReportsMu.Lock()
	defer m.shadowReportsMu.Unlock()
	reports := append([]ShadowReport(nil), m.shadowReports...)
	for i := range reports {
		reports[i].Paths = slices.Clone(reports[i].Paths)
		reports[i].UsageFields = slices.Clone(reports[i].UsageFields)
		reports[i].UsagePaths = slices.Clone(reports[i].UsagePaths)
	}
	return map[string]any{"reports": reports, "dropped": m.shadowDropped.Load(), "running": m.shadowRunning.Load(), "capacity": 100}
}
func (m *Manager) compareShadow(parent context.Context, job shadowJob) (report ShadowReport) {
	report = ShadowReport{ChannelID: job.ticket.entry.binding.ChannelID, Plugin: job.ticket.entry.handle.Identity(), Stage: job.stage, Outcome: "hook_failed", CreatedAt: time.Now()}
	// JS error messages and panics can contain user data; never expose them.
	defer func() {
		if recover() != nil {
			report.Outcome = "hook_failed"
			report.Paths = nil
			report.UsageFields = nil
			report.UsageOutcome = ""
			report.UsagePaths = nil
		}
	}()
	ctx, cancel := context.WithTimeout(parent, time.Second)
	defer cancel()
	var obs observation
	if err := json.Unmarshal(job.payload, &obs); err != nil {
		return
	}
	h := job.ticket.entry.handle
	var actual any
	var err error
	switch job.stage {
	case "request":
		var d host.Descriptor
		d, err = h.Build(ctx, "buildSubmitRequest", obs.Input)
		if err == nil {
			actual = map[string]any{"method": d.Method, "url": d.URL, "headers": d.Headers, "body": d.Body}
		}
	case "submit":
		if obs.Response == nil {
			return
		}
		actual, err = h.ParseSubmit(ctx, obs.Input, *obs.Response)
		if obj, ok := actual.(map[string]any); ok {
			actual = map[string]any{"taskId": obj["taskId"]}
		} else if err == nil {
			err = errors.New("invalid submit result")
		}
	case "query":
		if obs.Response == nil {
			return
		}
		actual, err = h.ParseTask(ctx, obs.Input, *obs.Response)
		if obj, ok := actual.(map[string]any); ok {
			actual = map[string]any{"status": obj["status"], "url": emptyString(obj["url"]), "reason": emptyString(obj["reason"])}
		} else if err == nil {
			err = errors.New("invalid query result")
		}
	}
	if err != nil {
		return
	}
	// Canonicalize struct/header maps before comparing JSON field paths.
	raw, err := json.Marshal(actual)
	if err != nil {
		return
	}
	_ = json.Unmarshal(raw, &actual)
	report.Paths = host.DifferencePaths(obs.Expected, actual)
	report.Outcome = "match"
	if len(report.Paths) > 0 {
		report.Outcome = "different"
	}
	modelName, _ := obs.Input["upstreamModel"].(string)
	if modelName == "" {
		modelName, _ = obs.Input["model"].(string)
	}
	var facts map[string]any
	if job.stage == "request" {
		facts, err = h.Usage(ctx, modelName, "extractUsage", obs.Input)
	} else if job.stage == "query" {
		facts, err = h.Usage(ctx, modelName, "extractUsageOnComplete", obs.Input, map[string]any{}, obs.Response.Body)
	}
	if err != nil {
		report.Outcome = "invalid_usage"
		report.UsageOutcome = "invalid"
		return
	}
	if job.stage != "submit" {
		report.UsageOutcome = "not_comparable"
		if obs.ExpectedUsage != nil && facts != nil {
			report.UsagePaths = host.DifferencePaths(obs.ExpectedUsage, facts)
			report.UsageOutcome = "match"
			if len(report.UsagePaths) > 0 {
				report.UsageOutcome = "different"
			}
		}
	}
	for k := range facts { // schema field names also originate in untrusted code.
		switch k {
		case "seconds", "resolution", "tokens", "input_tokens", "output_tokens", "images", "count":
			report.UsageFields = append(report.UsageFields, k)
		default:
			report.UsageFields = append(report.UsageFields, "*")
		}
	}
	sort.Strings(report.UsageFields)
	return
}
func emptyString(v any) any {
	if v == nil {
		return ""
	}
	return v
}
