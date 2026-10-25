package service

import (
	"net/http/httptest"
	"strconv"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/setting"
	"eflow.local/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRelayRuntimePolicyDoesNotDriftAfterHotUpdate(t *testing.T) {
	originalAutoGroup := setting.GetAutoGroupPolicy()
	originalFailure := operation_setting.GetChannelFailurePolicy()
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateAutoGroupPolicy(map[string]string{
			"AutoGroups":          mustJSON(t, originalAutoGroup.Groups),
			"DefaultUseAutoGroup": strconv.FormatBool(originalAutoGroup.DefaultUseAutoGroup),
		}))
		require.NoError(t, operation_setting.UpdateChannelFailurePolicy(map[string]string{
			"AutomaticDisableChannelEnabled": strconv.FormatBool(originalFailure.AutomaticDisableChannelEnabled),
			"RetryTimes":                     strconv.Itoa(originalFailure.RetryTimes),
		}))
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
	})

	require.NoError(t, setting.UpdateAutoGroupPolicy(map[string]string{
		"AutoGroups": `["default"]`,
	}))
	require.NoError(t, operation_setting.UpdateChannelFailurePolicy(map[string]string{
		"AutomaticDisableChannelEnabled": "true",
		"RetryTimes":                     "1",
	}))

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(c, constant.ContextKeyTokenCrossGroupRetry, true)
	pinned := GetOrCreateRelayRuntimePolicy(c)

	require.NoError(t, setting.UpdateAutoGroupPolicy(map[string]string{
		"AutoGroups": `[]`,
	}))
	require.NoError(t, operation_setting.UpdateChannelFailurePolicy(map[string]string{
		"AutomaticDisableChannelEnabled": "false",
		"RetryTimes":                     "9",
	}))

	afterUpdate := GetOrCreateRelayRuntimePolicy(c)
	require.Equal(t, []string{"default"}, afterUpdate.AutoGroupPolicy.Groups)
	require.True(t, afterUpdate.ChannelFailurePolicy.AutomaticDisableChannelEnabled)
	require.Equal(t, 1, afterUpdate.ChannelFailurePolicy.RetryTimes)
	require.Equal(t, pinned, afterUpdate)

	common.MemoryCacheEnabled = true
	param := &RetryParam{
		Ctx:        c,
		TokenGroup: "auto",
		ModelName:  "snapshot-contract-model",
		Retry:      common.GetPointer(0),
	}
	_, _, err := CacheGetRandomSatisfiedChannelWithPolicy(param, afterUpdate)
	require.NoError(t, err, "the pinned non-empty group policy must remain usable")

	_, _, err = CacheGetRandomSatisfiedChannel(&RetryParam{
		Ctx:        c,
		TokenGroup: "auto",
		ModelName:  "snapshot-contract-model",
		Retry:      common.GetPointer(0),
	})
	require.ErrorContains(t, err, "auto groups is not enabled", "the compatibility entrypoint must use the current policy")
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := common.Marshal(value)
	require.NoError(t, err)
	return string(encoded)
}
