package service

import (
	"net/http/httptest"
	"testing"

	relaycommon "eflow.local/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestBillingSessionWalletLifecycle(t *testing.T) {
	truncate(t)
	seedUser(t, 90, 1000)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	relayInfo := &relaycommon.RelayInfo{
		UserId:       90,
		IsPlayground: true,
	}
	session, apiErr := NewBillingSession(ctx, relayInfo, 100)
	require.Nil(t, apiErr)
	require.Equal(t, 900, getUserQuota(t, 90))

	require.NoError(t, session.Settle(150))
	require.Equal(t, 850, getUserQuota(t, 90))

	require.NoError(t, session.Compensate(ctx))
	require.Equal(t, 1000, getUserQuota(t, 90))
}
