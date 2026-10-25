package model

import (
	"bytes"
	"log"
	"strings"
	"testing"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestGetAllLogsProtectsModelFilterFromOrderingIndexScan(t *testing.T) {
	generatedSQL := captureGetAllLogsSQL(t, "kimi-k2.6")
	const guardedFromClause = "SELECT * FROM logs IGNORE INDEX FOR ORDER BY (`PRIMARY`, `idx_created_at_id`)"
	if !strings.Contains(generatedSQL, guardedFromClause) {
		t.Fatalf("model-filtered log query has no ordering-index guard:\n%s", generatedSQL)
	}
	if count := strings.Count(generatedSQL, "SELECT count(*)"); count != 1 {
		t.Fatalf("expected one count query before the log list, got %d:\n%s", count, generatedSQL)
	}
}

func TestGetAllLogsLeavesUnfilteredOrderingPlanUnchanged(t *testing.T) {
	generatedSQL := captureGetAllLogsSQL(t, "")
	if strings.Contains(generatedSQL, "IGNORE INDEX FOR ORDER BY") {
		t.Fatalf("unfiltered log query unexpectedly has an ordering-index guard:\n%s", generatedSQL)
	}
}

func captureGetAllLogsSQL(t *testing.T, modelName string) string {
	t.Helper()

	var sqlLog bytes.Buffer
	dryRunDB, err := gorm.Open(mysql.New(mysql.Config{
		DSN:                       "gorm:gorm@tcp(localhost:9910)/gorm?charset=utf8&parseTime=True&loc=Local",
		SkipInitializeWithVersion: true,
	}), &gorm.Config{
		DryRun:               true,
		DisableAutomaticPing: true,
		Logger: logger.New(log.New(&sqlLog, "", 0), logger.Config{
			LogLevel: logger.Info,
			Colorful: false,
		}),
	})
	if err != nil {
		t.Fatalf("open MySQL dry-run database: %v", err)
	}

	previousDB, previousLogDB := DB, LOG_DB
	DB, LOG_DB = dryRunDB, dryRunDB
	t.Cleanup(func() {
		DB, LOG_DB = previousDB, previousLogDB
	})

	_, _, err = GetAllLogs(
		LogTypeUnknown,
		1788019200,
		1788074048,
		modelName,
		"",
		"",
		0,
		100,
		0,
		"",
		"",
		"",
		0,
		NewLogSortOptions("", ""),
	)
	if err != nil {
		t.Fatalf("build filtered log query: %v", err)
	}
	return sqlLog.String()
}
