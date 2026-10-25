package service

import (
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"eflow.local/new-api/internal/testconfig"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestChannelAffinityRequestKeepsMatchedConfigSnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cacheKeySuffix := fmt.Sprintf("snapshot-policy:%d", time.Now().UnixNano())
	cache := getChannelAffinityCache()
	t.Cleanup(func() {
		_, _ = cache.DeleteMany([]string{cacheKeySuffix})
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	setChannelAffinityContext(ctx, channelAffinityMeta{
		CacheKey:              cache.FullKey(cacheKeySuffix),
		TTLSeconds:            60,
		SwitchOnSuccess:       false,
		FailoverOnChannelDown: true,
	})
	ctx.Set("channel_id", 202)

	// Simulate an option reload after channel selection. The in-flight request
	// must continue with the policy version that produced its affinity match.
	testconfig.Set(t, "channel_affinity_setting", map[string]string{
		"enabled":                  "false",
		"switch_on_success":        "true",
		"failover_on_channel_down": "false",
	})

	require.True(t, ChannelAffinityFailoverOnChannelDownEnabledForRequest(ctx))
	RecordChannelAffinity(ctx, 101)

	channelID, found, err := cache.Get(cacheKeySuffix)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, 101, channelID)
}
