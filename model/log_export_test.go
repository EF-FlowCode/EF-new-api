package model

import (
	"context"
	"testing"

	"eflow.local/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestUserLogExportUsesListFilters(t *testing.T) {
	u, _, _ := setupUsageRefundTest(t)
	extra := []Log{
		{UserId: u.Id, Type: LogTypeConsume, ModelName: "gpt-4o-mini", TokenName: "team-alpha", UpstreamRequestId: "up-1", CreatedAt: 200, Quota: 3},
		{UserId: u.Id, Type: LogTypeConsume, ModelName: "gpt-4o-mini", TokenName: "team-beta", UpstreamRequestId: "up-2", CreatedAt: 201, Quota: 4},
		{UserId: u.Id, Type: LogTypeConsume, ModelName: "claude-sonnet", TokenName: "team-alpha", CreatedAt: 202, Quota: 5},
	}
	require.NoError(t, LOG_DB.Create(&extra).Error)

	cases := []UserLogExportFilter{
		{UserId: u.Id, ModelName: "4o-mini"},
		{UserId: u.Id, TokenName: "alpha"},
		{UserId: u.Id, UpstreamRequestId: "up-2"},
	}
	for _, f := range cases {
		listed, _, err := GetUserLogs(u.Id, f.LogType, 0, 0, f.ModelName, f.TokenName, 0, 100, "", "", f.UpstreamRequestId, NewLogSortOptions("", ""))
		require.NoError(t, err)
		exported, truncated, err := GetUserLogsForExport(f)
		require.NoError(t, err)
		require.False(t, truncated)
		require.NotEmpty(t, listed)
		require.Len(t, exported, len(listed), "filter %+v", f)
	}
}

func TestUserLogExportMarksRefundedLogs(t *testing.T) {
	u, _, logs := setupUsageRefundTest(t)
	v, err := PreviewUsageRefund(context.Background(), 99, common.RoleRootUser, UsageRefundSelection{LogIDs: []int{logs[0].Id}, Reason: "test compensation"})
	require.NoError(t, err)
	_, err = ExecuteUsageRefund(context.Background(), v.ID, 99, common.RoleRootUser, v.Quota, v.Count)
	require.NoError(t, err)

	exported, _, err := GetUserLogsForExport(UserLogExportFilter{UserId: u.Id, LogType: LogTypeConsume})
	require.NoError(t, err)
	marked := 0
	for _, l := range exported {
		if l.RequestId == logs[0].RequestId {
			require.NotNil(t, l.UsageRefund)
			require.Equal(t, logs[0].Quota, l.UsageRefund.Quota)
			marked++
		} else {
			require.Nil(t, l.UsageRefund)
		}
	}
	require.Equal(t, 1, marked)
}

func TestEstimatedUserLogTotalKeepsNextPage(t *testing.T) {
	require.Equal(t, int64(21), estimatedUserLogTotal(0, 20, 20))
	require.Equal(t, int64(45), estimatedUserLogTotal(40, 20, 5))
	require.Equal(t, int64(0), estimatedUserLogTotal(0, 20, 0))
}
