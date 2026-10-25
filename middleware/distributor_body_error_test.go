package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/i18n"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// TestDistributeReportsBodyReadCause covers a request body that cannot be
// read: an oversized body gets 413 with the limit in the message, and the
// "invalid request" prefix appears once instead of being repeated.
func TestDistributeReportsBodyReadCause(t *testing.T) {
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())
	old := constant.MaxRequestBodyMB
	t.Cleanup(func() { constant.MaxRequestBodyMB = old })
	constant.MaxRequestBodyMB = 1

	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantCause  string
	}{
		{name: "too large", body: `{"model":"gpt-4o","x":"` + strings.Repeat("a", 2<<20) + `"}`, wantStatus: http.StatusRequestEntityTooLarge, wantCause: "request body exceeds 1 MB"},
		{name: "invalid json", body: `{"model":`, wantStatus: http.StatusBadRequest, wantCause: "invalid JSON request body"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Next()
				common.CleanupBodyStorage(c)
			})
			router.Use(Distribute())
			router.POST("/v1/chat/completions", func(c *gin.Context) { c.Status(http.StatusNoContent) })

			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(tt.body))
			request.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(recorder, request)

			require.Equal(t, tt.wantStatus, recorder.Code)
			body := recorder.Body.String()
			require.Contains(t, body, tt.wantCause)
			require.Equal(t, 1, strings.Count(body, "Invalid request")+strings.Count(body, "无效的请求"), body)
		})
	}
}

// TestPinGenericTaskReportsBodyReadCause covers the generic task route, which
// used to answer "model is required" for a body it could not read.
func TestPinGenericTaskReportsBodyReadCause(t *testing.T) {
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())
	old := constant.MaxRequestBodyMB
	t.Cleanup(func() { constant.MaxRequestBodyMB = old })
	constant.MaxRequestBodyMB = 1

	router := gin.New()
	router.POST("/v1/tasks/:key", PinGenericTask(), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/tasks/demo", strings.NewReader(`{"model":"m","x":"`+strings.Repeat("a", 2<<20)+`"}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
	require.Contains(t, recorder.Body.String(), "request body exceeds 1 MB")
}
