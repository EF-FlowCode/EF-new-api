package taskplugin

import (
	"encoding/json"
	"strconv"
	"testing"

	"eflow.local/new-api/constant"
	"eflow.local/new-api/model"
	"eflow.local/new-api/plugins"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
	"github.com/stretchr/testify/require"
)

func TestAllPluginLegacyAdoptionMySQL(t *testing.T) {
	s := taskQuerySQLStore(t)
	require.NoError(t, s.DB().AutoMigrate(&model.Channel{}))
	factories := map[string]string{}
	for _, key := range []string{"alibaba", "happyhorse", "doubao", "google", "hailuo", "jimeng", "kling", "sora", "sunoapi", "vertex-ai", "vidu"} {
		source, err := plugins.Source(key)
		require.NoError(t, err)
		factories[key] = source
	}
	m := NewManager(s, factories)
	for _, channelType := range []int{17, 1001, 54, 24, 35, 51, 50, 55, 36, 41, 52, 1, 45} {
		t.Run(strconv.Itoa(channelType), func(t *testing.T) {
			key := model.TaskPluginKeyForChannel(channelType)
			h, err := host.Compile(factories[key])
			require.NoError(t, err)
			require.NoError(t, s.DB().Create(&model.Channel{Id: channelType, Type: channelType}).Error)
			require.NoError(t, m.BindProduction(t.Context(), channelType, ProductionBindingRequest{Key: key, Version: h.Identity().Version, Factory: true, Enabled: true}))
			legacy := queryTask(strconv.Itoa(channelType), h.Meta().Models[0])
			legacy.ChannelId = channelType
			legacy.Platform = constant.TaskPlatform(strconv.Itoa(channelType))
			if key == "sunoapi" {
				legacy.Platform = constant.TaskPlatformSuno
			}
			if key == "jimeng" {
				legacy.Data = json.RawMessage(`{"req_key":"jimeng_t2v_v30_1080p"}`)
			}
			if key == "sora" {
				legacy.Data = json.RawMessage(`{"seconds":"8","size":"1792x1024"}`)
			}
			require.NoError(t, s.DB().Create(&legacy).Error)
			require.NoError(t, s.DB().First(&legacy, legacy.ID).Error)
			saved, err := m.AdoptLegacyTask(t.Context(), legacy.ID)
			require.NoError(t, err)
			require.Equal(t, key, saved.PrivateData.Plugin.Key)
			again, err := m.AdoptLegacyTask(t.Context(), legacy.ID)
			require.NoError(t, err)
			require.Equal(t, saved, again)
			if key == "jimeng" {
				require.Equal(t, "jimeng_t2v_v30_1080p", saved.PrivateData.PluginContext.State["req_key"])
			}
			if key == "sora" {
				require.Equal(t, float64(8), saved.PrivateData.PluginContext.Usage["seconds"])
			}
			saved.PrivateData.Plugin = nil
			saved.PrivateData.PluginContext = nil
			require.Equal(t, legacy, *saved, "business fields must survive adoption")
			if key == "jimeng" {
				missing := legacy
				missing.ID = 0
				missing.TaskID = "missing-product"
				missing.Data = json.RawMessage(`{}`)
				require.NoError(t, s.DB().Create(&missing).Error)
				_, err = m.AdoptLegacyTask(t.Context(), missing.ID)
				require.ErrorIs(t, err, model.ErrTaskPluginIdentity)
				var stored model.Task
				require.NoError(t, s.DB().First(&stored, missing.ID).Error)
				require.Equal(t, missing, stored)
			}
		})
	}
}
