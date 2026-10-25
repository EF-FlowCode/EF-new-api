package taskplugin

import (
	"context"
	"errors"
	"slices"
	"strings"

	"eflow.local/new-api/common"
	"eflow.local/new-api/model"
	"eflow.local/new-api/pkg/jsplugin"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
)

const PublicRouteContextKey = "task_plugin_public_route"

// PublicRoute is a request-local alias view. It is derived afresh from channel
// metadata and fixed production handles, never the global active registry.
// The client model remains the permission, billing and response identity.
type PublicRoute struct {
	Model        string
	RoutingModel string
	Protocol     string
	Targets      map[int]PublicTarget
	Native       *NativeRequest
}

type PublicTarget struct {
	Binding model.TaskPluginProductionBinding
	Model   string
	Handle  *host.Handle
}

func ProtocolForPath(path string) string {
	switch path {
	case "/v1/responses":
		return "openai_responses"
	case "/v1/videos":
		return "openai_video"
	}
	return ""
}

func SupportsProtocol(meta jsplugin.Meta, name, protocol, mode string) bool {
	for _, claim := range meta.Protocols {
		if claim.Name != protocol || len(claim.Models) > 0 && !slices.Contains(claim.Models, name) {
			continue
		}
		return mode == "" || slices.Contains(claim.Supports, mode)
	}
	return false
}

// compileHandle and listPublicChannels are test seams for the request path.
var (
	compileHandle      = host.Compile
	listPublicChannels = publicChannels
)

// publicChannels lists enabled channels advertising name (ASCII-folded) from
// the channel memory cache; the database is read only when the cache is off.
func publicChannels(ctx context.Context, name string) ([]model.Channel, error) {
	folded := jsplugin.ASCIIFold(name)
	advertises := func(ch *model.Channel) bool {
		for advertised := range strings.SplitSeq(ch.Models, ",") {
			if jsplugin.ASCIIFold(advertised) == folded {
				return true
			}
		}
		return false
	}
	if channels, ok := model.CacheSelectEnabledChannels(advertises); ok {
		return channels, nil
	}
	channels, err := taskEntryChannels(ctx)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(channels, func(ch model.Channel) bool { return !advertises(&ch) }), nil
}

func (m *Manager) ResolvePublicRoute(ctx context.Context, name, path string) (*PublicRoute, error) {
	if name == "" || ProtocolForPath(path) == "" {
		return nil, nil
	}
	channels, err := listPublicChannels(ctx, name)
	if err != nil {
		// Storage trouble only fences names a task plugin declares itself;
		// ordinary models keep the token relay available.
		if m.declaresModel(name) {
			return nil, err
		}
		return nil, nil
	}
	route, err := m.resolvePublicRoute(channels, name, path)
	if err == nil && route == nil && ProtocolForPath(path) == "openai_responses" {
		err = m.rejectUnavailablePublicTask(ctx, channels, name)
	}
	return route, err
}

// A missing public claim is not permission to send a known task model through
// the ordinary token relay. In particular, disabling a binding must fail closed.
// Factory declarations here are denial-only; they never authorize execution.
// Declarations come precompiled from the applied view; storage is read only
// before the first snapshot is applied.
func (m *Manager) rejectUnavailablePublicTask(ctx context.Context, channels []model.Channel, name string) error {
	v := m.view.Load()
	var state *model.TaskPluginSnapshot
	var stateErr error
	read := func() (model.TaskPluginSnapshot, error) {
		if state == nil && stateErr == nil {
			s, err := m.read(ctx)
			state, stateErr = &s, err
		}
		return *state, stateErr
	}
	for _, ch := range channels {
		key := model.TaskPluginKeyForChannel(ch.Type)
		if key == "" {
			continue
		}
		for advertised := range strings.SplitSeq(ch.Models, ",") {
			if jsplugin.ASCIIFold(advertised) != jsplugin.ASCIIFold(name) {
				continue
			}
			models, err := m.denialModels(ctx, v, ch.Id, key, read)
			if err != nil && !errors.Is(err, model.ErrTaskPluginDisabled) {
				// Unreadable bindings fence only what the factory declares.
				factory, factoryErr := m.factoryModels(key)
				if _, declared, _ := declaredTarget(factory, advertised, ch.ModelMapping); factoryErr == nil && !declared {
					continue
				}
			}
			if err != nil {
				return err
			}
			_, declared, err := declaredTarget(models, advertised, ch.ModelMapping)
			if err != nil {
				return err
			}
			if declared {
				return model.ErrTaskPluginDisabled
			}
		}
	}
	return nil
}

// denialModels returns the declarations that keep an advertised name off the
// ordinary relay: the channel's fixed binding when one exists (even paused),
// otherwise the embedded factory for its channel type.
func (m *Manager) denialModels(ctx context.Context, v *runtimeView, channelID int, key string, read func() (model.TaskPluginSnapshot, error)) ([]string, error) {
	if b := v.production[channelID]; b != nil {
		return b.handle.Meta().Models, nil
	}
	if f := v.fenced[channelID]; f != nil {
		if f.unusable {
			return nil, model.ErrTaskPluginDisabled
		}
		return f.models, nil
	}
	if !v.loaded {
		s, err := read()
		if err != nil {
			return nil, err
		}
		if b, bound := productionBindingFor(s, channelID); bound {
			s.Production = []model.TaskPluginProductionBinding{b}
			handles, failures, err := m.fixedPricingHandles(ctx, s, nil)
			if err != nil {
				return nil, err
			}
			if failures[channelID] != "" {
				return nil, model.ErrTaskPluginDisabled
			}
			return handles[channelID].Meta().Models, nil
		}
	}
	return m.factoryModels(key)
}

