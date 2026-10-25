package controller

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/internal/testdb"
	"eflow.local/new-api/model"
	"eflow.local/new-api/plugins"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
	"eflow.local/new-api/service"
	management "eflow.local/new-api/service/taskplugin"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func observationTask(mode string) *model.Task {
	return &model.Task{TaskID: "observe", UserId: 1, Status: model.TaskStatusSubmitted, PrivateData: model.TaskPrivateData{PluginContext: &model.TaskPluginContext{PublicMode: mode, PublicBackground: true}}}
}

func TestPublicObservationHeartbeatAndSerialRefresh(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
		task := observationTask("stream")
		cfg := constant.TaskProtocolConfig{Timeout: 40 * time.Second, Tick: 14 * time.Second, Heartbeat: 15 * time.Second}
		calls := 0
		start := time.Now()
		err := observePublicTask(c, task, cfg, func(ctx context.Context, initial bool, previous any) publicObservation {
			calls++
			if initial {
				return publicObservation{task: task, previous: "seen"}
			}
			require.Equal(t, "seen", previous)
			require.Equal(t, 14*time.Second, time.Since(start))
			select {
			case <-time.After(4 * time.Second):
			case <-ctx.Done():
				t.Fatal("read timed out prematurely")
			}
			return publicObservation{task: task, terminal: true, response: map[string]any{"status": "completed", "output": []any{}}}
		})
		require.Nil(t, err)
		require.Equal(t, 2, calls)
		require.Equal(t, 18*time.Second, time.Since(start))
		body := rec.Body.String()
		require.Equal(t, 1, strings.Count(body, ": heartbeat\n"))
		require.Contains(t, body, `"sequence_number":0`)
		require.Contains(t, body, `"sequence_number":1`)
		require.NotContains(t, body, `"sequence_number":2`)
		require.Less(t, strings.Index(body, ": heartbeat"), strings.Index(body, "event: response.completed"))
	})
}

func TestPublicObservationTimeoutCancellationAndReadError(t *testing.T) {
	for _, mode := range []string{"sync", "stream"} {
		for _, outcome := range []string{"timeout", "cancel", "read_error", "render_error", "slow_read", "panic"} {
			t.Run(mode+"/"+outcome, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					rec := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(rec)
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					c.Request = httptest.NewRequest("POST", "/v1/responses", nil).WithContext(ctx)
					cfg := constant.TaskProtocolConfig{Timeout: 4 * time.Second, Tick: time.Second, Heartbeat: 2 * time.Second}
					task := observationTask(mode)
					active, calls := 0, 0
					if outcome == "cancel" {
						go func() { time.Sleep(500 * time.Millisecond); cancel() }()
					}
					err := observePublicTask(c, task, cfg, func(ctx context.Context, initial bool, previous any) publicObservation {
						active++
						calls++
						defer func() { active-- }()
						if outcome == "panic" {
							panic("refresh failed")
						}
						require.Equal(t, 1, active)
						r := publicObservation{task: task}
						if outcome == "slow_read" {
							<-ctx.Done()
							r.err = service.TaskErrorWrapperLocal(ctx.Err(), "task_unavailable", 503)
						}
						if outcome == "read_error" || outcome == "render_error" {
							code := "task_unavailable"
							if outcome == "render_error" {
								code = "task_render_failed"
							}
							r.err = service.TaskErrorWrapperLocal(errors.New("hook/database failure"), code, 503)
						}
						return r
					})
					require.Zero(t, active)
					count := calls
					time.Sleep(10 * time.Second)
					require.Equal(t, count, calls)
					require.EqualValues(t, model.TaskStatusSubmitted, task.Status)
					if mode == "sync" {
						require.NotNil(t, err)
						if outcome == "timeout" || outcome == "cancel" {
							require.Equal(t, 504, err.StatusCode)
						}
					} else {
						require.Nil(t, err)
						if outcome == "timeout" {
							require.Contains(t, rec.Body.String(), "task_wait_timeout")
						}
						if outcome == "cancel" {
							require.NotContains(t, rec.Body.String(), "event: error")
						}
					}
				})
			})
		}
	}
	cfg := constant.TaskProtocol
	for tick := range uint64(100) {
		d := publicTickDelay(cfg, "task", tick)
		require.GreaterOrEqual(t, d, cfg.Tick)
		require.LessOrEqual(t, d, cfg.Tick+cfg.Jitter)
		require.Equal(t, d, publicTickDelay(cfg, "task", tick))
	}
}

func observationIdentity(c *gin.Context) {
	common.SetContextKey(c, constant.ContextKeyUserId, 1)
	common.SetContextKey(c, constant.ContextKeyTokenId, 1)
	c.Set(management.PublicRouteContextKey, &management.PublicRoute{Targets: map[int]management.PublicTarget{54: {Binding: model.TaskPluginProductionBinding{Key: "doubao"}}}})
}

