package taskplugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"

	"eflow.local/new-api/model"
	"eflow.local/new-api/pkg/jsplugin"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
)

type ProductionPrepared struct {
	manager       *Manager
	Handle        *host.Handle
	Identity      host.Identity
	Binding       model.TaskPluginProductionBinding
	UpstreamModel string
	Input         map[string]any
	Descriptor    host.Descriptor
	Facts         map[string]any
}

func (m *Manager) CheckProductionEnabled(ctx context.Context, channelID int) error {
	s, err := m.read(ctx)
	if err != nil {
		return err
	}
	b, found := productionBindingFor(s, channelID)
	if !found {
		return model.ErrTaskPluginDisabled
	}
	return productionEnabled(s, b)
}

func productionBindingFor(s model.TaskPluginSnapshot, channelID int) (model.TaskPluginProductionBinding, bool) {
	for _, b := range s.Production {
		if b.ChannelID == channelID {
			return b, true
		}
	}
	return model.TaskPluginProductionBinding{}, false
}

func productionEnabled(s model.TaskPluginSnapshot, b model.TaskPluginProductionBinding) error {
	if !b.Enabled {
		return model.ErrTaskPluginDisabled
	}
	return model.CheckTaskPluginEnabled(s, model.TaskPluginIdentity{IdentityVersion: 1, APIVersion: 1, Key: b.Key, Version: b.Version, SourceHash: b.SourceHash, Factory: b.Factory})
}

// Execute rechecks the exact binding and switches after preparation/usage and
// immediately before invoking HTTP. It does not retry or select Go fallback.
func (p *ProductionPrepared) Execute(ctx context.Context, executor host.Executor, base string, files map[string]host.File) (host.Response, error) {
	if p == nil || p.manager == nil || p.Handle == nil {
		return host.Response{}, ErrTaskQueryHook
	}
	if err := ctx.Err(); err != nil {
		return host.Response{}, err
	}
	s, err := p.manager.read(ctx)
	if err != nil {
		return host.Response{}, err
	}
	b, found := productionBindingFor(s, p.Binding.ChannelID)
	if !found || b != p.Binding {
		return host.Response{}, model.ErrTaskPluginDisabled
	}
	if err := productionEnabled(s, b); err != nil {
		return host.Response{}, err
	}
	return executor.Execute(ctx, p.Handle, base, p.Descriptor, files)
}

func (m *Manager) PrepareProductionSubmit(ctx context.Context, channelID int, input map[string]any) (*ProductionPrepared, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	state, err := m.read(ctx)
	if err != nil {
		return nil, err
	}
	var binding model.TaskPluginProductionBinding
	found := false
	for _, b := range state.Production {
		if b.ChannelID == channelID {
			binding = b
			found = true
			break
		}
	}
	if !found || !binding.Enabled {
		return nil, model.ErrTaskPluginDisabled
	}
	identity := model.TaskPluginIdentity{IdentityVersion: 1, APIVersion: 1, Key: binding.Key, Version: binding.Version, SourceHash: binding.SourceHash, Factory: binding.Factory}
	if err := model.CheckTaskPluginEnabled(state, identity); err != nil {
		return nil, err
	}
	handle, current, ok := m.ProductionHandle(channelID)
	if !ok || current != binding {
		return nil, model.ErrTaskPluginDisabled
	}
	name, declared, targetErr := declaredPricingTarget(handle, stringValue(input["upstreamModel"]), nil)
	if targetErr != nil {
		return nil, targetErr
	}
	if !declared {
		return nil, errors.New("model is not declared by the bound plugin")
	}
	// Resolve only against the fixed binding, not the active/global registry.
	// Keep model and requestBody untouched: they identify the client's request.
	input = maps.Clone(input)
	input["upstreamModel"] = name
	if protocol := stringValue(input["publicProtocol"]); protocol != "" {
		if err := decodePublicSubmit(ctx, handle, input, protocol); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrPublicProtocolRequest, err)
		}
	}
	descriptor, err := handle.Build(ctx, "buildSubmitRequest", input)
	if err != nil {
		return nil, ErrTaskQueryHook
	}
	return &ProductionPrepared{manager: m, Handle: handle, Identity: handle.Identity(), Binding: binding, UpstreamModel: name, Input: cloneJSONMap(input), Descriptor: descriptor}, nil
}

