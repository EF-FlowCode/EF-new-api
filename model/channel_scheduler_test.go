package model

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/service/multi_key_scheduler"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGetNextEnabledKeyForModelReturnsRetryableErrorWhenSchedulerIsFull(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)

	const (
		channelID = 987654321
		modelName = "scheduler-full-test-model"
		key       = "scheduler-full-test-key"
	)
	cfg := &dto.SchedulerConfig{
		Enabled: true,
		Policy:  dto.SchedulerPolicy{OnLimit: multi_key_scheduler.OnLimitSkip},
		Models: map[string]dto.ModelQuota{
			modelName: {ConcurrencyMax: 1},
		},
	}
	channel := &Channel{
		Id:   channelID,
		Key:  key,
		Keys: []string{key},
		ChannelInfo: ChannelInfo{
			IsMultiKey:      true,
			SchedulerConfig: cfg,
		},
	}

	_, _, firstErr := channel.GetNextEnabledKeyForModel(c, modelName)
	require.Nil(t, firstErr)
	t.Cleanup(func() {
		multi_key_scheduler.ReportError(c.Request.Context(), channelID, multi_key_scheduler.KeyFingerprint(key), modelName, 0, "", cfg)
	})

	selected, _, secondErr := channel.GetNextEnabledKeyForModel(c, modelName)
	require.Empty(t, selected)
	require.NotNil(t, secondErr)
	require.False(t, secondErr.IsSkipRetry())
}
