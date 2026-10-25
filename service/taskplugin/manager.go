// Package taskplugin owns the task plugin control plane. It does not dispatch
// provider requests or perform billing. MySQL is authoritative; a node publishes
// an immutable runtime view only after its transaction commits successfully.
package taskplugin

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"maps"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"eflow.local/new-api/model"
	"eflow.local/new-api/pkg/jsplugin"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
)

const MaxSourceBytes = 1 << 20

var (
	ErrNotFound    = errors.New("task plugin not found")
	ErrConflict    = errors.New("task plugin conflict")
	ErrUnavailable = errors.New("task plugin storage unavailable")
)

type Store interface {
	Read(context.Context) (model.TaskPluginSnapshot, error)
	Update(context.Context, func(*model.TaskPluginSnapshot) error) (model.TaskPluginSnapshot, error)
}

type runtimeView struct {
	registry   *jsplugin.Registry
	revision   uint64
	appliedAt  time.Time
	shadow     map[int]*shadowBinding
	production map[int]*productionBinding
	// loaded is false only for the placeholder view before any snapshot.
	loaded bool
	// fenced holds snapshot bindings that do not route (paused, master switch
	// off, source disabled), precompiled so public requests never compile.
	fenced map[int]*fencedBinding
}

type productionBinding struct {
	binding model.TaskPluginProductionBinding
	handle  *host.Handle
}

// fencedBinding keeps a non-routable binding's declared models off the
// ordinary relay. An unusable fixed source fences every advertised name.
type fencedBinding struct {
	models   []string
	unusable bool
}

type factoryDeclaration struct {
	models []string
	err    error
}

type Manager struct {
	store            Store
	factories        map[string]string
	factoryOnce      sync.Once
	factoryDecl      map[string]factoryDeclaration
	mu               sync.Mutex
	view             atomic.Pointer[runtimeView]
	lastError        string
	lastAttempt      time.Time
	observedRevision uint64
	dryrunSlots      chan struct{}
	shadowQueue      chan shadowJob
	shadowReportsMu  sync.Mutex
	shadowReports    []ShadowReport
	shadowDropped    atomic.Uint64
	shadowRunning    atomic.Bool
}

func NewManager(store Store, factories map[string]string) *Manager {
	m := &Manager{store: store, factories: make(map[string]string), dryrunSlots: make(chan struct{}, 4), shadowQueue: make(chan shadowJob, 16)}
	maps.Copy(m.factories, factories)
	r := jsplugin.NewRegistry()
	r.SetEnabled(false)
	m.view.Store(&runtimeView{registry: r})
	return m
}

func sourceHash(source string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(source))) }

// build works on a private registry: its intermediate generations are never
// visible to callers. Turning the master switch off always works, including
// when a stored script has become invalid after an upgrade.
func (m *Manager) build(s model.TaskPluginSnapshot) (*jsplugin.Registry, error) {
	r := jsplugin.NewRegistry()
	if !s.Enabled {
		r.SetEnabled(false)
		return r, nil
	}
	disabled := append([]string(nil), s.DisabledFactory...)
	for key := range m.factories {
		if !s.FactoryEnabled {
			disabled = append(disabled, key)
		}
	}
	for _, row := range s.Plugins {
		if row.Active && !row.Enabled {
			disabled = append(disabled, row.Key)
		}
	}
	r.SetDisabledFactoryKeys(disabled)
	keys := make([]string, 0, len(m.factories))
	for key := range m.factories {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if _, err := r.RegisterFactory(m.factories[key], jsplugin.Options{Key: key}); err != nil {
			return nil, err
		}
	}
	var overrides []*jsplugin.LoadedPlugin
	active := make(map[string]bool)
	for _, row := range s.Plugins {
		if !row.Active {
			continue
		}
		if active[row.Key] {
			return nil, fmt.Errorf("multiple active versions for %s", row.Key)
		}
		active[row.Key] = true
		if !row.Enabled {
			continue
		}
		if sourceHash(row.Source) != row.SourceHash {
			return nil, fmt.Errorf("source hash mismatch for %s@%s", row.Key, row.Version)
		}
		p, err := jsplugin.CompilePlugin(row.Source, jsplugin.Options{Key: row.Key, Version: row.Version})
		if err != nil {
			return nil, err
		}
		if p.Meta.Key != row.Key || p.Meta.Version != row.Version || p.Meta.APIVersion != row.APIVersion {
			return nil, fmt.Errorf("stored metadata mismatch for %s", row.Key)
		}
		overrides = append(overrides, p)
	}
	if err := r.ReplaceOverrides(overrides); err != nil {
		return nil, err
	}
	if err := r.LastRebuildError(); err != "" {
		return nil, errors.New(err)
	}
	if conflicts := r.RoutingErrors(); len(conflicts) > 0 {
		return nil, fmt.Errorf("routing conflicts: %v", conflicts)
	}
	return r, nil
}

