package plugins

import (
	"strings"
	"testing"

	host "eflow.local/new-api/relay/channel/task/jsplugin"
	"github.com/stretchr/testify/require"
)

func TestKlingPluginAuthorization(t *testing.T) {
	source, err := Source("kling")
	require.NoError(t, err)
	h, err := host.Compile(source)
	require.NoError(t, err)
	build := func(apiKey string) (string, error) {
		d, err := h.Build(t.Context(), "buildSubmitRequest", map[string]any{
			"upstreamModel": "kling-v1", "baseUrl": "https://api.klingai.com", "apiKey": apiKey,
			"requestBody": map[string]any{"prompt": "a cat"},
		})
		if err != nil {
			return "", err
		}
		return d.Headers["Authorization"], nil
	}

	auth, err := build("  single-key  ")
	require.NoError(t, err)
	require.Equal(t, "Bearer single-key", auth, "a single API key is sent as-is")

	auth, err = build("sk-relay")
	require.NoError(t, err)
	require.Equal(t, "Bearer sk-relay", auth)

	auth, err = build("access|secret")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(auth, "Bearer ey"), "an accessKey|secretKey pair is signed into a JWT")

	for _, key := range []string{"", "   ", "a|b|c"} {
		_, err = build(key)
		require.Error(t, err, key)
	}
}
