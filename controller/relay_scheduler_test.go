package controller

import (
	"net/http/httptest"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/dto"
	"eflow.local/new-api/model"
	relaycommon "eflow.local/new-api/relay/common"
	"eflow.local/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGetChannelFirstAttemptKeepsCompleteSelectedChannel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)

	setting := `{"rate_limit":{"enabled":true,"concurrency":1}}`
	selected := &model.Channel{
		Id:      42,
		Name:    "complete-channel",
		Setting: &setting,
		ChannelInfo: model.ChannelInfo{
			IsMultiKey: true,
			SchedulerConfig: &dto.SchedulerConfig{
				Enabled: true,
				Models:  map[string]dto.ModelQuota{"test-model": {ConcurrencyMax: 1}},
			},
		},
	}
	common.SetContextKey(c, constant.ContextKeySelectedChannel, selected)

	got, apiErr := getChannel(c, &relaycommon.RelayInfo{}, &service.RetryParam{Retry: common.GetPointer(0)})
	require.Nil(t, apiErr)
	require.Same(t, selected, got)
	require.NotNil(t, got.GetSetting().RateLimit)
	require.True(t, got.ChannelInfo.IsMultiKey)
}
