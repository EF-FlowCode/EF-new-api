package service

import (
	"net/http/httptest"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/dto"
	relaycommon "eflow.local/new-api/relay/common"
	relayconstant "eflow.local/new-api/relay/constant"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestClassifyBillingAnomaly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	usage := &dto.Usage{PromptTokens: 10, CompletionTokens: 5}
	chat := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeChatCompletions}

	newCtx := func() *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		return c
	}

	t.Run("zero settlement is no upstream usage", func(t *testing.T) {
		require.Equal(t, constant.BillingAnomalyNoUpstreamUsage, classifyBillingAnomaly(newCtx(), chat, usage, 0))
	})
	t.Run("normal upstream usage", func(t *testing.T) {
		require.Equal(t, constant.BillingAnomalyNone, classifyBillingAnomaly(newCtx(), chat, usage, 15))
	})
	t.Run("handler explicit mark wins", func(t *testing.T) {
		c := newCtx()
		common.SetContextKey(c, constant.ContextKeyLocalCountTokens, true)
		common.SetContextKey(c, constant.ContextKeyBillingAnomaly, constant.BillingAnomalyOutputEstimated)
		require.Equal(t, constant.BillingAnomalyOutputEstimated, classifyBillingAnomaly(c, chat, usage, 15))
	})
	t.Run("local count without mark means input estimated", func(t *testing.T) {
		c := newCtx()
		common.SetContextKey(c, constant.ContextKeyLocalCountTokens, true)
		require.Equal(t, constant.BillingAnomalyInputEstimated, classifyBillingAnomaly(c, chat, usage, 15))
	})
	t.Run("nil usage settled by estimate", func(t *testing.T) {
		require.Equal(t, constant.BillingAnomalyInputEstimated, classifyBillingAnomaly(newCtx(), chat, nil, 15))
	})
	t.Run("audio local count is regular billing", func(t *testing.T) {
		c := newCtx()
		common.SetContextKey(c, constant.ContextKeyLocalCountTokens, true)
		tts := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeAudioSpeech}
		require.Equal(t, constant.BillingAnomalyNone, classifyBillingAnomaly(c, tts, usage, 15))
	})
}
