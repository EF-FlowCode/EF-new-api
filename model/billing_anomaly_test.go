package model

import (
	"context"
	"testing"

	"eflow.local/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestListBillingAnomaliesFiltersAndMarksRefunds(t *testing.T) {
	u, token, _ := setupUsageRefundTest(t)
	other := User{Username: "other-user", Role: common.RoleCommonUser, Quota: 100, AffCode: "anomaly-other"}
	require.NoError(t, DB.Create(&other).Error)
	logs := []Log{
		{UserId: u.Id, Username: u.Username, Type: LogTypeConsume, Quota: 40, TokenId: token.Id, CreatedAt: 200, BillingAnomaly: LogBillingAnomalyInputEstimated, Other: `{"stream_status":{"end_reason":"client_gone"}}`},
		{UserId: u.Id, Username: u.Username, Type: LogTypeConsume, Quota: 0, TokenId: token.Id, CreatedAt: 201, BillingAnomaly: LogBillingAnomalyNoUpstreamUsage},
		{UserId: other.Id, Username: other.Username, Type: LogTypeConsume, Quota: 9, CreatedAt: 202, BillingAnomaly: LogBillingAnomalyOutputEstimated},
		// 正常日志与时间窗外的异常都不应出现。
		{UserId: u.Id, Username: u.Username, Type: LogTypeConsume, Quota: 11, CreatedAt: 203},
		{UserId: u.Id, Username: u.Username, Type: LogTypeConsume, Quota: 12, CreatedAt: 999, BillingAnomaly: LogBillingAnomalyInputEstimated},
	}
	require.NoError(t, LOG_DB.Create(&logs).Error)

	query := BillingAnomalyQuery{Start: 150, End: 500}
	got, err := ListBillingAnomalies(context.Background(), 99, common.RoleRootUser, query)
	require.NoError(t, err)
	require.Len(t, got.Items, 3)
	// 按日志 ID 倒序；审核场景下 client_gone 不作为跳过理由。
	require.Equal(t, logs[2].Id, got.Items[0].LogID)
	require.Equal(t, "not_charged", got.Items[1].Status)
	require.Equal(t, "eligible", got.Items[2].Status)
	require.Equal(t, LogBillingAnomalyInputEstimated, got.Items[2].Anomaly)
	require.Len(t, got.Users, 2)
	require.Equal(t, u.Id, got.Users[0].UserID)
	require.Equal(t, 2, got.Users[0].Count)
	require.Equal(t, 40, got.Users[0].Quota)

	// 返还后回显批次与已返还状态。
	v, err := PreviewUsageRefund(context.Background(), 99, common.RoleRootUser, UsageRefundSelection{LogIDs: []int{logs[0].Id}, Reason: "anomaly", IncludeClientGone: true})
	require.NoError(t, err)
	require.Equal(t, 1, v.Count)
	_, err = ExecuteUsageRefund(context.Background(), v.ID, 99, common.RoleRootUser, 40, 1)
	require.NoError(t, err)

	query.Anomaly = LogBillingAnomalyInputEstimated
	query.UserID = u.Id
	got, err = ListBillingAnomalies(context.Background(), 99, common.RoleRootUser, query)
	require.NoError(t, err)
	require.Len(t, got.Items, 1)
	require.Equal(t, "already_refunded", got.Items[0].Status)
	require.Equal(t, v.ID, got.Items[0].RefundBatchID)
	require.Equal(t, 40, got.Users[0].RefundedQuota)
}

func TestListBillingAnomaliesRejectsInvalidQuery(t *testing.T) {
	setupUsageRefundTest(t)
	ctx := context.Background()
	_, err := ListBillingAnomalies(ctx, 99, common.RoleAdminUser, BillingAnomalyQuery{Start: 1, End: 2})
	require.ErrorIs(t, err, ErrUsageRefundForbidden)
	for _, q := range []BillingAnomalyQuery{
		{Start: 0, End: 10},
		{Start: 10, End: 5},
		{Start: 1, End: 1 + 32*86400},
		{Start: 1, End: 2, Anomaly: 9},
	} {
		_, err = ListBillingAnomalies(ctx, 99, common.RoleRootUser, q)
		require.ErrorIs(t, err, ErrUsageRefundInvalid)
	}
}

// 标记上线前的日志 billing_anomaly 全为 0，「输出 ≤1」模式不看标记也要能找出来。
func TestListBillingAnomaliesLowOutputIncludesUnflagged(t *testing.T) {
	u, token, _ := setupUsageRefundTest(t)
	logs := []Log{
		{UserId: u.Id, Username: u.Username, Type: LogTypeConsume, Quota: 900, TokenId: token.Id, CreatedAt: 300, PromptTokens: 3000000, CompletionTokens: 0},
		{UserId: u.Id, Username: u.Username, Type: LogTypeConsume, Quota: 8, TokenId: token.Id, CreatedAt: 301, CompletionTokens: 1, BillingAnomaly: LogBillingAnomalyOutputEstimated},
		// 输出正常、免单、非消费日志都不应出现。
		{UserId: u.Id, Username: u.Username, Type: LogTypeConsume, Quota: 50, TokenId: token.Id, CreatedAt: 302, CompletionTokens: 2},
		{UserId: u.Id, Username: u.Username, Type: LogTypeConsume, Quota: 0, CreatedAt: 303, BillingAnomaly: LogBillingAnomalyNoUpstreamUsage},
		{UserId: u.Id, Username: u.Username, Type: LogTypeError, Quota: 0, CreatedAt: 304},
	}
	require.NoError(t, LOG_DB.Create(&logs).Error)

	got, err := ListBillingAnomalies(context.Background(), 99, common.RoleRootUser, BillingAnomalyQuery{Start: 250, End: 400, LowOutput: true})
	require.NoError(t, err)
	require.Len(t, got.Items, 2)
	require.Equal(t, logs[1].Id, got.Items[0].LogID)
	require.Equal(t, logs[0].Id, got.Items[1].LogID)
	require.Equal(t, LogBillingAnomalyNone, got.Items[1].Anomaly)
	require.Equal(t, "eligible", got.Items[1].Status)

	_, err = ListBillingAnomalies(context.Background(), 99, common.RoleRootUser, BillingAnomalyQuery{Start: 250, End: 400, LowOutput: true, Anomaly: LogBillingAnomalyInputEstimated})
	require.ErrorIs(t, err, ErrUsageRefundInvalid)
}
