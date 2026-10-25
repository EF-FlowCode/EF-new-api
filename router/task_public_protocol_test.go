package router

import (
	"eflow.local/new-api/i18n"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublicProtocolRoutesRequireAuth(t *testing.T) {
	require.NoError(t, i18n.Init())
	r := gin.New()
	SetRelayRouter(r)
	SetVideoRouter(r)
	for _, path := range []string{"/v1/responses/id", "/v1/responses/id/content/audio-unknown", "/v1/tasks/id", "/doubao/api/v3/contents/generations/tasks/id", "/kling/v1/videos/text2video/id"} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		require.Equal(t, 401, rec.Code, rec.Body.String())
	}
	for _, path := range []string{"/v1/responses", "/v1/videos", "/v1/tasks/doubao", "/doubao/api/v3/contents/generations/tasks", "/ali/api/v1/services/aigc/video-generation/video-synthesis", "/jimeng/", "/suno/submit/music"} {
		req := httptest.NewRequest("POST", path, strings.NewReader(`{"model":"Movie","input":"test"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, 401, rec.Code, rec.Body.String())
	}
}
