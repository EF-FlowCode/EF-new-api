package model

import (
	"net/http/httptest"
	"strconv"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/internal/testdb"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func withDataExportEnabled(t *testing.T) {
	t.Helper()
	original := common.GetFeatureSettings()
	require.NoError(t, common.UpdateFeatureSettings(map[string]string{"DataExportEnabled": "true"}))
	t.Cleanup(func() {
		require.NoError(t, common.UpdateFeatureSettings(map[string]string{
			"DataExportEnabled": strconv.FormatBool(original.DataExportEnabled),
		}))
	})
}

func TestLogQuotaDataAtomicallyIncrementsOneBucket(t *testing.T) {
	truncateTables(t)

	require.NoError(t, LogQuotaData(44, "user-44", "gpt-test", 120, 7199, 300))
	require.NoError(t, LogQuotaData(44, "user-44", "gpt-test", 80, 7198, 200))

	var rows []QuotaData
	require.NoError(t, DB.Find(&rows).Error)
	require.Len(t, rows, 1)
	require.Equal(t, int64(3600), rows[0].CreatedAt)
	require.Equal(t, 2, rows[0].Count)
	require.Equal(t, 200, rows[0].Quota)
	require.Equal(t, 500, rows[0].TokenUsed)
}

func TestRecordConsumeLogUsesLogTimestampForQuotaBucket(t *testing.T) {
	truncateTables(t)
	withDataExportEnabled(t)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("username", "user-44")
	recordConsumeLogAt(c, 44, RecordConsumeLogParams{
		ModelName:        "gpt-test",
		PromptTokens:     100,
		CompletionTokens: 20,
		QuotaDataTokens:  1350,
		Quota:            250,
	}, 7199)

	var log Log
	require.NoError(t, DB.First(&log).Error)
	require.Equal(t, int64(7199), log.CreatedAt)

	var quotaData QuotaData
	require.NoError(t, DB.First(&quotaData).Error)
	require.Equal(t, int64(3600), quotaData.CreatedAt)
	require.Equal(t, 1, quotaData.Count)
	require.Equal(t, 250, quotaData.Quota)
	require.Equal(t, 1350, quotaData.TokenUsed)
}

func TestRecordConsumeLogDoesNotAggregateFailedLogInsert(t *testing.T) {
	truncateTables(t)
	withDataExportEnabled(t)

	badLogDB := testdb.OpenMySQL8(t)
	oldLogDB := LOG_DB
	LOG_DB = badLogDB
	t.Cleanup(func() { LOG_DB = oldLogDB })

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("username", "user-44")
	recordConsumeLogAt(c, 44, RecordConsumeLogParams{
		ModelName: "gpt-test",
		Quota:     250,
	}, 7199)

	var count int64
	require.NoError(t, DB.Model(&QuotaData{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestTaskSettlementLogDoesNotCountAsNewDashboardRequest(t *testing.T) {
	truncateTables(t)
	withDataExportEnabled(t)

	RecordTaskBillingLog(RecordTaskBillingLogParams{
		UserId:    44,
		LogType:   LogTypeConsume,
		ModelName: "video-test",
		Quota:     100,
		Other: map[string]any{
			"task_id":            "task-1",
			"pre_consumed_quota": 100,
			"actual_quota":       200,
		},
	})

	var logCount int64
	require.NoError(t, DB.Model(&Log{}).Count(&logCount).Error)
	require.EqualValues(t, 1, logCount)

	var quotaDataCount int64
	require.NoError(t, DB.Model(&QuotaData{}).Count(&quotaDataCount).Error)
	require.Zero(t, quotaDataCount)
}
