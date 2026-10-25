package model

import (
	"os"
	"strings"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/internal/testdb"
	"gorm.io/gorm"
)

func TestOpenMySQLDBRequiresDSN(t *testing.T) {
	t.Setenv("SQL_DSN", "")

	db, err := openMySQLDB("SQL_DSN")
	if db != nil {
		t.Fatal("expected no database connection without SQL_DSN")
	}
	if err == nil || !strings.Contains(err.Error(), "MySQL 8.x") {
		t.Fatalf("expected clear MySQL 8.x requirement, got %v", err)
	}
}

func TestOpenMySQLDBAcceptsConfiguredMySQL8(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("TEST_MYSQL_DSN"))
	if dsn == "" {
		t.Skip("set TEST_MYSQL_DSN to run MySQL 8 integration tests")
	}
	t.Setenv("SQL_DSN", dsn)

	db, err := openMySQLDB("SQL_DSN")
	if err != nil {
		t.Fatalf("open configured MySQL 8 server: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql.DB: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
}

func TestLongTextColumnMigrationRequired(t *testing.T) {
	tests := []struct {
		columnType string
		want       bool
		wantErr    bool
	}{
		{columnType: "varchar(1024)", want: true},
		{columnType: "char(64)", want: true},
		{columnType: "tinytext", want: true},
		{columnType: "text", want: true},
		{columnType: "MEDIUMTEXT", want: true},
		{columnType: "longtext"},
		{columnType: "json", wantErr: true},
		{columnType: "", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.columnType, func(t *testing.T) {
			got, err := longTextColumnMigrationRequired(test.columnType)
			if (err != nil) != test.wantErr {
				t.Fatalf("longTextColumnMigrationRequired(%q) error = %v, wantErr %v", test.columnType, err, test.wantErr)
			}
			if got != test.want {
				t.Fatalf("longTextColumnMigrationRequired(%q) = %v, want %v", test.columnType, got, test.want)
			}
		})
	}
}

func useMigrationDatabases(t *testing.T, primaryDB, logDB *gorm.DB) {
	t.Helper()
	previousDB, previousLogDB := DB, LOG_DB
	previousMasterNode := common.IsMasterNode
	DB, LOG_DB = primaryDB, logDB
	common.IsMasterNode = true
	t.Cleanup(func() {
		DB, LOG_DB = previousDB, previousLogDB
		common.IsMasterNode = previousMasterNode
	})
}

func TestMigrateDatabasesPreservesWideTextColumns(t *testing.T) {
	db := testdb.OpenMySQL8(t, &Channel{}, &Token{})
	useMigrationDatabases(t, db, db)

	if err := db.Exec("ALTER TABLE `tokens` MODIFY COLUMN `model_limits` MEDIUMTEXT").Error; err != nil {
		t.Fatalf("prepare mediumtext token column: %v", err)
	}
	wideValue := strings.Repeat("wide-value,", 7_000)
	channel := Channel{Name: "wide-channel", Key: "sk-wide", Group: wideValue}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatalf("insert wide channel value: %v", err)
	}
	token := Token{Name: "wide-token", Key: "sk-wide-token", ModelLimits: wideValue}
	if err := db.Create(&token).Error; err != nil {
		t.Fatalf("insert wide token value: %v", err)
	}

	for attempt := 1; attempt <= 2; attempt++ {
		if err := MigrateDatabases(); err != nil {
			t.Fatalf("migration attempt %d: %v", attempt, err)
		}
	}

	var savedChannel Channel
	if err := db.Select("id", "group").First(&savedChannel, channel.Id).Error; err != nil {
		t.Fatalf("read migrated channel: %v", err)
	}
	if savedChannel.Group != wideValue {
		t.Fatalf("channel group changed during migration: got %d bytes, want %d", len(savedChannel.Group), len(wideValue))
	}
	var savedToken Token
	if err := db.Select("id", "model_limits").First(&savedToken, token.Id).Error; err != nil {
		t.Fatalf("read migrated token: %v", err)
	}
	if savedToken.ModelLimits != wideValue {
		t.Fatalf("token model limits changed during migration: got %d bytes, want %d", len(savedToken.ModelLimits), len(wideValue))
	}
	for tableName, columnName := range map[string]string{
		"channels": "group",
		"tokens":   "model_limits",
	} {
		var columnType string
		if err := db.Raw(`SELECT DATA_TYPE FROM information_schema.columns
			WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?`,
			tableName, columnName).Scan(&columnType).Error; err != nil {
			t.Fatalf("inspect %s.%s: %v", tableName, columnName, err)
		}
		if columnType != "longtext" {
			t.Fatalf("%s.%s type = %q, want longtext", tableName, columnName, columnType)
		}
	}
}

