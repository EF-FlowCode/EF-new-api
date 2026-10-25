package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"eflow.local/new-api/dto"
	"eflow.local/new-api/pkg/billingexpr"
	relaycommon "eflow.local/new-api/relay/common"
	"eflow.local/new-api/service"
	"eflow.local/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestReconcileOaiImageCountProtectsClientAbortBilling(t *testing.T) {
	newInfo := func(requested float64) *relaycommon.RelayInfo {
		return &relaycommon.RelayInfo{PriceData: types.PriceData{
			UsePrice:    true,
			OtherRatios: map[string]float64{"n": requested},
		}}
	}

	info := newInfo(3)
	reconcileOaiImageCount(info, 1, false)
	require.Equal(t, 3.0, info.PriceData.OtherRatios["n"])

	info = newInfo(1)
	reconcileOaiImageCount(info, 2, false)
	require.Equal(t, 2.0, info.PriceData.OtherRatios["n"])

	info = newInfo(3)
	reconcileOaiImageCount(info, 2, true)
	require.Equal(t, 2.0, info.PriceData.OtherRatios["n"])
}

// 按上游 e2bb05f70：Images API 的 output_tokens_details 要并入 completion_tokens_details，
// 图片输出 token 才能进入 img_o。
func TestOpenAIImageUsageMapsOutputTokenDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := `{"created":1,"data":[{"b64_json":"aGk="}],"usage":{"input_tokens":50,"output_tokens":4160,"total_tokens":4210,` +
		`"input_tokens_details":{"text_tokens":50,"image_tokens":0},"output_tokens_details":{"text_tokens":0,"image_tokens":4160}}}`

	t.Run("non-stream", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
		resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
		usage, apiErr := OpenaiHandlerWithUsage(c, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}, resp)
		require.Nil(t, apiErr)
		require.Equal(t, 4160, usage.CompletionTokens)
		require.Equal(t, 4160, usage.CompletionTokenDetails.ImageTokens)
		params := service.BuildTieredTokenParams(usage, false, billingexpr.UsedVars("tier(\"base\", p * 5 + c * 10 + img_o * 40)"))
		require.Equal(t, 4160.0, params.ImgO)
		require.Equal(t, 0.0, params.C)
	})

	t.Run("compatible upstream keeps chat-style details", func(t *testing.T) {
		usage := &dto.Usage{
			CompletionTokenDetails: dto.OutputTokenDetails{ImageTokens: 7},
			OutputTokensDetails:    &dto.OutputTokenDetails{ImageTokens: 9, TextTokens: 3},
		}
		fillOutputTokenDetailsFromImageUsage(usage)
		require.Equal(t, 7, usage.CompletionTokenDetails.ImageTokens)
		require.Equal(t, 3, usage.CompletionTokenDetails.TextTokens)
	})
}
