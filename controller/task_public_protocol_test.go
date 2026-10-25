package controller

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/internal/testdb"
	"eflow.local/new-api/model"
	"eflow.local/new-api/plugins"
	"eflow.local/new-api/relay"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
	management "eflow.local/new-api/service/taskplugin"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestPublicTaskPresentationMySQL(t *testing.T) {
	db := testdb.OpenMySQL8(t, &model.Task{}, &model.Channel{}, &model.TaskPlugin{}, &model.TaskPluginControl{}, &model.TaskPluginShadowBinding{}, &model.TaskPluginProductionBinding{}, &model.TaskPluginSourceSnapshot{}, &model.TaskPluginTaskReference{})
	oldDB, oldManager := model.DB, management.Default
	t.Cleanup(func() { model.DB, management.Default = oldDB, oldManager })
	model.DB = db
	require.NoError(t, db.Create(&model.Channel{Id: 54, Type: 54, Status: common.ChannelStatusEnabled}).Error)
	require.NoError(t, db.Create(&model.TaskPluginControl{Id: 1, Revision: 1, Enabled: true, FactoryEnabled: true, DisabledFactory: "[]"}).Error)
	source, err := plugins.Source("doubao")
	require.NoError(t, err)
	m := management.NewManager(model.SQLTaskPluginStore{DB: func() *gorm.DB { return db }}, map[string]string{"doubao": source})
	management.Default = m
	require.NoError(t, m.BindProduction(t.Context(), 54, management.ProductionBindingRequest{Key: "doubao", Version: "1.0.2", Factory: true, Enabled: true}))
	task, err := m.CreatePluginTask(t.Context(), model.Task{TaskID: "public-response", UserId: 1, ChannelId: 54, Platform: "54", Status: model.TaskStatusSubmitted, Properties: model.Properties{OriginModelName: "MyAlias", UpstreamModelName: "doubao-seedance-1-0-lite-t2v"}, PrivateData: model.TaskPrivateData{Key: "must-not-leak", UpstreamTaskID: "provider-id", PluginContext: &model.TaskPluginContext{PublicProtocol: "openai_responses", PublicMode: "background"}}}, management.TaskVersion{Key: "doubao", Version: "1.0.2", Factory: true})
	require.NoError(t, err)
	present := func(ctx context.Context) (*httptest.ResponseRecorder, error) {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest("POST", "/v1/responses", nil).WithContext(ctx)
		e := completeTaskSubmission(c, nil, &relay.TaskSubmitResult{SavedTask: task})
		if e != nil {
			return rec, e.Error
		}
		return rec, nil
	}
	rec, err := present(t.Context())
	require.NoError(t, err)
	require.Contains(t, rec.Body.String(), `"status":"queued"`)
	require.Equal(t, task.TaskID, rec.Header().Get("X-Task-ID"))
	t.Run("bounded_initial_database_read", func(t *testing.T) {
		old := constant.TaskProtocol
		constant.TaskProtocol.Heartbeat = 100 * time.Millisecond
		defer func() { constant.TaskProtocol = old }()
		const callback = "test_public_slow_read"
		require.NoError(t, db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
			if tx.Statement.Table == "tasks" {
				// Execute a real cancellable MySQL operation under the PublicTask
				// query context, rather than sleeping in an uncancellable test stub.
				tx.AddError(db.WithContext(tx.Statement.Context).Exec("SELECT SLEEP(5)").Error)
			}
		}))
		started := time.Now()
		_, err := present(t.Context())
		elapsed := time.Since(started)
		require.NoError(t, db.Callback().Query().Remove(callback))
		require.Error(t, err)
		require.Less(t, elapsed, time.Second)
		var saved model.Task
		require.NoError(t, db.First(&saved, task.ID).Error)
		require.EqualValues(t, model.TaskStatusSubmitted, saved.Status)
		require.Equal(t, task.Quota, saved.Quota)
	})
	_, _, err = m.PublicTask(t.Context(), task.TaskID, 2, "openai_responses")
	require.Error(t, err)
	t.Run("owner_http_query", func(t *testing.T) {
		for _, owner := range []int{1, 2} {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest("GET", "/v1/responses/public-response", nil)
			c.Params = gin.Params{{Key: "response_id", Value: task.TaskID}}
			common.SetContextKey(c, constant.ContextKeyUserId, owner)
			RetrievePublicTaskResponse(c)
			if owner == 1 {
				require.Equal(t, 200, rec.Code)
			} else {
				require.Equal(t, 404, rec.Code)
			}
		}
	})
	// Rebinding cannot change how an already accepted task is rendered.
	changed := strings.Replace(source, `version: "1.0.2"`, `version: "2.0.0"`, 1)
	changed = strings.Replace(changed, `vendor: "doubao"`, `vendor: "changed"`, 1)
	_, err = m.Upload(t.Context(), management.UploadRequest{Source: changed})
	require.NoError(t, err)
	require.NoError(t, m.BindProduction(t.Context(), 54, management.ProductionBindingRequest{Key: "doubao", Version: "2.0.0", Enabled: true}))
	for _, mode := range []string{"background", "sync", "stream"} {
		t.Run(mode, func(t *testing.T) {
			task.PrivateData.PluginContext.PublicMode = mode
			task.Status = model.TaskStatusSuccess
			task.Data = []byte(`{"status":"succeeded"}`)
			require.NoError(t, db.Save(task).Error)
			rec, err := present(t.Context())
			require.NoError(t, err)
			require.NotContains(t, rec.Body.String(), "must-not-leak")
			require.NotContains(t, rec.Body.String(), "changed")
			if mode == "stream" {
				require.Equal(t, "text/event-stream", rec.Header().Get("Content-Type"))
				require.Contains(t, rec.Body.String(), "event: response.output_text.delta")
				require.Contains(t, rec.Body.String(), "event: response.completed")
				n := 0
				for line := range strings.SplitSeq(rec.Body.String(), "\n") {
					if strings.HasPrefix(line, "data: ") {
						var e map[string]any
						require.NoError(t, json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &e))
						require.EqualValues(t, n, e["sequence_number"])
						n++
					}
				}
			} else {
				require.Contains(t, rec.Body.String(), `"status":"completed"`)
				require.Contains(t, rec.Body.String(), `"vendor":"doubao"`)
			}
		})
	}
	task.Status = model.TaskStatusBillingPending
	task.PrivateData.PluginContext.PublicMode = "background"
	require.NoError(t, db.Save(task).Error)
	rec, err = present(t.Context())
	require.NoError(t, err)
	require.Contains(t, rec.Body.String(), `"status":"in_progress"`)
	require.NotContains(t, rec.Body.String(), `"status":"completed"`)
	// Disconnect ends presentation only. The durable task is still unsettled.
	task.PrivateData.PluginContext.PublicMode = "stream"
	require.NoError(t, db.Save(task).Error)
	streamCtx, stopStream := context.WithCancel(t.Context())
	streamRec := &cancelPublicRecorder{ResponseRecorder: httptest.NewRecorder(), cancel: stopStream}
	streamC, _ := gin.CreateTestContext(streamRec)
	streamC.Request = httptest.NewRequest("POST", "/v1/responses", nil).WithContext(streamCtx)
	require.Nil(t, completeTaskSubmission(streamC, nil, &relay.TaskSubmitResult{SavedTask: task}))
	require.Contains(t, streamRec.Body.String(), "event: response.created")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, _ = present(ctx)
	var saved model.Task
	require.NoError(t, db.First(&saved, task.ID).Error)
	require.EqualValues(t, model.TaskStatusBillingPending, saved.Status)
	t.Run("wait_for_local_settlement", func(t *testing.T) {
		task.PrivateData.PluginContext.PublicMode = "sync"
		require.NoError(t, db.Save(task).Error)
		done := make(chan error, 1)
		go func() {
			time.Sleep(100 * time.Millisecond)
			done <- db.Model(&model.Task{}).Where("id = ?", task.ID).Update("status", model.TaskStatusSuccess).Error
		}()
		rec, err := present(t.Context())
		require.NoError(t, err)
		require.NoError(t, <-done)
		require.Contains(t, rec.Body.String(), `"status":"completed"`)
	})
	task.PrivateData.PluginContext.PublicMode = "stream"
	task.Status = model.TaskStatusFailure
	task.FailReason = "fixture failure"
	require.NoError(t, db.Save(task).Error)
	rec, err = present(t.Context())
	require.NoError(t, err)
	require.Contains(t, rec.Body.String(), "event: response.failed")
	require.NotContains(t, rec.Body.String(), "event: response.completed")
	task.PrivateData.PluginContext.PublicProtocol = "openai_video"
	task.Status = model.TaskStatusBillingPending
	require.NoError(t, db.Save(task).Error)
	rec, err = present(t.Context())
	require.NoError(t, err)
	require.Contains(t, rec.Body.String(), `"model":"MyAlias"`)
	require.Contains(t, rec.Body.String(), `"status":"in_progress"`)
	require.NoError(t, db.Model(&model.Channel{}).Where("id = 54").Update("status", common.ChannelStatusManuallyDisabled).Error)
	_, _, err = m.PublicTask(t.Context(), task.TaskID, 1, "openai_video")
	require.Error(t, err)
	t.Run("terminal_result_after_channel_disabled", func(t *testing.T) {
		task.Status = model.TaskStatusSuccess
		task.Data = []byte(`{"status":"succeeded"}`)
		require.NoError(t, db.Save(task).Error)
		for _, unbind := range []bool{false, true} {
			if unbind {
				require.NoError(t, m.BindProduction(t.Context(), 54, management.ProductionBindingRequest{Key: "doubao", Version: "2.0.0", Enabled: false}))
			}
			got, h, err := m.PublicTask(t.Context(), task.TaskID, 1, "openai_video")
			require.NoError(t, err)
			require.True(t, got.ChannelDisabled)
			out, err := management.RenderPublicVideo(t.Context(), h, got)
			require.NoError(t, err)
			require.Equal(t, management.ChannelStatusDisabled, out["channel_status"])
			require.Equal(t, management.ChannelStatusDisabled, management.GenericTaskView(got)["channel_status"])
			// The historical result is still rendered by the task's own source.
			raw, err := json.Marshal(out)
			require.NoError(t, err)
			require.NotContains(t, string(raw), "changed")
			require.Equal(t, management.ChannelStatusDisabled, tasksToDto([]*model.Task{got}, false)[0].ChannelStatus)
			_, _, err = m.PublicTask(t.Context(), task.TaskID, 2, "openai_video")
			require.ErrorIs(t, err, management.ErrNotFound)
		}
		task.PrivateData.PluginContext.PublicProtocol, task.PrivateData.PluginContext.PublicMode = "openai_responses", "background"
		require.NoError(t, db.Save(task).Error)
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest("GET", "/v1/responses/public-response", nil)
		c.Params = gin.Params{{Key: "response_id", Value: task.TaskID}}
		common.SetContextKey(c, constant.ContextKeyUserId, 1)
		RetrievePublicTaskResponse(c)
		require.Equal(t, 200, rec.Code)
		var body struct {
			Status   string            `json:"status"`
			Metadata map[string]string `json:"metadata"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		require.Equal(t, "completed", body.Status)
		require.Equal(t, management.ChannelStatusDisabled, body.Metadata["channel_status"])
		// Unsettled work stays fail-closed while the channel is off.
		require.NoError(t, db.Model(&model.Task{}).Where("id = ?", task.ID).Update("status", model.TaskStatusInProgress).Error)
		_, _, err = m.PublicTask(t.Context(), task.TaskID, 1, "openai_responses")
		require.ErrorIs(t, err, model.ErrTaskPluginDisabled)
		require.Empty(t, tasksToDto([]*model.Task{reloadPublicTask(t, db, task.ID)}, false)[0].ChannelStatus)
	})
}

type cancelPublicRecorder struct {
	*httptest.ResponseRecorder
	cancel context.CancelFunc
}

func (r *cancelPublicRecorder) Flush() { r.ResponseRecorder.Flush(); r.cancel() }

func TestPublicAllProvidersPresentation(t *testing.T) {
	for _, key := range []string{"alibaba", "doubao", "google", "hailuo", "jimeng", "kling", "sora", "sunoapi", "vertex-ai", "vidu"} {
		t.Run(key, func(t *testing.T) {
			source, err := plugins.Source(key)
			require.NoError(t, err)
			h, err := host.Compile(source)
			require.NoError(t, err)
			name := h.Meta().Models[0]
			if key == "alibaba" {
				name = "wan2.6-t2v"
			}
			task := &model.Task{TaskID: "local-id", Status: model.TaskStatusSuccess, Properties: model.Properties{OriginModelName: "Alias", UpstreamModelName: name}}
			if key == "sunoapi" {
				task.Properties.UpstreamModelName = "suno_music"
				task.Data = []byte(`[{"id":"song-1","audio_url":"https://private-provider.example/one.mp3","text":"lyrics"}]`)
			}
			response, err := publicResponse(t.Context(), h, task)
			require.NoError(t, err)
			require.Equal(t, "completed", response["status"])
			require.Equal(t, "Alias", response["model"])
			raw, err := json.Marshal(response)
			require.NoError(t, err)
			require.NotContains(t, string(raw), "private-provider")
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			writer := publicEventWriter{c: c}
			require.NoError(t, writer.finish(response))
			require.Contains(t, rec.Body.String(), "event: response.completed")
			if key == "sunoapi" {
				require.Contains(t, string(raw), "/v1/responses/local-id/content/audio-")
			}
		})
	}
}

func reloadPublicTask(t *testing.T, db *gorm.DB, id int64) *model.Task {
	t.Helper()
	var task model.Task
	require.NoError(t, db.First(&task, id).Error)
	return &task
}
