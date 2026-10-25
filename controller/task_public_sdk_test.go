package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/internal/testconfig"
	"eflow.local/new-api/internal/testdb"
	"eflow.local/new-api/model"
	"eflow.local/new-api/plugins"
	"eflow.local/new-api/relay"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
	relaycommon "eflow.local/new-api/relay/common"
	"eflow.local/new-api/service"
	management "eflow.local/new-api/service/taskplugin"
	"eflow.local/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/responses"
	"github.com/openai/openai-go/shared"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func sdkTaskParams(background bool) responses.ResponseNewParams {
	return responses.ResponseNewParams{Model: shared.ResponsesModel("Movie"), Input: responses.ResponseNewParamsInputUnion{OfString: openai.String("a fox")}, Background: openai.Bool(background)}
}

func sdkTaskClient(server *httptest.Server) openai.Client {
	return openai.NewClient(option.WithAPIKey("test-key"), option.WithBaseURL(server.URL+"/v1/"), option.WithMaxRetries(0), option.WithHTTPClient(server.Client()))
}

func TestPublicSDKFinalResponses(t *testing.T) {
	for _, fixture := range []string{"custom", "kling", "empty"} {
		for _, streaming := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%v", fixture, streaming), func(t *testing.T) {
				var h *host.Handle
				if fixture == "kling" {
					source, err := plugins.Source("kling")
					require.NoError(t, err)
					h, err = host.Compile(source)
					require.NoError(t, err)
				} else {
					output := []any{}
					if fixture == "custom" {
						output = append(output, map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": "first"}, map[string]any{"type": "output_text", "text": "second"}}})
					}
					h = publicFinalFixture(t, map[string]any{"metadata": map[string]any{"vendor": "fixture", "number": 123, "task_id": "forged"}, "output": output})
				}
				task := observationTask("sync")
				task.Status = model.TaskStatusSuccess
				task.Properties.OriginModelName = "Movie"
				if streaming {
					task.PrivateData.PluginContext.PublicMode = "stream"
				}
				srv := httptest.NewServer(http.HandlerFunc(func(raw http.ResponseWriter, req *http.Request) {
					c, _ := gin.CreateTestContext(raw)
					c.Request = req
					out, err := publicResponse(req.Context(), h, task)
					if err != nil {
						http.Error(raw, err.Error(), 500)
						return
					}
					if !streaming {
						c.JSON(200, out)
						return
					}
					w, err := newPublicDeadlineWriter(req.Context(), c.Writer, time.Second)
					if err != nil {
						http.Error(raw, err.Error(), 500)
						return
					}
					c.Writer = w
					defer w.close()
					c.Header("Content-Type", "text/event-stream")
					events := publicEventWriter{c: c}
					_ = events.send("response.created", map[string]any{"response": publicTaskProgressResponse(task)})
					_, _ = fmt.Fprint(c.Writer, ": heartbeat\n")
					_ = flushPublicWriter(c.Writer)
					_ = events.finish(out)
				}))
				defer srv.Close()
				client := sdkTaskClient(srv)
				var final responses.Response
				if !streaming {
					out, err := client.Responses.New(t.Context(), sdkTaskParams(false))
					require.NoError(t, err)
					final = *out
				} else {
					stream := client.Responses.NewStreaming(t.Context(), sdkTaskParams(true))
					defer stream.Close()
					sequence := int64(0)
					completed := 0
					for stream.Next() {
						event := stream.Current()
						require.NotEmpty(t, event.Type, "heartbeat must not yield an SDK event")
						require.Equal(t, sequence, event.SequenceNumber)
						sequence++
						if event.Type == "response.completed" {
							final = event.Response
							completed++
						}
					}
					require.NoError(t, stream.Err())
					require.Equal(t, 1, completed)
				}
				require.Equal(t, "observe", final.ID)
				require.Equal(t, responses.ResponseStatusCompleted, final.Status)
				require.Equal(t, "observe", final.Metadata["task_id"])
				require.Equal(t, "/v1/responses/observe", final.Metadata["retrieval_path"])
				if fixture == "custom" {
					require.Equal(t, "firstsecond", final.OutputText())
					require.NotContains(t, final.Metadata, "number")
				}
				if fixture == "kling" {
					require.Contains(t, final.OutputText(), "/v1/videos/observe/content")
					require.Equal(t, "kling", final.Metadata["vendor"])
				}
				if fixture == "empty" {
					require.Empty(t, final.OutputText())
					require.Empty(t, final.Output)
				}
			})
		}
	}
}

