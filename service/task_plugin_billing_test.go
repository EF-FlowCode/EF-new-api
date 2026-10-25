package service

import (
	"math"
	"testing"

	"eflow.local/new-api/plugins"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
	"github.com/stretchr/testify/require"
)

func TestPluginVeoDurationMatchesBilling(t *testing.T) {
	for _, key := range []string{"google", "vertex-ai"} {
		t.Run(key, func(t *testing.T) {
			source, err := plugins.Source(key)
			require.NoError(t, err)
			h, err := host.Compile(source)
			require.NoError(t, err)
			input := map[string]any{"baseUrl": "https://provider.example", "upstreamModel": "veo-3.1-fast-generate-preview", "auth": map[string]any{"projectId": "project"}, "authHeader": "Bearer token", "requestBody": map[string]any{"prompt": "test", "metadata": map[string]any{"durationSeconds": float64(6), "resolution": "4k"}}}
			d, err := h.Build(t.Context(), "buildSubmitRequest", input)
			require.NoError(t, err)
			facts, err := h.Usage(t.Context(), "veo-3.1-fast-generate-preview", "extractUsage", input)
			require.NoError(t, err)
			require.Equal(t, d.Body.(map[string]any)["parameters"].(map[string]any)["durationSeconds"], facts["seconds"])
			ratios, err := TaskPluginProviderRatios(key, "veo-3.1-fast-generate-preview", facts)
			require.NoError(t, err)
			quota, err := TaskPluginQuota(1000, ratios)
			require.NoError(t, err)
			require.Equal(t, 13999, quota)
		})
	}
	for _, key := range []string{"kling", "sunoapi", "hailuo", "jimeng", "vidu"} {
		ratios, err := TaskPluginProviderRatios(key, "model", map[string]any{"units": 1000000, "credits": 1000000, "clips": 1000000})
		require.NoError(t, err)
		quota, err := TaskPluginQuota(100, ratios)
		require.NoError(t, err)
		require.Equal(t, 100, quota)
	}
}

func TestTaskPluginHappyhorseLocalPricing(t *testing.T) {
	for _, name := range []string{"happyhorse-1.0-t2v", "happyhorse-1.0-i2v", "happyhorse-1.0-r2v", "happyhorse-1.0-video-edit"} {
		for _, tc := range []struct {
			resolution string
			quota      int
		}{{"720P", 500}, {"1080P", 889}, {"480P", 500}} {
			ratios, err := TaskPluginBillingRatios(name, map[string]any{"seconds": float64(5), "resolution": tc.resolution})
			require.NoError(t, err)
			quota, err := TaskPluginQuota(100, ratios)
			require.NoError(t, err)
			require.Equal(t, tc.quota, quota, name+" "+tc.resolution)
		}
	}
	for _, seconds := range []float64{-1, 0, 3601, math.NaN(), math.Inf(1)} {
		_, err := TaskPluginBillingRatios("happyhorse-1.0-t2v", map[string]any{"seconds": seconds, "resolution": "720P"})
		require.Error(t, err)
	}
	_, err := TaskPluginQuota(int(^uint(0)>>1), map[string]float64{"seconds": 3600})
	require.Error(t, err)
}

func TestTaskPluginWANLocalPricing(t *testing.T) {
	for _, tc := range []struct {
		model, resolution string
		quota             int
	}{
		{"wan2.6-i2v", "1080P", 10000},
		{"wan2.5-t2v-preview", "720P", 12000},
		{"wan2.5-i2v-preview", "1080P", 20000},
		{"wan2.2-t2v-plus", "1080P", 30000},
		{"wan2.2-i2v-plus", "1080P", 30000},
		{"wan2.2-kf2v-flash", "1080P", 28800},
		{"wan2.2-i2v-flash", "720P", 12000},
		{"wan2.2-s2v", "720P", 10800},
		{"wan2.6-t2v", "1080P", 6000},
	} {
		ratios, err := TaskPluginBillingRatios(tc.model, map[string]any{"seconds": float64(5), "resolution": tc.resolution})
		require.NoError(t, err)
		quota, err := TaskPluginQuota(1200, ratios)
		require.NoError(t, err)
		require.Equal(t, tc.quota, quota, tc.model)
	}
}

func TestPluginDoubaoVideoInputPricing(t *testing.T) {
	for _, tc := range []struct {
		name, resolution string
		video            bool
		want             float64
	}{
		{"doubao-seedance-2-0-260128", "", false, 1},
		{"doubao-seedance-2-0-260128", "", true, 28.0 / 46.0},
		{"doubao-seedance-2-0-260128", "1080P", false, 51.0 / 46.0},
		{"doubao-seedance-2-0-260128", " 4K ", true, 16.0 / 46.0},
		{"doubao-seedance-2-0-fast-260128", "4k", false, 1},
		{"doubao-seedance-2-0-fast-260128", "720p", true, 22.0 / 37.0},
		{"unknown", "", false, 1},
	} {
		facts := map[string]any{"resolution": tc.resolution}
		if tc.video {
			facts["video_input"] = "video"
		}
		ratios, err := TaskPluginProviderRatios("doubao", tc.name, facts)
		require.NoError(t, err)
		require.InDelta(t, tc.want, ratios["video_input"], 1e-12)
	}
}
