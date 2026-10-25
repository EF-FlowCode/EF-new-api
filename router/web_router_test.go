package router

import (
	"embed"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestWebMissingAssetsDoNotReturnHTML(t *testing.T) {
	r := gin.New()
	SetWebRouter(r, WebAssets{BuildFS: embed.FS{}, IndexPage: []byte("<html>application</html>")})
	for _, path := range []string{"/static/js/old.123.js", "/static/css/old.123.css?retry=1", "/assets/old.js"} {
		t.Run(path, func(t *testing.T) {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
			if w.Code != http.StatusNotFound || w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), "<html>") {
				t.Fatalf("missing asset: status=%d cache=%q body=%q", w.Code, w.Header().Get("Cache-Control"), w.Body.String())
			}
		})
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/dashboard", nil))
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-cache" || w.Body.String() != "<html>application</html>" {
		t.Fatalf("SPA navigation must still serve revalidated HTML: %d %s", w.Code, w.Body.String())
	}
}
