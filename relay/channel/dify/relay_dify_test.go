package dify

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"eflow.local/new-api/dto"
	relaycommon "eflow.local/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRequestOpenAI2DifyRemoteImage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	message := dto.Message{Role: "user"}
	message.SetMediaContent([]dto.MediaContent{{
		Type: dto.ContentTypeImageURL,
		ImageUrl: &dto.MessageImageUrl{
			Url:      "https://example.com/image.png",
			MimeType: "image/png",
		},
	}})
	request := dto.GeneralOpenAIRequest{
		User:     []byte(`"user-1"`),
		Messages: []dto.Message{message},
	}

	got := requestOpenAI2Dify(c, &relaycommon.RelayInfo{}, request)
	require.NotNil(t, got)
	require.Len(t, got.Files, 1)
	require.Equal(t, DifyFile{
		Type:         "image/png",
		TransferMode: "remote_url",
		URL:          "https://example.com/image.png",
	}, got.Files[0])
}
