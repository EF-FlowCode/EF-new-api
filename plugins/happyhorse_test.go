package plugins

import (
	"testing"

	host "eflow.local/new-api/relay/channel/task/jsplugin"
	"github.com/stretchr/testify/require"
)

func TestHappyhorsePluginContracts(t *testing.T) {
	source, err := Source("happyhorse")
	require.NoError(t, err)
	h, err := host.Compile(source)
	require.NoError(t, err)
	for _, tc := range []struct{ suffix, mediaType string }{{"t2v", ""}, {"i2v", "first_frame"}, {"r2v", "reference_image"}, {"video-edit", "video"}} {
		t.Run(tc.suffix, func(t *testing.T) {
			name := "happyhorse-1.0-" + tc.suffix
			input := map[string]any{"model": "customer-alias", "upstreamModel": name, "baseUrl": "https://provider.example", "apiKey": "  fake-key  ", "requestBody": map[string]any{"model": "customer-alias", "prompt": "a running horse", "input_reference": "https://image.example/input.png", "size": "1080x1920", "seconds": "10", "metadata": map[string]any{"media_url": "https://video.example/input.mp4"}}}
			d, err := h.Build(t.Context(), "buildSubmitRequest", input)
			require.NoError(t, err)
			require.Equal(t, "Bearer fake-key", d.Headers["Authorization"])
			require.Equal(t, "enable", d.Headers["X-DashScope-Async"])
			body := d.Body.(map[string]any)
			require.Equal(t, name, body["model"])
			require.Equal(t, map[string]any{"resolution": "1080P", "ratio": "9:16", "duration": float64(10)}, body["parameters"])
			wireInput := body["input"].(map[string]any)
			require.Equal(t, "a running horse", wireInput["prompt"])
			if tc.mediaType == "" {
				require.NotContains(t, wireInput, "media")
			} else {
				require.Equal(t, tc.mediaType, wireInput["media"].([]any)[0].(map[string]any)["type"])
			}
			facts, err := h.Usage(t.Context(), name, "extractUsage", input)
			require.NoError(t, err)
			require.Equal(t, map[string]any{"seconds": float64(10), "resolution": "1080P"}, facts)
			result, err := h.ParseSubmit(t.Context(), input, host.Response{Status: 200, Body: map[string]any{"output": map[string]any{"task_id": "accepted"}}})
			require.NoError(t, err)
			require.Equal(t, "accepted", result["taskId"])
		})
	}
	base := map[string]any{"upstreamModel": "happyhorse-1.0-t2v", "baseUrl": "https://provider.example", "requestBody": map[string]any{"prompt": "a horse"}}
	d, err := h.Build(t.Context(), "buildSubmitRequest", base)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"duration": float64(5), "resolution": "720P", "ratio": "16:9"}, d.Body.(map[string]any)["parameters"])
	for _, body := range []map[string]any{
		{"prompt": ""}, {"prompt": "horse", "duration": 2}, {"prompt": "horse", "duration": 16}, {"prompt": "horse", "seconds": "bad"}, {"prompt": "horse", "metadata": map[string]any{"resolution": "4K"}},
	} {
		base["requestBody"] = body
		_, err := h.Build(t.Context(), "buildSubmitRequest", base)
		require.Error(t, err)
	}
	base["requestBody"] = map[string]any{"prompt": "horse"}
	for _, suffix := range []string{"i2v", "r2v", "video-edit"} {
		base["upstreamModel"] = "happyhorse-1.0-" + suffix
		_, err := h.Build(t.Context(), "buildSubmitRequest", base)
		require.Error(t, err)
	}
	for providerStatus, status := range map[string]string{"PENDING": "QUEUED", "RUNNING": "IN_PROGRESS", "SUCCEEDED": "SUCCESS", "FAILED": "FAILURE", "CANCELED": "FAILURE", "UNKNOWN": "FAILURE", "new-state": "QUEUED"} {
		result, err := h.ParseTask(t.Context(), base, host.Response{Status: 200, Body: map[string]any{"output": map[string]any{"task_status": providerStatus, "video_url": "https://result.example/video.mp4", "message": "denied"}}})
		require.NoError(t, err)
		require.Equal(t, status, result["status"])
	}
}

// Migrated from the retired Go adapter: provider usage shape must not prevent
// reading a completed Happyhorse video. Money remains governed by the host.
func TestHappyhorseFlexibleUsageResponse(t *testing.T) {
	source, err := Source("happyhorse")
	require.NoError(t, err)
	h, err := host.Compile(source)
	require.NoError(t, err)
	for _, usage := range []string{
		`{"duration":5.04,"input_video_duration":5.033,"output_video_duration":3.0,"video_count":1,"SR":720}`,
		`{"duration":5,"input_video_duration":5,"output_video_duration":3,"video_count":1,"SR":720}`,
		`{"duration":"5.04","input_video_duration":"5.033","output_video_duration":"3","video_count":"1","SR":"720"}`,
	} {
		response, err := host.DecodeResponse(200, nil, []byte(`{"output":{"task_status":"SUCCEEDED","video_url":"https://example.com/video.mp4"},"usage":`+usage+`}`))
		require.NoError(t, err)
		result, err := h.ParseTask(t.Context(), map[string]any{"upstreamModel": "happyhorse-1.0-video-edit"}, response)
		require.NoError(t, err)
		require.Equal(t, "SUCCESS", result["status"])
		require.Equal(t, "https://example.com/video.mp4", result["url"])
	}
}

func TestFactoryModelsOwnsItsCatalog(t *testing.T) {
	models := FactoryModels("happyhorse")
	require.ElementsMatch(t, []string{"happyhorse-1.0-t2v", "happyhorse-1.0-i2v", "happyhorse-1.0-r2v", "happyhorse-1.0-video-edit"}, models)
	models[0] = "changed"
	require.NotContains(t, FactoryModels("happyhorse"), "changed")
}