func boundModelName(models []string, name string) (string, bool) {
	if name == "" {
		return "", false
	}
	if slices.Contains(models, name) {
		return name, true
	}
	folded := jsplugin.ASCIIFold(name)
	var match string
	for _, declared := range models {
		if jsplugin.ASCIIFold(declared) == folded {
			if match != "" {
				return "", false
			}
			match = declared
		}
	}
	return match, match != ""
}

func (p *ProductionPrepared) ParseSubmit(ctx context.Context, response host.Response) (map[string]any, error) {
	if p == nil || p.Handle == nil {
		return nil, ErrTaskQueryHook
	}
	result, err := p.Handle.ParseSubmit(ctx, p.Input, response)
	if err != nil {
		return nil, ErrTaskQueryHook
	}
	return result, nil
}

func (p *ProductionPrepared) Usage(ctx context.Context) (map[string]any, error) {
	if p == nil || p.Handle == nil {
		return nil, ErrTaskQueryHook
	}
	facts, err := p.Handle.Usage(ctx, p.UpstreamModel, "extractUsage", p.Input)
	if err == nil {
		p.Facts = facts
	}
	return facts, err
}

func cloneJSONMap(input map[string]any) map[string]any {
	raw, _ := json.Marshal(input)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out
}

func stringValue(v any) string { s, _ := v.(string); return s }

type ProductionBindingRequest struct {
	Key     string `json:"key"`
	Version string `json:"version"`
	Factory bool   `json:"factory"`
	Enabled bool   `json:"enabled"`
}

func (r *ProductionBindingRequest) UnmarshalJSON(data []byte) error {
	type request ProductionBindingRequest
	var value request
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	*r = ProductionBindingRequest(value)
	return nil
}

func (m *Manager) BindProduction(ctx context.Context, channelID int, req ProductionBindingRequest) error {
	if channelID <= 0 || model.TaskPluginChannelType(req.Key) == 0 || req.Version == "" {
		return errors.New("production binding requires a supported provider, channel and version")
	}
	return m.update(ctx, func(s *model.TaskPluginSnapshot) error {
		b := model.TaskPluginProductionBinding{ChannelID: channelID, Key: req.Key, Version: req.Version, Factory: req.Factory, Enabled: req.Enabled}
		var source string
		if req.Factory {
			source = m.factories[req.Key]
		} else {
			for _, p := range s.Plugins {
				if p.Key == req.Key && p.Version == req.Version {
					source = p.Source
					b.SourceHash = p.SourceHash
					break
				}
			}
		}
		if source == "" {
			return ErrNotFound
		}
		h, err := host.Compile(source)
		if err != nil {
			return err
		}
		if h.Identity().Version != req.Version {
			return ErrNotFound
		}
		b.SourceHash = h.Identity().SourceHash
		for i, old := range s.Production {
			if old.ChannelID == channelID {
				s.Production[i] = b
				return nil
			}
		}
		if len(s.Production) >= 64 {
			return errors.New("at most 64 production bindings")
		}
		s.Production = append(s.Production, b)
		return nil
	})
}

func (m *Manager) UnbindProduction(ctx context.Context, channelID int) error {
	return m.update(ctx, func(s *model.TaskPluginSnapshot) error {
		for i, b := range s.Production {
			if b.ChannelID == channelID {
				s.Production = append(s.Production[:i], s.Production[i+1:]...)
				return nil
			}
		}
		return ErrNotFound
	})
}

func (m *Manager) ProductionBindings(ctx context.Context) ([]model.TaskPluginProductionBinding, error) {
	s, err := m.read(ctx)
	if err != nil {
		return nil, err
	}
	return append([]model.TaskPluginProductionBinding(nil), s.Production...), nil
}

func (m *Manager) ProductionHandle(channelID int) (*host.Handle, model.TaskPluginProductionBinding, bool) {
	v := m.view.Load()
	if v == nil {
		return nil, model.TaskPluginProductionBinding{}, false
	}
	b, ok := v.production[channelID]
	if !ok {
		return nil, model.TaskPluginProductionBinding{}, false
	}
	return b.handle, b.binding, true
}
