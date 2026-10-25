package model

import (
	"net/http/httptest"
	"strings"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/dto"
	"eflow.local/new-api/service/multi_key_scheduler"
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

func TestGetNextEnabledKeyForModelDoesNotRaceMultiKeyStatusUpdates(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const (
		channelID = 987654322
		modelName = "scheduler-race-test-model"
	)
	keys := []string{"race-key-a", "race-key-b", "race-key-c", "race-key-d"}
	cfg := &dto.SchedulerConfig{
		Enabled: true,
		Policy:  dto.SchedulerPolicy{OnLimit: multi_key_scheduler.OnLimitSkip},
		Models:  map[string]dto.ModelQuota{modelName: {}},
	}
	channel := &Channel{
		Id:   channelID,
		Key:  strings.Join(keys, "\n"),
		Keys: keys,
		ChannelInfo: ChannelInfo{
			IsMultiKey:         true,
			MultiKeySize:       len(keys),
			MultiKeyStatusList: map[int]int{},
			SchedulerConfig:    cfg,
		},
	}

	// Before the fix the selector read MultiKeyStatusList without the polling
	// lock while key auto-disable rewrote it under the lock, which is a fatal
	// "concurrent map read and map write" that no recover can catch.
	done := make(chan struct{})
	go func() {
		defer close(done)
		lock := GetChannelPollingLock(channelID)
		for i := range 20000 {
			lock.Lock()
			if i%2 == 0 {
				handlerMultiKeyUpdate(channel, keys[i%len(keys)], common.ChannelStatusAutoDisabled, "race")
			} else {
				handlerMultiKeyUpdate(channel, keys[i%len(keys)], common.ChannelStatusEnabled, "")
			}
			lock.Unlock()
		}
	}()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	for {
		select {
		case <-done:
			return
		default:
			_, index, err := channel.GetNextEnabledKeyForModel(c, modelName)
			if err == nil {
				multi_key_scheduler.ReportError(c.Request.Context(), channelID, multi_key_scheduler.KeyFingerprint(keys[index]), modelName, 0, "", cfg)
			}
		}
	}
}
