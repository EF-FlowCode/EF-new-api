package ali

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/dto"
	relaycommon "eflow.local/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAliImageCountMatchesValidatedRequest(t *testing.T) {
	for _, params := range []string{"", `{}`, `{"n":3}`} {
		request := dto.ImageRequest{Model: "test-image", N: common.GetPointer(uint(2)), Size: "1024x1024"}
		if params != "" {
			request.Extra = map[string]json.RawMessage{"parameters": json.RawMessage(params)}
		}
		converted, err := oaiImage2AliImageRequest(&relaycommon.RelayInfo{}, request, true)
		require.NoError(t, err)
		want := 2
		if params == `{"n":3}` {
			want = 3
		}
		require.Equal(t, want, converted.Parameters.N)
		require.Equal(t, "1024*1024", converted.Parameters.Size)
	}
}

func TestAliImageResponseFormatComesFromRequest(t *testing.T) {
	r := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(r)
	c.Request = httptest.NewRequest("POST", "/v1/images/generations", nil)
	c.Set("response_format", "b64_json") // stale unrelated context must not override the request
	info := &relaycommon.RelayInfo{Request: &dto.ImageRequest{ResponseFormat: "url"}}
	resp := &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"output":{"results":[{"url":"not-a-fetchable-url"}]}}`))}
	err, _ := aliImageHandler(&Adaptor{IsSyncImageModel: true}, c, resp, info)
	require.Nil(t, err)
	var output dto.ImageResponse
	require.NoError(t, json.Unmarshal(r.Body.Bytes(), &output))
	require.Len(t, output.Data, 1)
	require.Equal(t, "not-a-fetchable-url", output.Data[0].Url)
}
