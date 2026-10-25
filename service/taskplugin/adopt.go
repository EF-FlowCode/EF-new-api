package taskplugin

import (
	"context"
	"slices"

	"eflow.local/new-api/model"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
)

func (m *Manager) AdoptLegacyTask(ctx context.Context, id int64) (*model.Task, error) {
	s, err := m.taskStore()
	if err != nil {
		return nil, err
	}
	adopter, ok := s.(interface {
		AdoptLegacyPluginTask(context.Context, int64, string, model.TaskPluginProductionBinding, model.TaskPluginSourceSnapshot) (*model.Task, error)
	})
	if !ok {
		return nil, ErrUnavailable
	}
	read, err := s.ReadPluginTasks(ctx, []int64{id})
	if err != nil {
		return nil, taskStoreError(err)
	}
	if len(read.Tasks) != 1 {
		return nil, model.ErrTaskPluginIdentity
	}
	task := read.Tasks[0].Task
	binding, found := productionBindingFor(read.Control, task.ChannelId)
	if !found {
		return nil, model.ErrTaskPluginDisabled
	}
	if err := productionEnabled(read.Control, binding); err != nil {
		return nil, err
	}
	source := m.factories[binding.Key]
	if !binding.Factory {
		source = ""
		for _, p := range read.Control.Plugins {
			if p.Key == binding.Key && p.Version == binding.Version {
				source = p.Source
			}
		}
	}
	h, err := host.Compile(source)
	if err != nil || h.Identity() != (host.Identity{Key: binding.Key, Version: binding.Version, SourceHash: binding.SourceHash}) {
		return nil, model.ErrTaskPluginIdentity
	}
	name := task.Properties.UpstreamModelName
	if name == "" {
		name = task.Properties.OriginModelName
	}
	if !slices.Contains(h.Meta().Models, name) {
		return nil, model.ErrTaskPluginIdentity
	}
	saved, err := adopter.AdoptLegacyPluginTask(ctx, id, name, binding, model.TaskPluginSourceSnapshot{Key: binding.Key, Version: binding.Version, SourceHash: binding.SourceHash, APIVersion: h.Meta().APIVersion, Source: source})
	return saved, taskStoreError(err)
}
