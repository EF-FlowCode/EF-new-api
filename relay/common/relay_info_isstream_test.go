package common

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"eflow.local/new-api/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestGenRelayInfoGeminiPreservesStreamDetection(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name string
		url  string
		want bool
	}{
		{name: "alt sse", url: "/v1beta/models/gemini-test:generateContent?alt=sse", want: true},
		{name: "stream action", url: "/v1beta/models/gemini-test:streamGenerateContent", want: true},
		{name: "non stream", url: "/v1beta/models/gemini-test:generateContent", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request, _ = http.NewRequest(http.MethodPost, tt.url, nil)

			info := GenRelayInfoGemini(ctx, &dto.GeminiChatRequest{})

			assert.Equal(t, tt.want, info.IsStream)
		})
	}
}
