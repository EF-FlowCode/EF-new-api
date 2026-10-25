package model

import (
	"net/http/httptest"
	"testing"

	"eflow.local/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestResetStaleChannelMonitoringStatsMarksChannelsWithoutTraffic(t *testing.T) {
	db := useModelIntegrationTestDB(t, &ChannelMonitoringStat{})
	require.NoError(t, db.Create(&[]ChannelMonitoringStat{
		{GroupName: "vip", ChannelId: 1, AvailabilityRate: 99, CacheHitRate: 40, AvgResponseTime: 800, IsOnline: true, UpdatedAt: 100},
		{GroupName: "vip", ChannelId: 2, AvailabilityRate: 97, CacheHitRate: 30, IsOnline: true, UpdatedAt: 500},
		{GroupName: "other", ChannelId: 3, AvailabilityRate: 90, IsOnline: true, UpdatedAt: 100},
	}).Error)

	require.NoError(t, ResetStaleChannelMonitoringStats([]string{"vip"}, 400))

	var rows []ChannelMonitoringStat
	require.NoError(t, db.Order("channel_id").Find(&rows).Error)
	require.Equal(t, -1.0, rows[0].AvailabilityRate)
	require.Equal(t, -1.0, rows[0].CacheHitRate)
	require.False(t, rows[0].IsOnline)
	require.Zero(t, rows[0].AvgResponseTime)
	require.EqualValues(t, 100, rows[0].UpdatedAt, "updated_at keeps the last time data was seen")
	require.Equal(t, 97.0, rows[1].AvailabilityRate, "refreshed this cycle")
	require.Equal(t, 90.0, rows[2].AvailabilityRate, "unmonitored group untouched")
}

func TestAggregateLogsForMonitoringGroupsByChannelID(t *testing.T) {
	db := useModelIntegrationTestDB(t, &Log{})
	require.NoError(t, db.Create(&[]Log{
		{Type: LogTypeConsume, Group: "vip", ChannelId: 7, CreatedAt: 1000, UseTime: 2},
		{Type: LogTypeError, Group: "vip", ChannelId: 7, CreatedAt: 1000},
	}).Error)

	rows, err := AggregateLogsForMonitoring(900)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	for _, row := range rows {
		require.Equal(t, 7, row.ChannelId)
	}
}

func TestConsumeMonitoringRecordedWhenConsumeLogDisabled(t *testing.T) {
	useModelIntegrationTestDB(t, &Log{}, &User{})
	require.NoError(t, common.UpdateFeatureSettings(map[string]string{"LogConsumeEnabled": "false"}))
	prevHook := common.GroupMonitoringHook
	t.Cleanup(func() { common.GroupMonitoringHook = prevHook })
	var successes int
	common.GroupMonitoringHook = func(group string, channelId int, isSuccess bool, _, _, _, _ int, _ string, _ int, _ string) {
		if isSuccess && group == "vip" && channelId == 9 {
			successes++
		}
	}

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	RecordConsumeLog(c, 1, RecordConsumeLogParams{Group: "vip", ChannelId: 9, ModelName: "m"})
	require.Equal(t, 1, successes)
	var count int64
	require.NoError(t, LOG_DB.Model(&Log{}).Count(&count).Error)
	require.Zero(t, count, "consume log stays disabled")
}