func TestMigrateRiskSubjectUniqueIndexReplacesConstraintSafely(t *testing.T) {
	db := testdb.OpenMySQL8(t)
	useMigrationDatabases(t, db, db)

	const tableName = "risk_subject_snapshots"
	if err := db.Exec(`CREATE TABLE risk_subject_snapshots (
		id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
		subject_type VARCHAR(16) NOT NULL,
		subject_id BIGINT NOT NULL,
		UNIQUE KEY idx_risk_subject_unique (subject_type, subject_id)
	)`).Error; err != nil {
		t.Fatalf("create legacy risk subject table: %v", err)
	}
	if err := db.Exec(`INSERT INTO risk_subject_snapshots (subject_type, subject_id) VALUES ('user', 42)`).Error; err != nil {
		t.Fatalf("insert legacy risk subject: %v", err)
	}
	for attempt := 1; attempt <= 2; attempt++ {
		if err := MigrateDatabases(); err != nil {
			t.Fatalf("migration attempt %d: %v", attempt, err)
		}
	}

	if !db.Migrator().HasColumn(&RiskSubjectSnapshot{}, "Group") {
		t.Fatal("expected group column to be added before replacing the index")
	}
	columns, err := mysqlIndexColumns(tableName, "idx_risk_subject_unique_v2")
	if err != nil {
		t.Fatalf("inspect replacement index: %v", err)
	}
	if strings.Join(columns, ",") != "subject_type,subject_id,group" {
		t.Fatalf("unexpected replacement index columns: %v", columns)
	}
	isUnique, err := mysqlIndexIsUnique(tableName, "idx_risk_subject_unique_v2")
	if err != nil {
		t.Fatalf("inspect replacement index uniqueness: %v", err)
	}
	if !isUnique {
		t.Fatal("replacement index is not unique")
	}
	legacyColumns, err := mysqlIndexColumns(tableName, "idx_risk_subject_unique")
	if err != nil {
		t.Fatalf("inspect legacy index: %v", err)
	}
	if len(legacyColumns) != 0 {
		t.Fatalf("legacy index still exists: %v", legacyColumns)
	}
	var saved RiskSubjectSnapshot
	if err := db.Where("subject_type = ? AND subject_id = ?", "user", 42).First(&saved).Error; err != nil {
		t.Fatalf("read migrated risk subject: %v", err)
	}
	if saved.Group != "" {
		t.Fatalf("legacy risk subject group = %q, want empty default", saved.Group)
	}
}

func TestMigrateDatabasesKeepsSeparateLogTableOutOfPrimary(t *testing.T) {
	primaryDB := testdb.OpenMySQL8(t)
	logDB := testdb.OpenMySQL8(t, &Log{})
	useMigrationDatabases(t, primaryDB, logDB)

	if err := primaryDB.Exec(`CREATE TABLE logs (
		id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
		content VARCHAR(255) NOT NULL
	)`).Error; err != nil {
		t.Fatalf("create legacy primary log table: %v", err)
	}
	if err := primaryDB.Exec(`INSERT INTO logs (content) VALUES ('primary-sentinel')`).Error; err != nil {
		t.Fatalf("insert legacy primary log: %v", err)
	}
	logRecord := Log{Content: "dedicated-sentinel"}
	if err := logDB.Create(&logRecord).Error; err != nil {
		t.Fatalf("insert dedicated log: %v", err)
	}

	for attempt := 1; attempt <= 2; attempt++ {
		if err := MigrateDatabases(); err != nil {
			t.Fatalf("migration attempt %d: %v", attempt, err)
		}
	}
	if primaryDB.Migrator().HasColumn(&Log{}, "UserId") {
		t.Fatal("primary legacy log table was unexpectedly migrated")
	}
	var primaryContent string
	if err := primaryDB.Raw("SELECT content FROM logs WHERE id = 1").Scan(&primaryContent).Error; err != nil {
		t.Fatalf("read primary legacy log: %v", err)
	}
	if primaryContent != "primary-sentinel" {
		t.Fatalf("primary legacy log changed to %q", primaryContent)
	}
	var savedLog Log
	if err := logDB.First(&savedLog, logRecord.Id).Error; err != nil {
		t.Fatalf("read dedicated log after migration: %v", err)
	}
	if savedLog.Content != "dedicated-sentinel" {
		t.Fatalf("dedicated log changed to %q", savedLog.Content)
	}
}