func TestPublicSDKDurableLifecycleMySQL(t *testing.T) {
	db := testdb.OpenMySQL8(t, &model.Task{}, &model.User{}, &model.Token{}, &model.Log{}, &model.Channel{}, &model.TaskPlugin{}, &model.TaskPluginControl{}, &model.TaskPluginShadowBinding{}, &model.TaskPluginProductionBinding{}, &model.TaskPluginSourceSnapshot{}, &model.TaskPluginTaskReference{}, &model.TaskPluginSubmission{}, &model.TaskPluginReview{})
	oldDB, oldLogDB, oldManager := model.DB, model.LOG_DB, management.Default
	oldRedis, oldCache, oldBatch := common.RedisEnabled, common.MemoryCacheEnabled, common.BatchUpdateEnabled
	oldConfig := constant.TaskProtocol
	model.DB, model.LOG_DB = db, db
	common.RedisEnabled, common.MemoryCacheEnabled, common.BatchUpdateEnabled = false, false, false
	constant.TaskProtocol = constant.TaskProtocolConfig{Timeout: 600 * time.Millisecond, Tick: 25 * time.Millisecond, Heartbeat: 200 * time.Millisecond}
	t.Cleanup(func() {
		model.DB, model.LOG_DB, management.Default = oldDB, oldLogDB, oldManager
		common.RedisEnabled, common.MemoryCacheEnabled, common.BatchUpdateEnabled = oldRedis, oldCache, oldBatch
		constant.TaskProtocol = oldConfig
	})
	require.NoError(t, db.Create(&model.User{Id: 1, Username: "sdk", Quota: 100000000, Status: common.UserStatusEnabled}).Error)
	require.NoError(t, db.Create(&model.Token{Id: 1, UserId: 1, Key: "sdk-token", RemainQuota: 100000000, Status: common.TokenStatusEnabled}).Error)
	require.NoError(t, db.Create(&model.Channel{Id: 54, Type: 54, Status: common.ChannelStatusEnabled, Models: "Movie", ModelMapping: ptrSDK(`{"Movie":"doubao-seedance-1-0-lite-t2v"}`)}).Error)
	require.NoError(t, db.Create(&model.TaskPluginControl{Id: 1, Revision: 1, Enabled: true, FactoryEnabled: true, DisabledFactory: "[]"}).Error)
	source, err := plugins.Source("doubao")
	require.NoError(t, err)
	// Keep the provider contract, with a deterministic render fault on saved test data.
	source += `
const sdkRenderFinal = protocols.openai_responses.renderFinal;
protocols.openai_responses.renderFinal = function(ctx, task) {
 if (task.data && task.data.sdk_render_fail) throw new Error("SECRET renderer credential");
 return sdkRenderFinal(ctx, task);
};
const sdkRenderEvents = protocols.openai_responses.renderEvents;
protocols.openai_responses.renderEvents = function(ctx, task, previous) {
 if (task.data && task.data.sdk_render_fail) throw new Error("SECRET renderer credential");
 if (task.data && task.data.sdk_early_done) return {events:[],done:true};
 if (task.data && task.data.sdk_event_error) return {events:[{type:"error",code:"SECRET",message:"SECRET"}],done:false};
 return sdkRenderEvents(ctx, task, previous);
};`
	m := management.NewManager(model.SQLTaskPluginStore{DB: func() *gorm.DB { return db }}, map[string]string{"doubao": source})
	management.Default = m
	require.NoError(t, m.BindProduction(t.Context(), 54, management.ProductionBindingRequest{Key: "doubao", Version: "1.0.2", Factory: true, Enabled: true}))
	oldPrices := ratio_setting.ModelPrice2JSONString()
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(oldPrices)) })
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"Movie":0.0123}`))
	var sends atomic.Int32
	var unknownProvider atomic.Bool
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := sends.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if unknownProvider.Load() {
			_, _ = fmt.Fprint(w, `{}`)
			return
		}
		_, _ = fmt.Fprintf(w, `{"id":"provider-%d"}`, n)
	}))
	defer provider.Close()
	u, err := url.Parse(provider.URL)
	require.NoError(t, err)
	testconfig.Set(t, "fetch_setting", map[string]string{"enable_ssrf_protection": "true", "allow_private_ip": "true", "ip_filter_mode": "true", "ip_list": `["127.0.0.1/32"]`, "allowed_ports": fmt.Sprintf(`[%q]`, u.Port())})
	service.InitHttpClient()
	route, err := m.ResolvePublicRoute(t.Context(), "Movie", "/v1/responses")
	require.NoError(t, err)
	require.NotNil(t, route)
	limiter := newPluginProtocolObservationLimiter(defaultPluginProtocolObservationLimits)
	finish := func(id string, failed bool) error {
		var task model.Task
		if err := db.Where("task_id = ?", id).First(&task).Error; err != nil {
			return err
		}
		previous := task.Status
		task.Status = model.TaskStatusSuccess
		task.FinishTime = time.Now().Unix()
		quota := task.Quota
		if failed {
			task.Status = model.TaskStatusFailure
			task.FailReason = "SECRET provider credential"
			quota = 0
		}
		_, err := service.FinalizeTaskWithBilling(context.Background(), &task, previous, quota, "SDK fixture")
		return err
	}
	finishErrors := make(chan error, 16)
	var finishWorkers sync.WaitGroup
	defer finishWorkers.Wait()
	srv := httptest.NewServer(http.HandlerFunc(func(raw http.ResponseWriter, req *http.Request) {
		c, _ := gin.CreateTestContext(raw)
		c.Request = req
		user := 1
		if req.Header.Get("Authorization") == "Bearer other" {
			user = 2
		}
		common.SetContextKey(c, constant.ContextKeyUserId, user)
		common.SetContextKey(c, constant.ContextKeyTokenId, 1)
		if req.Method == "GET" {
			c.Params = gin.Params{{Key: "response_id", Value: strings.TrimPrefix(req.URL.Path, "/v1/responses/")}}
			RetrievePublicTaskResponse(c)
			return
		}
		c.Set(management.PublicRouteContextKey, route)
		common.SetContextKey(c, constant.ContextKeyChannelType, 54)
		common.SetContextKey(c, constant.ContextKeyChannelId, 54)
		common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, provider.URL)
		common.SetContextKey(c, constant.ContextKeyChannelKey, "provider-key")
		common.SetContextKey(c, constant.ContextKeyChannelModelMapping, `{"Movie":"doubao-seedance-1-0-lite-t2v"}`)
		relayPublicTaskWithAdmission(c, limiter, func(c *gin.Context) {
			info := &relaycommon.RelayInfo{UserId: 1, TokenId: 1, OriginModelName: "Movie", UsingGroup: "default"}
			result, e := relay.RelayTaskSubmit(c, info)
			if e != nil {
				respondTaskError(c, e)
				return
			}
			if mode := req.Header.Get("X-Fixture-Finish"); mode != "" {
				id := result.SavedTask.TaskID
				finishWorkers.Add(1)
				go func() {
					defer finishWorkers.Done()
					time.Sleep(350 * time.Millisecond)
					finishErrors <- finish(id, mode == "failed")
				}()
			}
			if e = completeTaskSubmission(c, info, result); e != nil {
				respondTaskError(c, e)
			}
		})
	}))
	defer srv.Close()
	client := sdkTaskClient(srv)
	assertBalances := func(want int) {
		var user model.User
		var token model.Token
		require.NoError(t, db.First(&user, 1).Error)
		require.NoError(t, db.First(&token, 1).Error)
		require.Equal(t, want, user.Quota)
		require.Equal(t, want, token.RemainQuota)
	}
	// Both requested modes must be rejected before durable reservation or send.
	for _, modes := range []string{`["stream", "sync"]`, `["sync", "background"]`} {
		limitedSource := strings.Replace(source, `["stream", "sync", "background"]`, modes, 1)
		if !strings.Contains(modes, "stream") {
			limitedSource += "\ndelete protocols.openai_responses.renderEvents;"
		}
		limited := management.NewManager(model.SQLTaskPluginStore{DB: func() *gorm.DB { return db }}, map[string]string{"doubao": limitedSource})
		management.Default = limited
		require.NoError(t, limited.BindProduction(t.Context(), 54, management.ProductionBindingRequest{Key: "doubao", Version: "1.0.2", Factory: true, Enabled: true}))
		route, err = limited.ResolvePublicRoute(t.Context(), "Movie", "/v1/responses")
		require.NoError(t, err)
		stream := client.Responses.NewStreaming(t.Context(), sdkTaskParams(true))
		require.False(t, stream.Next())
		var rejected *openai.Error
		require.ErrorAs(t, stream.Err(), &rejected)
		require.Equal(t, 400, rejected.StatusCode)
		require.NoError(t, stream.Close())
		assertBalances(100000000)
		require.Zero(t, sends.Load())
		var count int64
		require.NoError(t, db.Model(&model.Task{}).Count(&count).Error)
		require.Zero(t, count)
	}
	management.Default = m
	require.NoError(t, m.BindProduction(t.Context(), 54, management.ProductionBindingRequest{Key: "doubao", Version: "1.0.2", Factory: true, Enabled: true}))
	route, err = m.ResolvePublicRoute(t.Context(), "Movie", "/v1/responses")
	require.NoError(t, err)
	background, err := client.Responses.New(t.Context(), sdkTaskParams(true), option.WithHeader("Idempotency-Key", "sdk-background"))
	require.NoError(t, err)
	require.Equal(t, responses.ResponseStatusQueued, background.Status)
	require.True(t, background.Background)
	require.Equal(t, background.ID, background.Metadata["task_id"])
	replay, err := client.Responses.New(t.Context(), sdkTaskParams(true), option.WithHeader("Idempotency-Key", "sdk-background"))
	require.NoError(t, err)
	require.Equal(t, background.ID, replay.ID)
	require.EqualValues(t, 1, sends.Load())
	assertBalances(100000000 - 6150)
	pending, err := client.Responses.Get(t.Context(), background.ID, responses.ResponseGetParams{})
	require.NoError(t, err)
	require.Equal(t, responses.ResponseStatusQueued, pending.Status)
	_, err = client.Responses.Get(t.Context(), background.ID, responses.ResponseGetParams{}, option.WithAPIKey("other"))
	var apiErr *openai.Error
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, 404, apiErr.StatusCode)
	require.Empty(t, apiErr.Response.Header.Get("X-Task-ID"))
	// The same key with changed input is rejected without sending or reserving again.
	changed := sdkTaskParams(true)
	changed.Input.OfString = openai.String("different fox")
	_, err = client.Responses.New(t.Context(), changed, option.WithHeader("Idempotency-Key", "sdk-background"))
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, 409, apiErr.StatusCode)
	require.Equal(t, "plugin_idempotency_conflict", apiErr.Code)
	require.EqualValues(t, 1, sends.Load())
	assertBalances(100000000 - 6150)
	require.NoError(t, db.Model(&model.Task{}).Where("task_id = ?", background.ID).Update("status", model.TaskStatusBillingPending).Error)
	billingPending, err := client.Responses.Get(t.Context(), background.ID, responses.ResponseGetParams{})
	require.NoError(t, err)
	require.Equal(t, responses.ResponseStatusInProgress, billingPending.Status)
	require.Empty(t, billingPending.Output)
	assertBalances(100000000 - 6150)
	require.NoError(t, db.Model(&model.Task{}).Where("task_id = ?", background.ID).Update("status", model.TaskStatusSubmitted).Error)
	_, err = client.Responses.New(t.Context(), sdkTaskParams(false), option.WithHeader("Idempotency-Key", "sdk-timeout"))
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, 504, apiErr.StatusCode)
	require.Equal(t, "task_wait_timeout", apiErr.Code)
	require.EqualValues(t, 2, sends.Load())
	assertBalances(100000000 - 2*6150)
	var tasks []model.Task
	require.NoError(t, db.Order("id").Find(&tasks).Error)
	require.Len(t, tasks, 2)
	require.EqualValues(t, model.TaskStatusSubmitted, tasks[1].Status)
	require.NoError(t, finish(tasks[1].TaskID, false))
	completed, err := client.Responses.Get(t.Context(), tasks[1].TaskID, responses.ResponseGetParams{})
	require.NoError(t, err)
	require.Equal(t, responses.ResponseStatusCompleted, completed.Status)
	require.Contains(t, completed.OutputText(), "/v1/videos/")
	stream := client.Responses.NewStreaming(t.Context(), sdkTaskParams(true), option.WithHeader("Idempotency-Key", "sdk-stream"), option.WithHeader("X-Fixture-Finish", "true"))
	sequence := int64(0)
	var streamed responses.Response
	for stream.Next() {
		event := stream.Current()
		require.NotEmpty(t, event.Type)
		require.Equal(t, sequence, event.SequenceNumber)
		sequence++
		if event.Type == "response.created" {
			require.True(t, event.Response.Background)
			require.NotEmpty(t, event.Response.Metadata["retrieval_path"])
		}
		if event.Type == "response.completed" {
			streamed = event.Response
		}
	}
	require.NoError(t, stream.Err())
	stream.Close()
	require.Equal(t, responses.ResponseStatusCompleted, streamed.Status)
	require.NoError(t, <-finishErrors)
	require.EqualValues(t, 3, sends.Load())
	assertBalances(100000000 - 3*6150)
	require.NoError(t, finish(background.ID, true))
	failed, err := client.Responses.Get(t.Context(), background.ID, responses.ResponseGetParams{})
	require.NoError(t, err)
	require.Equal(t, responses.ResponseStatusFailed, failed.Status)
	require.NotContains(t, failed.Error.Message, "SECRET")
	assertBalances(100000000 - 2*6150)
	var saved model.Task
	require.NoError(t, db.Where("task_id = ?", background.ID).First(&saved).Error)
	require.Equal(t, "SECRET provider credential", saved.FailReason)
	// Query/database failure must remain distinct from both owner 404 and task failure.
	const callback = "sdk_read_failure"
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) { tx.AddError(errors.New("SECRET database outage")) }))
	_, err = client.Responses.Get(t.Context(), background.ID, responses.ResponseGetParams{})
	require.NoError(t, db.Callback().Query().Remove(callback))
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, 503, apiErr.StatusCode)
	require.Equal(t, "task_unavailable", apiErr.Code)
	require.NotContains(t, apiErr.Message, "SECRET")
	assertBalances(100000000 - 2*6150)
	require.EqualValues(t, 3, sends.Load())

	// Terminal failure is response.failed, not an observation error or a success.
	failedStream := client.Responses.NewStreaming(t.Context(), sdkTaskParams(true), option.WithHeader("Idempotency-Key", "sdk-failed-stream"), option.WithHeader("X-Fixture-Finish", "failed"))
	defer failedStream.Close()
	failedEvents := 0
	for failedStream.Next() {
		event := failedStream.Current()
		require.NotContains(t, event.RawJSON(), "SECRET")
		require.NotEqual(t, "response.completed", event.Type)
		if event.Type == "response.failed" {
			failedEvents++
			require.Equal(t, responses.ResponseStatusFailed, event.Response.Status)
		}
	}
	require.NoError(t, failedStream.Err())
	require.Equal(t, 1, failedEvents)
	require.NoError(t, <-finishErrors)
	assertBalances(100000000 - 2*6150)

	// Stream timeout is a typed error event; SDK Err alone is not sufficient.
	timeoutStream := client.Responses.NewStreaming(t.Context(), sdkTaskParams(true), option.WithHeader("Idempotency-Key", "sdk-stream-timeout"))
	defer timeoutStream.Close()
	timeoutEvents, timeoutID := 0, ""
	sequence = 0
	for timeoutStream.Next() {
		event := timeoutStream.Current()
		require.Equal(t, sequence, event.SequenceNumber)
		sequence++
		require.NotEqual(t, "response.completed", event.Type)
		if event.Type == "response.created" {
			timeoutID = event.Response.ID
		}
		if event.Type == "error" {
			timeoutEvents++
			var fields map[string]any
			require.NoError(t, json.Unmarshal([]byte(event.RawJSON()), &fields))
			require.Equal(t, "task_wait_timeout", fields["code"])
			require.Equal(t, timeoutID, fields["task_id"])
			require.Equal(t, "retrieve_task", fields["next_action"])
			require.Equal(t, "recorded", fields["submission_status"])
		}
	}
	require.NoError(t, timeoutStream.Err())
	require.Equal(t, 1, timeoutEvents)
	require.NotEmpty(t, timeoutID)
	assertBalances(100000000 - 3*6150)
	for _, fault := range []string{"sdk_early_done", "sdk_event_error"} {
		require.NoError(t, db.Model(&model.Task{}).Where("task_id = ?", timeoutID).Updates(map[string]any{"data": fmt.Sprintf(`{"%s":true}`, fault), "status": model.TaskStatusBillingPending}).Error)
		observed := client.Responses.NewStreaming(t.Context(), sdkTaskParams(true), option.WithHeader("Idempotency-Key", "sdk-stream-timeout"))
		errorsSeen := 0
		for observed.Next() {
			e := observed.Current()
			require.NotContains(t, e.RawJSON(), "SECRET")
			require.NotEqual(t, "response.completed", e.Type)
			require.NotEqual(t, "response.failed", e.Type)
			if e.Type == "error" {
				errorsSeen++
				require.Contains(t, e.RawJSON(), "retrieve_task")
			}
		}
		require.NoError(t, observed.Err())
		require.NoError(t, observed.Close())
		require.Equal(t, 1, errorsSeen)
		var unchanged model.Task
		require.NoError(t, db.Where("task_id = ?", timeoutID).First(&unchanged).Error)
		require.EqualValues(t, model.TaskStatusBillingPending, unchanged.Status)
		require.Equal(t, 6150, unchanged.Quota)
		assertBalances(100000000 - 3*6150)
		require.EqualValues(t, 5, sends.Load())
	}
	require.NoError(t, db.Model(&model.Task{}).Where("task_id = ?", timeoutID).Updates(map[string]any{"data": `{}`, "status": model.TaskStatusSubmitted}).Error)
	disconnectCtx, disconnect := context.WithCancel(t.Context())
	disconnected := client.Responses.NewStreaming(disconnectCtx, sdkTaskParams(true), option.WithHeader("Idempotency-Key", "sdk-stream-timeout"))
	require.True(t, disconnected.Next())
	require.Equal(t, "response.created", disconnected.Current().Type)
	disconnect()
	require.NoError(t, disconnected.Close())
	require.Eventually(t, func() bool { limiter.mu.Lock(); defer limiter.mu.Unlock(); return limiter.global == 0 }, time.Second, time.Millisecond)
	assertBalances(100000000 - 3*6150)
	require.EqualValues(t, 5, sends.Load())
	require.NoError(t, finish(timeoutID, false))
	afterTimeout, err := client.Responses.Get(t.Context(), timeoutID, responses.ResponseGetParams{})
	require.NoError(t, err)
	require.Equal(t, responses.ResponseStatusCompleted, afterTimeout.Status)

	// A renderer fault after successful billing cannot refund, resubmit or hide identity.
	renderTask, err := client.Responses.New(t.Context(), sdkTaskParams(true), option.WithHeader("Idempotency-Key", "sdk-render"))
	require.NoError(t, err)
	require.NoError(t, finish(renderTask.ID, false))
	require.NoError(t, db.Model(&model.Task{}).Where("task_id = ?", renderTask.ID).Update("data", `{"sdk_render_fail":true}`).Error)
	for attempt := range 2 {
		if attempt == 0 {
			_, err = client.Responses.Get(t.Context(), renderTask.ID, responses.ResponseGetParams{})
		} else {
			_, err = client.Responses.New(t.Context(), sdkTaskParams(true), option.WithHeader("Idempotency-Key", "sdk-render"))
		}
		require.ErrorAs(t, err, &apiErr)
		require.Equal(t, 500, apiErr.StatusCode)
		require.Equal(t, "task_render_failed", apiErr.Code)
		require.Equal(t, renderTask.ID, apiErr.Response.Header.Get("X-Task-ID"))
		require.NotContains(t, apiErr.Message, "SECRET")
		assertBalances(100000000 - 4*6150)
	}
	var renderedTask model.Task
	require.NoError(t, db.Where("task_id = ?", renderTask.ID).First(&renderedTask).Error)
	require.EqualValues(t, model.TaskStatusSuccess, renderedTask.Status)
	require.EqualValues(t, 6, sends.Load())

	// The same render fault after SSE headers is a typed error with the same identity.
	require.NoError(t, db.Model(&model.Task{}).Where("task_id = ?", streamed.ID).Update("data", `{"sdk_render_fail":true}`).Error)
	renderStream := client.Responses.NewStreaming(t.Context(), sdkTaskParams(true), option.WithHeader("Idempotency-Key", "sdk-stream"))
	defer renderStream.Close()
	renderErrors := 0
	for renderStream.Next() {
		event := renderStream.Current()
		require.NotContains(t, event.RawJSON(), "SECRET")
		require.NotEqual(t, "response.completed", event.Type)
		if event.Type == "error" {
			renderErrors++
			var fields map[string]any
			require.NoError(t, json.Unmarshal([]byte(event.RawJSON()), &fields))
			require.Equal(t, "task_render_failed", fields["code"])
			require.Equal(t, streamed.ID, fields["task_id"])
			require.Equal(t, "retrieve_task", fields["next_action"])
		}
	}
	require.NoError(t, renderStream.Err())
	require.Equal(t, 1, renderErrors)
	require.EqualValues(t, 6, sends.Load())
	assertBalances(100000000 - 4*6150)

	// A provider response without acceptance evidence stays unknown with one reservation.
	unknownProvider.Store(true)
	unknownID := ""
	for attempt := range 2 {
		_, err = client.Responses.New(t.Context(), sdkTaskParams(true), option.WithHeader("Idempotency-Key", "sdk-unknown"))
		require.ErrorAs(t, err, &apiErr)
		require.Equal(t, 409, apiErr.StatusCode)
		require.Equal(t, "plugin_submission_unknown", apiErr.Code)
		if attempt == 0 {
			unknownID = apiErr.Response.Header.Get("X-Task-ID")
		}
		require.NotEmpty(t, unknownID)
		require.Equal(t, unknownID, apiErr.Response.Header.Get("X-Task-ID"))
		require.Contains(t, apiErr.Message, "Do not submit with a new key")
		assertBalances(100000000 - 5*6150)
	}
	unknown, err := client.Responses.Get(t.Context(), unknownID, responses.ResponseGetParams{})
	require.NoError(t, err)
	require.Equal(t, responses.ResponseStatusInProgress, unknown.Status)
	require.EqualValues(t, 7, sends.Load())
	finishWorkers.Wait()
	limiter.mu.Lock()
	remaining := limiter.global
	limiter.mu.Unlock()
	require.Zero(t, remaining)
}

func ptrSDK(s string) *string { return &s }
