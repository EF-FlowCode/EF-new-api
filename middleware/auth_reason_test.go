package middleware

import (
	"encoding/json"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUserAuthReasonCodes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name, header, code string
		id                 any
	}{
		{"missing session", "1", "session_missing_or_invalid", nil},
		{"missing header", "", "user_id_required", 1},
		{"malformed header", "bad", "user_id_invalid", 1},
		{"account changed", "2", "user_id_mismatch", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router := gin.New()
			router.Use(sessions.Sessions("session", cookie.NewStore([]byte("local-reason-code-test"))))
			router.Use(func(c *gin.Context) {
				if tc.id != nil {
					sessions.Default(c).Set("id", tc.id)
				}
				c.Next()
			})
			router.GET("/self", UserAuth(), func(c *gin.Context) { t.Fatal("unauthenticated request reached handler") })
			req := httptest.NewRequest(http.MethodGet, "/self", nil)
			if tc.header != "" {
				req.Header.Set("New-Api-User", tc.header)
			}
			out := httptest.NewRecorder()
			router.ServeHTTP(out, req)
			var result struct {
				Success bool   `json:"success"`
				Code    string `json:"code"`
			}
			if err := json.Unmarshal(out.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if out.Code != 401 || result.Success || result.Code != tc.code {
				t.Fatalf("status=%d body=%s", out.Code, out.Body.String())
			}
			if out.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("authentication rejection must not be cached")
			}
		})
	}
}