func (m *Manager) publish(v *runtimeView, revision uint64) {
	v.revision = revision
	v.appliedAt = time.Now()
	m.view.Store(v)
	model.InvalidatePricingCache()
	m.lastError = ""
}

func (m *Manager) Refresh(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastAttempt = time.Now()
	s, err := m.read(ctx)
	if err == nil {
		m.observedRevision = s.Revision
		if s.Revision == m.view.Load().revision {
			m.lastError = ""
			return nil
		}
		var r *runtimeView
		r, err = m.buildView(s)
		if err == nil {
			m.publish(r, s.Revision)
		}
	}
	if err != nil {
		m.lastError = err.Error()
	}
	return err
}

func (m *Manager) update(ctx context.Context, change func(*model.TaskPluginSnapshot) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var candidate *runtimeView
	var validationErr error
	s, err := m.store.Update(ctx, func(s *model.TaskPluginSnapshot) error {
		if err := change(s); err != nil {
			validationErr = err
			return err
		}
		var err error
		candidate, err = m.buildView(*s)
		if err != nil {
			validationErr = fmt.Errorf("%w: %v", ErrConflict, err)
			return validationErr
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, model.ErrTaskPluginReferenced) {
			return fmt.Errorf("%w: %s", ErrConflict, model.ErrTaskPluginReferenced)
		}
		if validationErr == nil {
			return fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
		return err
	}
	m.observedRevision = s.Revision
	m.publish(candidate, s.Revision)
	return nil
}

// Start performs a bounded initial refresh, then polls until stopped. The
// returned stop function waits for the poller before the caller closes its DB.
func (m *Manager) Start(parent context.Context, interval time.Duration, report func(error)) func() {
	ctx, cancel := context.WithCancel(parent)
	stopShadow := m.startShadow(ctx)
	refresh := func() {
		c, stop := context.WithTimeout(ctx, 15*time.Second)
		defer stop()
		if err := m.Refresh(c); err != nil && report != nil {
			report(err)
		}
	}
	refresh()
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				refresh()
			}
		}
	}()
	return func() { cancel(); <-done; stopShadow() }
}

type Status struct {
	AppliedRevision       uint64    `json:"applied_revision"`
	ObservedRevision      uint64    `json:"observed_revision"`
	Enabled               bool      `json:"enabled"`
	AppliedAt             time.Time `json:"applied_at"`
	LastAttempt           time.Time `json:"last_attempt"`
	LastError             string    `json:"last_error,omitempty"`
	TaskDispatchConnected bool      `json:"task_dispatch_connected"`
}

func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	v := m.view.Load()
	return Status{AppliedRevision: v.revision, ObservedRevision: m.observedRevision, Enabled: v.registry.Enabled(), AppliedAt: v.appliedAt, LastAttempt: m.lastAttempt, LastError: m.lastError, TaskDispatchConnected: true}
}

type UploadRequest struct {
	Source       string `json:"source"`
	Enabled      *bool  `json:"enabled"`
	Remark       string `json:"remark"`
	Force        bool   `json:"force"`
	SourceSha256 string `json:"sourceSha256"`
	Icon         string `json:"icon"`
}

