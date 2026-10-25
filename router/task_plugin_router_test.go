package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestTaskPluginManagementRoutesRequireAuthInAPIRouter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(sessions.Sessions("session", cookie.NewStore([]byte("plugin-router-integration-test"))))
	SetApiRouter(r)
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/plugin/task"},
		{http.MethodGet, "/api/plugin/task/runtime/status"},
		{http.MethodPut, "/api/plugin/task/runtime/settings"},
		{http.MethodPost, "/api/plugin/task"},
		{http.MethodPost, "/api/plugin/task/test/dryrun"},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{}`)))
			require.Equal(t, http.StatusUnauthorized, w.Code)
			require.Contains(t, w.Body.String(), `"success":false`)
		})
	}
}
