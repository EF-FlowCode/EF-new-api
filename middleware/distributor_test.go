package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relayconstant "eflow.local/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestDistributeAllowsRequestsThatDoNotSelectChannel(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name          string
		method        string
		route         string
		target        string
		body          string
		wantRelayMode int
	}{
		{
			name:          "video generation fetch",
			method:        http.MethodGet,
			route:         "/v1/video/generations/:task_id",
			target:        "/v1/video/generations/task_test",
			wantRelayMode: relayconstant.RelayModeVideoFetchByID,
		},
		{
			name:          "openai video fetch",
			method:        http.MethodGet,
			route:         "/v1/videos/:task_id",
			target:        "/v1/videos/task_test",
			wantRelayMode: relayconstant.RelayModeVideoFetchByID,
		},
		{
			name:          "suno fetch by id",
			method:        http.MethodGet,
			route:         "/suno/fetch/:id",
			target:        "/suno/fetch/task_test",
			wantRelayMode: relayconstant.RelayModeSunoFetchByID,
		},
		{
			name:          "suno batch fetch",
			method:        http.MethodPost,
			route:         "/suno/fetch",
			target:        "/suno/fetch",
			body:          `{"ids":[]}`,
			wantRelayMode: relayconstant.RelayModeSunoFetch,
		},
		{
			name:          "midjourney task fetch",
			method:        http.MethodGet,
			route:         "/mj/task/:id/fetch",
			target:        "/mj/task/task_test/fetch",
			wantRelayMode: relayconstant.RelayModeMidjourneyTaskFetch,
		},
		{
			name:          "openai video remix",
			method:        http.MethodPost,
			route:         "/v1/videos/:video_id/remix",
			target:        "/v1/videos/task_test/remix",
			wantRelayMode: relayconstant.RelayModeVideoSubmit,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reachedHandler := false
			router := gin.New()
			router.Use(Distribute())
			router.Handle(tt.method, tt.route, func(c *gin.Context) {
				reachedHandler = true
				require.Equal(t, tt.wantRelayMode, c.GetInt("relay_mode"))
				c.Status(http.StatusNoContent)
			})

			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(tt.method, tt.target, strings.NewReader(tt.body))
			if tt.body != "" {
				request.Header.Set("Content-Type", "application/json")
			}
			router.ServeHTTP(recorder, request)

			require.True(t, reachedHandler)
			require.Equal(t, http.StatusNoContent, recorder.Code)
		})
	}
}