func TestPublicObservationHookFailureAndCancellation(t *testing.T) {
	source, err := plugins.Source("doubao")
	require.NoError(t, err)
	for _, hook := range []string{`throw new Error("renderer failed");`, `while (true) {}`} {
		t.Run(hook, func(t *testing.T) {
			modified := strings.Replace(source, "renderEvents: function (ctx, task, previousState) {", "renderEvents: function (ctx, task, previousState) {"+hook, 1)
			require.NotEqual(t, source, modified)
			h, err := host.Compile(modified)
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
			defer cancel()
			start := time.Now()
			r := refreshPublicObservation(ctx, observationTask("stream"), h, true, true, nil)
			require.NotNil(t, r.err)
			require.Equal(t, "task_render_failed", r.err.Code)
			require.Less(t, time.Since(start), time.Second)
		})
	}
}

type observationRecorder struct {
	*httptest.ResponseRecorder
	flushErr error
	writes   int
}

func (r *observationRecorder) SetWriteDeadline(time.Time) error { return nil }
func (r *observationRecorder) FlushError() error                { return r.flushErr }
func (r *observationRecorder) Write(b []byte) (int, error) {
	r.writes++
	return r.ResponseRecorder.Write(b)
}

func TestPublicObservationAdmissionRelease(t *testing.T) {
	for _, outcome := range []string{"background", "submit_error", "panic", "unsupported_transport", "flush_error", "canceled"} {
		t.Run(outcome, func(t *testing.T) {
			limiter := newPluginProtocolObservationLimiter(defaultPluginProtocolObservationLimits)
			raw := &observationRecorder{ResponseRecorder: httptest.NewRecorder()}
			var transport http.ResponseWriter = raw
			if outcome == "unsupported_transport" {
				transport = raw.ResponseRecorder
			}
			if outcome == "flush_error" {
				raw.flushErr = io.ErrClosedPipe
			}
			c, _ := gin.CreateTestContext(transport)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			c.Request = httptest.NewRequest("POST", "/v1/responses", nil).WithContext(ctx)
			observationIdentity(c)
			called := false
			invoke := func() {
				relayPublicTaskWithAdmission(c, limiter, func(c *gin.Context) {
					called = true
					require.Equal(t, 1, limiter.global)
					switch outcome {
					case "panic":
						panic("submit panic")
					case "canceled":
						cancel()
						_, err := c.Writer.Write([]byte("test"))
						require.ErrorIs(t, err, context.Canceled)
					case "submit_error":
						c.JSON(400, gin.H{"error": "invalid request"})
					case "flush_error":
						w := publicEventWriter{c: c}
						require.ErrorIs(t, w.send("response.created", map[string]any{}), io.ErrClosedPipe)
						require.ErrorIs(t, w.send("error", map[string]any{}), io.ErrClosedPipe)
						require.Equal(t, 1, raw.writes, "never write again after transport failure")
					default:
						c.JSON(200, gin.H{"status": "queued"})
					}
				})
			}
			if outcome == "panic" {
				require.Panics(t, invoke)
			} else {
				invoke()
			}
			require.Equal(t, outcome != "unsupported_transport", called)
			require.Zero(t, limiter.global)
			require.Empty(t, limiter.plugin)
			require.Empty(t, limiter.user)
			require.Empty(t, limiter.token)
		})
	}
}

func TestPublicObservationAdmissionBeforeBillingMySQL(t *testing.T) {
	db := testdb.OpenMySQL8(t, &model.User{}, &model.Token{}, &model.Task{})
	old := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = old })
	require.NoError(t, db.Create(&model.User{Id: 1, Username: "observation", Quota: 123456}).Error)
	require.NoError(t, db.Create(&model.Token{Id: 1, UserId: 1, Key: "observation", RemainQuota: 654321}).Error)
	oldLimiter := pluginProtocolObservationAdmissions
	pluginProtocolObservationAdmissions = newPluginProtocolObservationLimiter(defaultPluginProtocolObservationLimits)
	t.Cleanup(func() { pluginProtocolObservationAdmissions = oldLimiter })
	for range 2 {
		release, err := pluginProtocolObservationAdmissions.acquire("doubao", 1, 1)
		require.NoError(t, err)
		defer release()
	}
	for _, body := range []string{`{"background":true}`, `{"stream":true,"background":true}`, `{}`} {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest("POST", "/v1/responses", strings.NewReader(body))
		observationIdentity(c)
		RelayPublicResponses(c)
		require.Equal(t, 429, rec.Code, rec.Body.String())
		called := false
		relayPublicTaskWithAdmission(c, pluginProtocolObservationAdmissions, func(*gin.Context) { called = true })
		require.False(t, called, "must not invoke submit/provider path")
	}
	var user model.User
	var token model.Token
	var count int64
	require.NoError(t, db.First(&user, 1).Error)
	require.Equal(t, 123456, user.Quota)
	require.NoError(t, db.First(&token, 1).Error)
	require.Equal(t, 654321, token.RemainQuota)
	require.NoError(t, db.Model(&model.Task{}).Count(&count).Error)
	require.Zero(t, count)
}