func (m *Manager) Upload(ctx context.Context, req UploadRequest) (jsplugin.Meta, error) {
	if req.Force {
		return jsplugin.Meta{}, fmt.Errorf("force is not supported; upload disabled and resolve routing conflicts before activation")
	}
	if len(req.Source) == 0 || len(req.Source) > MaxSourceBytes {
		return jsplugin.Meta{}, fmt.Errorf("plugin source must contain 1 to %d bytes", MaxSourceBytes)
	}
	if len(req.Remark) > 8192 {
		return jsplugin.Meta{}, fmt.Errorf("remark exceeds 8192 bytes")
	}
	hash := sourceHash(req.Source)
	if req.SourceSha256 != "" && !strings.EqualFold(req.SourceSha256, hash) {
		return jsplugin.Meta{}, fmt.Errorf("sourceSha256 mismatch")
	}
	if req.Icon != "" {
		if _, _, err := jsplugin.DecodeIconDataURI(req.Icon); err != nil {
			return jsplugin.Meta{}, err
		}
	}
	p, err := jsplugin.CompilePlugin(req.Source, jsplugin.Options{})
	if err != nil {
		return jsplugin.Meta{}, err
	}
	if len(p.Meta.Version) > 64 {
		return jsplugin.Meta{}, fmt.Errorf("plugin version exceeds 64 bytes")
	}
	enabled := req.Enabled == nil || *req.Enabled
	err = m.update(ctx, func(s *model.TaskPluginSnapshot) error {
		hasActive := false
		for i := range s.Plugins {
			row := &s.Plugins[i]
			if row.Key != p.Meta.Key {
				continue
			}
			hasActive = hasActive || row.Active
			if row.Version == p.Meta.Version {
				if row.SourceHash != hash || row.Source != req.Source {
					return fmt.Errorf("%w: same version has different source", ErrConflict)
				}
				if req.Enabled != nil {
					row.Enabled = enabled
				}
				row.Remark = req.Remark
				if req.Icon != "" {
					row.Icon = req.Icon
				}
				return nil
			}
		}
		s.Plugins = append(s.Plugins, model.TaskPlugin{Key: p.Meta.Key, APIVersion: p.Meta.APIVersion, Version: p.Meta.Version, Source: req.Source, SourceHash: hash, Enabled: enabled, Active: !hasActive, Remark: req.Remark, Icon: req.Icon})
		return nil
	})
	return p.Meta, err
}

func (m *Manager) Activate(ctx context.Context, key, version string) error {
	return m.update(ctx, func(s *model.TaskPluginSnapshot) error {
		found := false
		for i := range s.Plugins {
			row := &s.Plugins[i]
			if row.Key == key {
				row.Active = row.Version == version
				if row.Active {
					row.Enabled = true
					found = true
				}
			}
		}
		if !found {
			return ErrNotFound
		}
		return nil
	})
}

