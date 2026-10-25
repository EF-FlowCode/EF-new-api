package model

import (
	"context"
	"strings"
	"testing"
	"time"

	"eflow.local/new-api/internal/testdb"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// ddlRecorder records every statement GORM executes so the test can prove
// AutoMigrate issues no DDL once the column and index already exist.
type ddlRecorder struct {
	logger.Interface
	statements []string
}

func (r *ddlRecorder) Trace(_ context.Context, _ time.Time, fc func() (string, int64), _ error) {
	sql, _ := fc()
	r.statements = append(r.statements, sql)
}

// eflow: 线上 logs 表 660 万行，billing_anomaly 列与索引要在发布前手动在线建。
// 本用例锁定手动 DDL 与模型定义一致：建好后 AutoMigrate 不再发出 ALTER/CREATE，
// 发布的 migrate 步骤不会再去改大表。
func TestBillingAnomalyManualDDLLeavesAutoMigrateNoOp(t *testing.T) {
	db := testdb.OpenMySQL8(t, &Log{})
	// 模拟线上旧表：去掉新列与新索引。
	require.NoError(t, db.Exec("ALTER TABLE logs DROP INDEX idx_logs_billing_anomaly_created").Error)
	require.NoError(t, db.Exec("ALTER TABLE logs DROP COLUMN billing_anomaly").Error)

	// 与 docs 中记录的线上手动 DDL 完全一致。
	require.NoError(t, db.Exec("ALTER TABLE logs ADD COLUMN billing_anomaly TINYINT NOT NULL DEFAULT 0, ALGORITHM=INSTANT").Error)
	require.NoError(t, db.Exec("ALTER TABLE logs ADD INDEX idx_logs_billing_anomaly_created (billing_anomaly, created_at), ALGORITHM=INPLACE, LOCK=NONE").Error)

	recorder := &ddlRecorder{Interface: logger.Discard}
	require.NoError(t, db.Session(&gorm.Session{Logger: recorder}).AutoMigrate(&Log{}))
	for _, statement := range recorder.statements {
		upper := strings.ToUpper(strings.TrimSpace(statement))
		require.False(t, strings.HasPrefix(upper, "ALTER") || strings.HasPrefix(upper, "CREATE"),
			"AutoMigrate issued DDL after the manual migration: %s", statement)
	}
}
