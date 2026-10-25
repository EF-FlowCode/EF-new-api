package service

import (
	"net/http/httptest"
	"testing"

	"eflow.local/new-api/dto"
	relaycommon "eflow.local/new-api/relay/common"
	"eflow.local/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestBillingSessionSettleOverdrawsWalletInsteadOfDroppingCharge(t *testing.T) {
	truncate(t)
	seedUser(t, 92, 1000)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	relayInfo := &relaycommon.RelayInfo{UserId: 92, IsPlayground: true, ForcePreConsume: true}
	session, apiErr := NewBillingSession(ctx, relayInfo, 100)
	require.Nil(t, apiErr)
	require.Equal(t, 900, getUserQuota(t, 92))

	require.NoError(t, session.Settle(1500))
	require.Equal(t, -500, getUserQuota(t, 92))

	_, apiErr = NewBillingSession(ctx, &relaycommon.RelayInfo{UserId: 92, IsPlayground: true}, 1)
	require.NotNil(t, apiErr, "a negative wallet must block new paid requests")
}

func TestBillingSessionReserveRejectsInsufficientWallet(t *testing.T) {
	truncate(t)
	seedUser(t, 93, 1000)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	relayInfo := &relaycommon.RelayInfo{UserId: 93, IsPlayground: true, ForcePreConsume: true}
	session, apiErr := NewBillingSession(ctx, relayInfo, 100)
	require.Nil(t, apiErr)

	err := session.Reserve(2000)
	require.Error(t, err)
	var reserveErr *types.NewAPIError
	require.ErrorAs(t, err, &reserveErr)
	require.Equal(t, types.ErrorCodeInsufficientUserQuota, reserveErr.GetErrorCode())
	require.Equal(t, 900, getUserQuota(t, 93))
	require.Equal(t, 100, session.GetPreConsumedQuota())

	require.NoError(t, session.Reserve(600))
	require.Equal(t, 400, getUserQuota(t, 93))
	require.Equal(t, 600, session.GetPreConsumedQuota())
}

func TestRealtimeSessionReservesCumulativeUsageAndSettlesOnce(t *testing.T) {
	truncate(t)
	seedUser(t, 94, 10_000_000)
	seedToken(t, 94, 94, "realtime-floor", 10_000_000)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	relayInfo := &relaycommon.RelayInfo{
		UserId:          94,
		TokenId:         94,
		TokenKey:        "realtime-floor",
		OriginModelName: "gpt-4o-realtime-preview",
		UsingGroup:      "default",
		ForcePreConsume: true,
	}
	session, apiErr := NewBillingSession(ctx, relayInfo, 0)
	require.Nil(t, apiErr)
	relayInfo.Billing = session

	total := &dto.RealtimeUsage{}
	for range 2 {
		total.TotalTokens += 1000
		total.InputTokens += 1000
		total.InputTokenDetails.TextTokens += 1000
		require.NoError(t, PreWssConsumeQuota(ctx, relayInfo, total))
	}
	reserved := session.GetPreConsumedQuota()
	require.Positive(t, reserved)
	require.Equal(t, 10_000_000-reserved, getUserQuota(t, 94), "mid-session segments must not be charged on top of the reservation")

	require.NoError(t, PreWssConsumeQuota(ctx, relayInfo, total))
	require.Equal(t, reserved, session.GetPreConsumedQuota(), "unchanged cumulative usage reserves nothing new")

	require.NoError(t, SettleBilling(ctx, relayInfo, reserved))
	require.Equal(t, 10_000_000-reserved, getUserQuota(t, 94))
	require.Equal(t, 10_000_000-reserved, getTokenRemainQuota(t, 94))
}
