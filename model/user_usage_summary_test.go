package model

import (
	"context"
	"encoding/json"
	"testing"

	"eflow.local/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestUserUsageSummaryAndRefundRecords(t *testing.T) {
	u, _, logs := setupUsageRefundTest(t)
	require.NoError(t, DB.AutoMigrate(&TicketRefund{}))
	require.NoError(t, DB.Model(&User{}).Where("id = ?", u.Id).Updates(map[string]any{"used_quota": 1000, "request_count": 12}).Error)
	v := previewUsageRefundTest(t, logs[0].Id, logs[1].Id)
	summary, err := GetUserUsageSummary(context.Background(), u.Id)
	require.NoError(t, err)
	require.EqualValues(t, 0, summary.RefundedQuota, "previews are not credits")
	_, err = ExecuteUsageRefund(context.Background(), v.ID, 99, common.RoleRootUser, v.Quota, v.Count)
	require.NoError(t, err)
	require.NoError(t, DB.Create(&UsageRefundReceipt{SourceLogID: 999, UserID: u.Id + 1, Quota: 99999, BatchID: v.ID}).Error)
	summary, err = GetUserUsageSummary(context.Background(), u.Id)
	require.NoError(t, err)
	require.EqualValues(t, 150, summary.Quota)
	require.EqualValues(t, 1000, summary.UsedQuota)
	require.EqualValues(t, 50, summary.RefundedQuota)
	require.EqualValues(t, 950, summary.NetUsedQuota)
	require.EqualValues(t, 12, summary.RequestCount)
	records, err := GetUserRefundRecords(context.Background(), u.Id, "usage", 1, 10)
	require.NoError(t, err)
	require.Len(t, records.Items, 1)
	require.EqualValues(t, 50, records.Items[0].Quota)

	for i := range 3 {
		require.NoError(t, LOG_DB.Create(&Log{UserId: u.Id, Type: LogTypeRefund, Quota: 20, CreatedAt: int64(200 + i), Other: `{"reason":"task failed","admin_info":{"secret":"private"}}`}).Error)
	}
	require.NoError(t, LOG_DB.Create(&Log{UserId: u.Id + 1, Type: LogTypeRefund, Quota: 9999}).Error)
	records, err = GetUserRefundRecords(context.Background(), u.Id, "task", 1, 2)
	require.NoError(t, err)
	require.Len(t, records.Items, 2)
	require.True(t, records.HasMore)
	require.Equal(t, "task failed", records.Items[0].Reason)
	require.EqualValues(t, 202, records.Items[0].CreatedAt)
	records, err = GetUserRefundRecords(context.Background(), u.Id, "task", 2, 2)
	require.NoError(t, err)
	require.Len(t, records.Items, 1, "bulk refund notice must not be counted again")
	require.False(t, records.HasMore)
	summary, err = GetUserUsageSummary(context.Background(), u.Id)
	require.NoError(t, err)
	require.EqualValues(t, 950, summary.NetUsedQuota, "automatic refunds are already represented by used_quota")

	require.NoError(t, DB.Create(&TicketRefund{TicketId: 1, UserId: u.Id, RefundQuota: 40, RefundStatus: RefundStatusPending, Reason: "requested withdrawal", PayeeAccount: "private-account"}).Error)
	require.NoError(t, DB.Create(&TicketRefund{TicketId: 2, UserId: u.Id + 1, RefundQuota: 9999, RefundStatus: RefundStatusRefunded}).Error)
	records, err = GetUserRefundRecords(context.Background(), u.Id, "ticket", 1, 10)
	require.NoError(t, err)
	require.Len(t, records.Items, 1)
	require.Equal(t, "pending", records.Items[0].Status)
	require.EqualValues(t, 40, records.Items[0].Quota)
	encoded, err := json.Marshal(records)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private")
	for _, kind := range []string{"usage", "task", "ticket"} {
		_, err = GetUserRefundRecords(context.Background(), u.Id, kind, -1, 10)
		require.Error(t, err)
	}
	_, err = GetUserUsageSummary(context.Background(), 0)
	require.Error(t, err)
}
