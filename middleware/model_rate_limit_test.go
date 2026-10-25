package middleware

import (
	"net/http/httptest"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/setting"
	"github.com/gin-gonic/gin"
)

func TestCheckUserModelRPMSeparatesUsersAndModels(t *testing.T) {
	previousConfig := setting.ModelRPMConfig2JSONString()
	previousRedisEnabled := common.RedisEnabled
	previousLimiter := inMemoryRateLimiter
	t.Cleanup(func() {
		common.RedisEnabled = previousRedisEnabled
		inMemoryRateLimiter = previousLimiter
		if err := setting.UpdateModelRPMConfigByJSONString(previousConfig); err != nil {
			t.Fatalf("restore model RPM config: %v", err)
		}
	})

	common.RedisEnabled = false
	inMemoryRateLimiter = &common.InMemoryRateLimiter{}
	if err := setting.UpdateModelRPMConfigByJSONString(`{
		"enabled": true,
		"models": {
			"test-model-a": {"global_rpm": 1},
			"test-model-b": {"global_rpm": 1}
		}
	}`); err != nil {
		t.Fatalf("update model RPM config: %v", err)
	}

	newContext := func(userID int) *gin.Context {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
		ctx.Set(string(constant.ContextKeyUserId), userID)
		common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
		common.SetContextKey(ctx, constant.ContextKeyUserAdditionalCustomerTiers, []string{})
		return ctx
	}

	if err := CheckUserModelRPM(newContext(1), "test-model-a"); err != nil {
		t.Fatalf("first request should pass: %v", err)
	}
	if err := CheckUserModelRPM(newContext(1), "test-model-a"); err == nil || err.StatusCode != 429 {
		t.Fatalf("second request should be rate limited, got %v", err)
	}
	if err := CheckUserModelRPM(newContext(2), "test-model-a"); err != nil {
		t.Fatalf("different user should have a separate limit: %v", err)
	}
	if err := CheckUserModelRPM(newContext(1), "test-model-b"); err != nil {
		t.Fatalf("different model should have a separate limit: %v", err)
	}
}
