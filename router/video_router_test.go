package router

import (
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestVideoRoutesDropBrokenOfficialAdapters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetVideoRouter(engine)

	submissions := map[string]bool{
		"/v1/video/generations":      false,
		"/v1/videos":                 false,
		"/v1/videos/:video_id/remix": false,
	}
	for _, route := range engine.Routes() {
		require.False(t, strings.HasPrefix(route.Path, "/kling/") || strings.HasPrefix(route.Path, "/jimeng"), route.Path)
		if _, ok := submissions[route.Path]; ok && route.Method == http.MethodPost {
			submissions[route.Path] = true
		}
	}
	for path, found := range submissions {
		require.True(t, found, path)
	}
}