// factoryModels compiles the embedded factories once per manager. buildView
// warms it so public requests do not compile.
func (m *Manager) factoryModels(key string) ([]string, error) {
	m.factoryOnce.Do(func() {
		m.factoryDecl = make(map[string]factoryDeclaration, len(m.factories))
		for k, source := range m.factories {
			h, err := compileHandle(source)
			if err != nil {
				m.factoryDecl[k] = factoryDeclaration{err: err}
				continue
			}
			m.factoryDecl[k] = factoryDeclaration{models: h.Meta().Models}
		}
	})
	d := m.factoryDecl[key]
	return d.models, d.err
}

// declaresModel reports whether any known task plugin declares name itself.
func (m *Manager) declaresModel(name string) bool {
	m.factoryModels("")
	for _, d := range m.factoryDecl {
		if _, found := boundModelName(d.models, name); found {
			return true
		}
	}
	v := m.view.Load()
	for _, b := range v.production {
		if _, found := boundModelName(b.handle.Meta().Models, name); found {
			return true
		}
	}
	for _, f := range v.fenced {
		if _, found := boundModelName(f.models, name); found {
			return true
		}
	}
	return false
}

// fenceBindings records every snapshot binding that did not become routable.
func (m *Manager) fenceBindings(v *runtimeView, s model.TaskPluginSnapshot) {
	m.factoryModels("")
	v.loaded, v.fenced = true, map[int]*fencedBinding{}
	pending := s
	pending.Production = nil
	for _, b := range s.Production {
		if v.production[b.ChannelID] == nil {
			pending.Production = append(pending.Production, b)
		}
	}
	if len(pending.Production) == 0 {
		return
	}
	handles, _, _ := m.fixedPricingHandles(context.Background(), pending, nil)
	for _, b := range pending.Production {
		if h := handles[b.ChannelID]; h != nil {
			v.fenced[b.ChannelID] = &fencedBinding{models: h.Meta().Models}
		} else {
			v.fenced[b.ChannelID] = &fencedBinding{unusable: true}
		}
	}
}

func (m *Manager) resolvePublicRoute(channels []model.Channel, name, path string) (*PublicRoute, error) {
	return m.resolveTaskRoute(channels, name, path, "POST", ProtocolForPath(path), "")
}

func (m *Manager) resolveTaskRoute(channels []model.Channel, name, path, method, protocol, keyFilter string) (*PublicRoute, error) {
	v := m.view.Load()
	if v == nil {
		return nil, nil
	}
	// Declared names win over mapping aliases. Within that scope exact advertised
	// spellings win over ASCII folds; conflicting spellings fail closed.
	type candidate struct {
		route    string
		target   PublicTarget
		declared bool
	}
	candidates := []candidate{}
	for _, ch := range channels {
		b, ok := v.production[ch.Id]
		if !ok || ch.Status != common.ChannelStatusEnabled || model.TaskPluginKeyForChannel(ch.Type) != b.binding.Key || keyFilter != "" && b.binding.Key != keyFilter {
			continue
		}
		meta := b.handle.Meta()
		for advertised := range strings.SplitSeq(ch.Models, ",") {
			if jsplugin.ASCIIFold(advertised) != jsplugin.ASCIIFold(name) {
				continue
			}
			tail, declared, err := declaredPricingTarget(b.handle, advertised, ch.ModelMapping)
			if err != nil {
				return nil, err
			}
			if !declared {
				continue
			}
			// Providers without a public Video declaration (local Happyhorse)
			// keep their established video host contract, not a new protocol claim.
			if protocol == "native" {
				r, _, found := MatchNativeRoute(meta, method, path)
				if !found || len(r.Models) > 0 && !slices.Contains(r.Models, tail) {
					continue
				}
			} else if protocol != "task" && !SupportsProtocol(meta, tail, protocol, "") {
				continue
			}
			_, isDeclaration := boundModelName(meta.Models, advertised)
			candidates = append(candidates, candidate{advertised, PublicTarget{b.binding, tail, b.handle}, isDeclaration})
		}
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	exact, declared := false, false
	for _, c := range candidates {
		if c.declared {
			declared = true
		}
	}
	for _, c := range candidates {
		if (!declared || c.declared) && c.route == name {
			exact = true
		}
	}
	r := &PublicRoute{Model: name, Protocol: protocol, Targets: map[int]PublicTarget{}}
	key := ""
	for _, c := range candidates {
		if exact && c.route != name || declared && !c.declared {
			continue
		}
		if r.RoutingModel != "" && (r.RoutingModel != c.route || key != c.target.Binding.Key) {
			return nil, errors.New("ambiguous task model alias")
		}
		r.RoutingModel, key = c.route, c.target.Binding.Key
		r.Targets[c.target.Binding.ChannelID] = c.target
	}
	if len(r.Targets) == 0 {
		return nil, errors.New("ambiguous task model alias")
	}
	return r, nil
}

// Allows is reused by distribution and after channel selection. No SQL or
// channel-cache lock is acquired from inside the scheduler's cache lock.
func (m *Manager) Allows(r *PublicRoute, ch *model.Channel) bool {
	if r == nil {
		return true
	}
	if ch == nil || ch.Status != common.ChannelStatusEnabled {
		return false
	}
	t, ok := r.Targets[ch.Id]
	if !ok {
		return false
	}
	h, b, ok := m.ProductionHandle(ch.Id)
	if !ok || b != t.Binding || model.TaskPluginKeyForChannel(ch.Type) != b.Key || !slices.Contains(strings.Split(ch.Models, ","), r.RoutingModel) {
		return false
	}
	tail, declared, err := declaredPricingTarget(h, r.RoutingModel, ch.ModelMapping)
	return err == nil && declared && tail == t.Model
}