// A real HTTP transport is necessary: a recorder cannot exercise kernel buffers,
// HTTP/2 flow control, write deadlines, or cancellation of blocked writes.
func TestPublicObservationTransport(t *testing.T) {
	for _, h2 := range []bool{false, true} {
		for _, behavior := range []string{"slow_reader", "disconnect", "idle_then_write"} {
			t.Run(fmt.Sprintf("h2=%v/%s", h2, behavior), func(t *testing.T) {
				limiter := newPluginProtocolObservationLimiter(defaultPluginProtocolObservationLimits)
				done := make(chan error, 1)
				var protocol atomic.Int32
				server := httptest.NewUnstartedServer(http.HandlerFunc(func(raw http.ResponseWriter, req *http.Request) {
					protocol.Store(int32(req.ProtoMajor))
					var result error
					release, err := limiter.acquire("plugin", 1, 1)
					if err != nil {
						done <- err
						return
					}
					defer func() { release(); done <- result }()
					c, _ := gin.CreateTestContext(raw)
					c.Request = req
					w, err := newPublicDeadlineWriter(req.Context(), c.Writer, 100*time.Millisecond)
					if err != nil {
						result = err
						return
					}
					defer w.close()
					_, result = w.Write([]byte("started\n"))
					if result != nil {
						return
					}
					result = w.FlushError()
					if result != nil {
						return
					}
					if behavior == "idle_then_write" {
						time.Sleep(250 * time.Millisecond)
						_, result = w.Write([]byte("finished\n"))
						return
					}
					payload := []byte(strings.Repeat("x", 64*1024))
					for range 1024 {
						if _, result = w.Write(payload); result != nil {
							return
						}
						if result = w.FlushError(); result != nil {
							return
						}
					}
					result = errors.New("slow reader never blocked")
				}))
				server.EnableHTTP2 = h2
				server.StartTLS()
				defer server.Close()
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				req, err := http.NewRequestWithContext(ctx, "GET", server.URL, nil)
				require.NoError(t, err)
				resp, err := server.Client().Do(req)
				require.NoError(t, err)
				defer resp.Body.Close()
				if behavior == "disconnect" {
					cancel()
					resp.Body.Close()
				}
				if behavior == "idle_then_write" {
					body, err := io.ReadAll(resp.Body)
					require.NoError(t, err)
					require.Equal(t, "started\nfinished\n", string(body))
				}
				select {
				case err := <-done:
					if behavior == "idle_then_write" {
						require.NoError(t, err)
					} else {
						require.Error(t, err)
						require.NotEqual(t, "slow reader never blocked", err.Error())
					}
				// TLS close_notify can add its own fixed 5-second shutdown
				// deadline after net/http sees a write error (Go crypto/tls).
				case <-time.After(7 * time.Second):
					t.Fatal("write did not release slot")
				}
				limiter.mu.Lock()
				remaining := limiter.global
				limiter.mu.Unlock()
				require.Zero(t, remaining)
				want := int32(1)
				if h2 {
					want = 2
				}
				require.Equal(t, want, protocol.Load())
			})
		}
	}
}

func TestPublicObservationTransportConnectionReuse(t *testing.T) {
	for _, h2 := range []bool{false, true} {
		t.Run(fmt.Sprintf("h2=%v", h2), func(t *testing.T) {
			addresses := make(chan string, 2)
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(raw http.ResponseWriter, req *http.Request) {
				addresses <- req.RemoteAddr
				if req.URL.Path == "/task" {
					c, _ := gin.CreateTestContext(raw)
					w, err := newPublicDeadlineWriter(req.Context(), c.Writer, 50*time.Millisecond)
					if err != nil {
						http.Error(raw, err.Error(), 500)
						return
					}
					defer w.close()
					_, _ = w.Write([]byte("task"))
					return
				}
				time.Sleep(150 * time.Millisecond)
				_, _ = io.WriteString(raw, "ordinary")
			}))
			server.EnableHTTP2 = h2
			server.StartTLS()
			defer server.Close()
			for _, path := range []string{"task", "ordinary"} {
				resp, err := server.Client().Get(server.URL + "/" + path)
				require.NoError(t, err)
				body, err := io.ReadAll(resp.Body)
				resp.Body.Close()
				require.NoError(t, err)
				require.Equal(t, path, string(body))
			}
			require.Equal(t, <-addresses, <-addresses, "must exercise the same connection")
		})
	}
}
