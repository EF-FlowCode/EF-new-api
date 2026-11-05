package service

import (
	"net/http/httptest"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	relaycommon "eflow.local/new-api/relay/common"

	"github.com/gin-gonic/gin"
)

func TestProtocolAttemptsForLogIncludesCurrentAttempt(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(c, constant.ContextKeyProtocolAttempts, []map[string]any{
		{"channel_id": 7, "mode": "native", "status": 400, "fallback_reason": "format_error"},
	})
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 7}, ProtocolAttemptMode: "chat_fallback"}

	attempts := protocolAttemptsForLog(c, info)
	if len(attempts) != 2 {
		t.Fatalf("attempts = %v", attempts)
	}
	if attempts[1]["mode"] != "chat_fallback" || attempts[1]["channel_id"] != 7 || attempts[1]["status"] != 200 {
		t.Fatalf("current attempt = %v", attempts[1])
	}
	stored, _ := common.GetContextKeyType[[]map[string]any](c, constant.ContextKeyProtocolAttempts)
	if len(stored) != 1 {
		t.Fatalf("context attempts changed: %v", stored)
	}
}

func TestProtocolAttemptsForLogWithoutProtocolRouting(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	if attempts := protocolAttemptsForLog(c, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 7}}); len(attempts) != 0 {
		t.Fatalf("attempts = %v", attempts)
	}
	if attempts := protocolAttemptsForLog(c, &relaycommon.RelayInfo{ProtocolAttemptMode: "native"}); len(attempts) != 0 {
		t.Fatalf("attempts without channel = %v", attempts)
	}
}
