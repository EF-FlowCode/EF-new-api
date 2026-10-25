package plugins

import (
	"testing"

	host "eflow.local/new-api/relay/channel/task/jsplugin"
	"github.com/stretchr/testify/require"
)

// Preserve the chosen protocol behavior after removing the Go/JS comparison.
func TestAlibabaPluginDefaultAndExplicitResolution(t *testing.T) {
	source, err := Source("alibaba")
	require.NoError(t, err)
	h, err := host.Compile(source)
	require.NoError(t, err)
	for _, tc := range []struct{ size, want string }{{"", "1920*1080"}, {"1280*720", "1280*720"}} {
		d, err := h.Build(t.Context(), "buildSubmitRequest", map[string]any{"baseUrl": "https://provider.example", "upstreamModel": "wan2.6-t2v", "requestBody": map[string]any{"prompt": "fixture", "duration": 5, "size": tc.size}})
		require.NoError(t, err)
		params := d.Body.(map[string]any)["parameters"].(map[string]any)
		require.Equal(t, tc.want, params["size"])
		require.Equal(t, float64(5), params["duration"])
	}
}