func (m *Manager) SetStatus(ctx context.Context, key string, enabled bool) error {
	return m.update(ctx, func(s *model.TaskPluginSnapshot) error {
		for i := range s.Plugins {
			row := &s.Plugins[i]
			if row.Key == key && row.Active {
				row.Enabled = enabled
				return nil
			}
		}
		if _, ok := m.factories[key]; !ok {
			return ErrNotFound
		}
		keys := make([]string, 0, len(s.DisabledFactory)+1)
		for _, k := range s.DisabledFactory {
			if k != key {
				keys = append(keys, k)
			}
		}
		if !enabled {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		s.DisabledFactory = keys
		return nil
	})
}

type Settings struct {
	Enabled        *bool `json:"enabled"`
	FactoryEnabled *bool `json:"factory_enabled"`
}

func (m *Manager) Configure(ctx context.Context, settings Settings) error {
	if settings.Enabled == nil && settings.FactoryEnabled == nil {
		return fmt.Errorf("enabled or factory_enabled is required")
	}
	return m.update(ctx, func(s *model.TaskPluginSnapshot) error {
		if settings.Enabled != nil {
			s.Enabled = *settings.Enabled
		}
		if settings.FactoryEnabled != nil {
			s.FactoryEnabled = *settings.FactoryEnabled
		}
		return nil
	})
}

func (m *Manager) Delete(ctx context.Context, key, version string) error {
	return m.update(ctx, func(s *model.TaskPluginSnapshot) error {
		for _, b := range s.Production {
			if !b.Factory && b.Key == key && b.Version == version {
				return fmt.Errorf("%w: version is referenced by a production binding", ErrConflict)
			}
		}
		for _, b := range s.Bindings {
			if !b.Factory && b.Key == key && b.Version == version {
				return fmt.Errorf("%w: version is referenced by a shadow binding", ErrConflict)
			}
		}
		index := -1
		for i, row := range s.Plugins {
			if row.Key == key && row.Version == version {
				index = i
				break
			}
		}
		if index < 0 {
			return ErrNotFound
		}
		active := s.Plugins[index].Active
		s.Plugins = append(s.Plugins[:index], s.Plugins[index+1:]...)
		if active {
			latest := -1
			for i, row := range s.Plugins {
				if row.Key == key && (latest < 0 || row.Id > s.Plugins[latest].Id) {
					latest = i
				}
			}
			if latest >= 0 {
				s.Plugins[latest].Active = true
			}
		}
		return nil
	})
}

func (m *Manager) Settings(ctx context.Context) (model.TaskPluginSnapshot, error) {
	return m.read(ctx)
}

func (m *Manager) read(ctx context.Context) (model.TaskPluginSnapshot, error) {
	s, err := m.store.Read(ctx)
	if err != nil {
		return model.TaskPluginSnapshot{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return s, nil
}

func (m *Manager) Versions(ctx context.Context, key string) ([]model.TaskPlugin, error) {
	s, err := m.read(ctx)
	if err != nil {
		return nil, err
	}
	rows := make([]model.TaskPlugin, 0)
	for i := len(s.Plugins) - 1; i >= 0; i-- {
		if s.Plugins[i].Key == key {
			rows = append(rows, s.Plugins[i])
		}
	}
	return rows, nil
}

func (m *Manager) Detail(ctx context.Context, key, version string) (model.TaskPlugin, error) {
	rows, err := m.Versions(ctx, key)
	if err != nil {
		return model.TaskPlugin{}, err
	}
	for _, row := range rows {
		if (version == "" && row.Active) || (version != "" && row.Version == version) {
			return row, nil
		}
	}
	source, ok := m.factories[key]
	if !ok || version != "" {
		return model.TaskPlugin{}, ErrNotFound
	}
	p, err := jsplugin.CompilePlugin(source, jsplugin.Options{Key: key})
	if err != nil {
		return model.TaskPlugin{}, err
	}
	return model.TaskPlugin{Key: key, Version: p.Meta.Version, APIVersion: p.Meta.APIVersion, Source: source, SourceHash: sourceHash(source), Enabled: true, Active: true}, nil
}

type Catalog struct {
	Settings  model.TaskPluginSnapshot `json:"settings"`
	Overrides []model.TaskPlugin       `json:"overrides"`
	Factory   []jsplugin.Meta          `json:"factory"`
}

func (m *Manager) List(ctx context.Context) (Catalog, error) {
	s, err := m.read(ctx)
	if err != nil {
		return Catalog{}, err
	}
	catalog := Catalog{Settings: s, Overrides: s.Plugins, Factory: make([]jsplugin.Meta, 0, len(m.factories))}
	for key, source := range m.factories {
		p, err := jsplugin.CompilePlugin(source, jsplugin.Options{Key: key})
		if err != nil {
			return Catalog{}, err
		}
		catalog.Factory = append(catalog.Factory, p.Meta)
	}
	sort.Slice(catalog.Factory, func(i, j int) bool { return catalog.Factory[i].Key < catalog.Factory[j].Key })
	return catalog, nil
}

func (m *Manager) DryRun(ctx context.Context, key, version, hook, member string, args []any) (any, error) {
	if hook == "" || len(args) > 16 {
		return nil, fmt.Errorf("hook is required and at most 16 arguments are allowed")
	}
	select {
	case m.dryrunSlots <- struct{}{}:
		defer func() { <-m.dryrunSlots }()
	default:
		return nil, fmt.Errorf("%w: too many concurrent dry runs", ErrConflict)
	}
	row, err := m.Detail(ctx, key, version)
	if err != nil {
		return nil, err
	}
	p, err := jsplugin.CompilePlugin(row.Source, jsplugin.Options{Timeout: time.Second, Concurrency: 1})
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if member == "" {
		return p.Engine.Call(ctx, hook, args...)
	}
	return p.Engine.CallMember(ctx, hook, member, args...)
}
