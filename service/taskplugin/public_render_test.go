package taskplugin

import (
	"testing"

	"eflow.local/new-api/model"
	"eflow.local/new-api/plugins"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
	"github.com/stretchr/testify/require"
)

// Every public renderer must see the same settlement gate as native rendering:
// provider terminal data stays hidden until the host has settled billing.
func TestPublicRenderersHideBillingPendingData(t *testing.T) {
	source, err := plugins.Source("doubao")
	require.NoError(t, err)
	h, err := host.Compile(source + "\nprotocols.openai_video.render = function (ctx, view) { return { echoed: view.data, echoed_status: view.status }; };")
	require.NoError(t, err)
	task := &model.Task{TaskID: "task_pending", Status: model.TaskStatusBillingPending, Data: []byte(`{"content":{"video_url":"https://provider.invalid/v.mp4"}}`)}

	_, view := PublicTaskView(task)
	require.Equal(t, map[string]any{}, view["data"])
	require.EqualValues(t, model.TaskStatusInProgress, view["status"])

	_, view, err = PublicTaskPresentation(t.Context(), h, task)
	require.NoError(t, err)
	require.Equal(t, map[string]any{}, view["data"])
	require.EqualValues(t, model.TaskStatusInProgress, view["status"])

	out, err := RenderPublicVideo(t.Context(), h, task)
	require.NoError(t, err)
	require.Equal(t, map[string]any{}, out["echoed"])
	require.Equal(t, string(model.TaskStatusInProgress), out["echoed_status"])
	require.Equal(t, task.Status.ToVideoStatus(), out["status"])

	task.Status = model.TaskStatusSuccess
	out, err = RenderPublicVideo(t.Context(), h, task)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"content": map[string]any{"video_url": "https://provider.invalid/v.mp4"}}, out["echoed"])
}
