package model

import (
	"strconv"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/internal/testdb"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func useModelIntegrationTestDB(t *testing.T, models ...any) *gorm.DB {
	t.Helper()

	db := testdb.OpenMySQL8(t, models...)
	oldDB, oldLogDB := DB, LOG_DB
	oldRedisEnabled := common.RedisEnabled
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	oldBatchUpdateEnabled := common.BatchUpdateEnabled
	oldFeatureSettings := common.GetFeatureSettings()

	DB, LOG_DB = db, db
	common.RedisEnabled = false
	common.MemoryCacheEnabled = false
	common.BatchUpdateEnabled = false
	require.NoError(t, common.UpdateFeatureSettings(map[string]string{"LogConsumeEnabled": "true"}))
	t.Cleanup(func() {
		DB, LOG_DB = oldDB, oldLogDB
		common.RedisEnabled = oldRedisEnabled
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
		common.BatchUpdateEnabled = oldBatchUpdateEnabled
		require.NoError(t, common.UpdateFeatureSettings(map[string]string{
			"LogConsumeEnabled": strconv.FormatBool(oldFeatureSettings.LogConsumeEnabled),
		}))
	})
	return db
}

func useFullModelIntegrationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return useModelIntegrationTestDB(t,
		&Task{},
		&User{},
		&Token{},
		&PasskeyCredential{},
		&TwoFA{},
		&TwoFABackupCode{},
		&Log{},
		&Channel{},
		&Ability{},
		&TopUp{},
		&Ticket{},
		&TicketMessage{},
		&TicketAttachment{},
		&UserOAuthBinding{},
		&PerfMetric{},
		&QuotaData{},
		&UserAccessToken{},
	)
}
