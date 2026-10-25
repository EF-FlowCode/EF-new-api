package model

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/internal/mysqlversion"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

const (
	commonGroupCol  = "`group`"
	commonGroupsCol = "`groups`"
	commonKeyCol    = "`key`"
	commonTrueVal   = "1"
	commonFalseVal  = "0"
	logGroupCol     = commonGroupCol
	logKeyCol       = commonKeyCol
)

var DB *gorm.DB

var LOG_DB *gorm.DB

func CheckSetup() {
	setup := GetSetup()
	if setup == nil {
		if RootUserExists() {
			common.SysLog("system setup is incomplete: root user exists without a setup record")
		} else {
			common.SysLog("system is not initialized and no root user exists")
		}
		constant.SetSetupComplete(false)
	} else {
		// Setup record exists, system is initialized
		common.SysLog("system is already initialized at: " + time.Unix(setup.InitializedAt, 0).String())
		constant.SetSetupComplete(true)
	}
}

// ensureMySQLParseTime requires DATETIME columns to scan into time.Time; the
// models store passkey, 2FA and OAuth timestamps that way.
func ensureMySQLParseTime(dsn string) (string, error) {
	query := ""
	if _, after, ok := strings.Cut(dsn, "?"); ok {
		query = after
	}
	for param := range strings.SplitSeq(query, "&") {
		name, value, _ := strings.Cut(param, "=")
		if !strings.EqualFold(name, "parseTime") {
			continue
		}
		if enabled, err := strconv.ParseBool(value); err != nil || !enabled {
			return "", fmt.Errorf("parseTime must be true, got %q", value)
		}
		return dsn, nil
	}
	if strings.Contains(dsn, "?") {
		return dsn + "&parseTime=true", nil
	}
	return dsn + "?parseTime=true", nil
}

func openMySQLDB(envName string) (*gorm.DB, error) {
	dsn := strings.TrimSpace(os.Getenv(envName))
	if dsn == "" {
		return nil, fmt.Errorf("%s is required; MySQL 8.x is the only supported database", envName)
	}
	dsn, err := ensureMySQLParseTime(dsn)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", envName, err)
	}
	db, err := gorm.Open(mysql.Open(dsn), newGormConfig(true))
	if err != nil {
		return nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err = mysqlversion.Require8Server(ctx, sqlDB); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("%s must target MySQL 8.x: %w", envName, err)
	}
	common.SysLog("using MySQL 8 database")
	return db, nil
}

func InitDB() (err error) {
	db, err := openMySQLDB("SQL_DSN")
	if err == nil {
		if common.DebugEnabled {
			db = db.Debug()
		}
		DB = db
		if err := checkMySQLChineseSupport(DB); err != nil {
			panic(err)
		}
		sqlDB, err := DB.DB()
		if err != nil {
			return err
		}
		sqlDB.SetMaxIdleConns(common.GetEnvOrDefault("SQL_MAX_IDLE_CONNS", 100))
		sqlDB.SetMaxOpenConns(common.GetEnvOrDefault("SQL_MAX_OPEN_CONNS", 1000))
		sqlDB.SetConnMaxLifetime(time.Second * time.Duration(common.GetEnvOrDefault("SQL_MAX_LIFETIME", 60)))

		return nil
	} else {
		common.FatalLog(err)
	}
	return err
}

func InitLogDB() (err error) {
	if os.Getenv("LOG_SQL_DSN") == "" {
		LOG_DB = DB
		return
	}
	db, err := openMySQLDB("LOG_SQL_DSN")
	if err == nil {
		if common.DebugEnabled {
			db = db.Debug()
		}
		LOG_DB = db
		if err := checkMySQLChineseSupport(LOG_DB); err != nil {
			panic(err)
		}
		sqlDB, err := LOG_DB.DB()
		if err != nil {
			return err
		}
		sqlDB.SetMaxIdleConns(common.GetEnvOrDefault("SQL_MAX_IDLE_CONNS", 100))
		sqlDB.SetMaxOpenConns(common.GetEnvOrDefault("SQL_MAX_OPEN_CONNS", 1000))
		sqlDB.SetConnMaxLifetime(time.Second * time.Duration(common.GetEnvOrDefault("SQL_MAX_LIFETIME", 60)))

		return nil
	} else {
		common.FatalLog(err)
	}
	return err
}

// MigrateDatabases mutates schemas only after InitDB and InitLogDB have both
// opened and validated their configured MySQL 8 servers.
func MigrateDatabases() error {
	if DB == nil || LOG_DB == nil {
		return fmt.Errorf("database connections must be initialized before migration")
	}
	if !common.IsMasterNode || common.DeploymentRole == "worker" {
		return nil
	}

	common.SysLog("database migration started")
	if err := migrateDB(LOG_DB == DB); err != nil {
		return err
	}
	if LOG_DB != DB {
		return migrateLOGDB()
	}
	return nil
}

func migrateDB(includeLogModel bool) error {
	if err := migrateChannelGroupToLongText(); err != nil {
		return err
	}
	// Expand model_limits without ever narrowing an existing text-family column.
	if err := migrateTokenModelLimitsToLongText(); err != nil {
		return err
	}
	// Preserve the legacy uniqueness constraint until its replacement exists.
	if err := migrateRiskSubjectUniqueIndex(); err != nil {
		return err
	}

	models := []any{
		&Channel{},
		&Token{},
		&User{},
		&PasskeyCredential{},
		&Option{},
		&Redemption{},
		&InvitationCode{},
		&InvitationCodeUsage{},
		&Ability{},
		&Midjourney{},
		&TopUp{},
		&QuotaData{},
		&Task{},
		&TaskPlugin{},
		&TaskPluginControl{},
		&TaskPluginShadowBinding{},
		&TaskPluginSourceSnapshot{},
		&TaskPluginTaskReference{},
		&TaskPluginProductionBinding{},
		&TaskPluginSubmission{},
		&TaskPluginReview{},
		&KnowledgeFile{},
		&Model{},
		&Vendor{},
		&PrefillGroup{},
		&Setup{},
		&TwoFA{},
		&TwoFABackupCode{},
		&Checkin{},
		&Ticket{},
		&TicketMessage{},
		&TicketInvoice{},
		&TicketRefund{},
		&UsageRefundBatch{},
		&UsageRefundReceipt{},
		&UsageRefundNotice{},
		&TicketAttachment{},
		&CustomOAuthProvider{},
		&UserOAuthBinding{},
		&RiskRule{},
		&RiskSubjectSnapshot{},
		&RiskIncident{},
		&EnforcementIncident{},
		&ModerationIncident{},
		&ModerationRule{},
		&ChannelMonitoringStat{},
		&GroupMonitoringStat{},
		&MonitoringHistory{},
		&PerfMetric{},
		&UserAccessToken{},
	}
	if includeLogModel {
		models = append(models, &Log{}, &UsageRefundLogDelivery{}, &AuditLog{})
	}
	if err := DB.AutoMigrate(models...); err != nil {
		return err
	}
	return ensureTaskPluginControl(DB)
}

// migrateChannelGroupToLongText expands the comma-separated channel group list
// without allowing the following AutoMigrate pass to narrow an existing value.
func migrateChannelGroupToLongText() error {
	const (
		tableName  = "channels"
		columnName = "group"
	)
	if !DB.Migrator().HasTable(tableName) || !DB.Migrator().HasColumn(&Channel{}, columnName) {
		return nil
	}

	var columnType string
	result := DB.Raw(`SELECT COLUMN_TYPE FROM information_schema.columns
		WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?`,
		tableName, columnName).Scan(&columnType)
	if result.Error != nil {
		return fmt.Errorf("inspect %s.%s type: %w", tableName, columnName, result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("inspect %s.%s type: expected one metadata row, got %d", tableName, columnName, result.RowsAffected)
	}
	needsMigration, err := longTextColumnMigrationRequired(columnType)
	if err != nil {
		return fmt.Errorf("inspect %s.%s type: %w", tableName, columnName, err)
	}
	if !needsMigration {
		return nil
	}

	if err := DB.Exec("ALTER TABLE `channels` MODIFY COLUMN `group` LONGTEXT NULL").Error; err != nil {
		return fmt.Errorf("migrate %s.%s to longtext: %w", tableName, columnName, err)
	}
	common.SysLog("Successfully migrated channels.group to longtext")
	return nil
}

func migrateLOGDB() error {
	var err error
	if err = LOG_DB.AutoMigrate(&Log{}, &UsageRefundLogDelivery{}, &AuditLog{}); err != nil {
		return err
	}
	return nil
}

// migrateRiskSubjectUniqueIndex installs and verifies the v2 unique index
// before removing the stricter legacy index. MySQL DDL auto-commits, so this
// order avoids a window with no database uniqueness constraint.
func migrateRiskSubjectUniqueIndex() error {
	const (
		legacyIndexName = "idx_risk_subject_unique"
		newIndexName    = "idx_risk_subject_unique_v2"
	)
	statement := &gorm.Statement{DB: DB}
	if err := statement.Parse(&RiskSubjectSnapshot{}); err != nil {
		return fmt.Errorf("resolve risk subject table: %w", err)
	}
	tableName := statement.Schema.Table
	if !DB.Migrator().HasTable(tableName) {
		return nil
	}
	if !DB.Migrator().HasColumn(&RiskSubjectSnapshot{}, "Group") {
		if err := DB.Migrator().AddColumn(&RiskSubjectSnapshot{}, "Group"); err != nil {
			return fmt.Errorf("add risk subject group column: %w", err)
		}
	}

	columns, err := mysqlIndexColumns(tableName, newIndexName)
	if err != nil {
		return err
	}
	if len(columns) == 0 {
		if err := DB.Migrator().CreateIndex(&RiskSubjectSnapshot{}, newIndexName); err != nil {
			return fmt.Errorf("create risk subject v2 unique index: %w", err)
		}
		columns, err = mysqlIndexColumns(tableName, newIndexName)
		if err != nil {
			return err
		}
	}
	if strings.Join(columns, ",") != "subject_type,subject_id,group" {
		return fmt.Errorf("risk subject v2 unique index has unexpected columns: %v", columns)
	}
	isUnique, err := mysqlIndexIsUnique(tableName, newIndexName)
	if err != nil {
		return err
	}
	if !isUnique {
		return fmt.Errorf("risk subject v2 index is not unique")
	}

	var count int
	if err := DB.Raw(`SELECT COUNT(*) FROM information_schema.statistics
			WHERE table_schema = DATABASE() AND table_name = ? AND index_name = ?`,
		tableName, legacyIndexName).Scan(&count).Error; err != nil {
		return fmt.Errorf("probe legacy risk subject unique index: %w", err)
	}
	if count > 0 {
		if err := DB.Exec("DROP INDEX `" + legacyIndexName + "` ON `" + tableName + "`").Error; err != nil {
			return fmt.Errorf("drop legacy risk subject unique index: %w", err)
		}
	}
	return nil
}

func mysqlIndexColumns(tableName string, indexName string) ([]string, error) {
	var joined string
	if err := DB.Raw(`SELECT COALESCE(GROUP_CONCAT(column_name ORDER BY seq_in_index SEPARATOR ','), '')
		FROM information_schema.statistics
		WHERE table_schema = DATABASE() AND table_name = ? AND index_name = ?`,
		tableName, indexName).Scan(&joined).Error; err != nil {
		return nil, fmt.Errorf("inspect index %s on %s: %w", indexName, tableName, err)
	}
	if joined == "" {
		return nil, nil
	}
	return strings.Split(joined, ","), nil
}

func mysqlIndexIsUnique(tableName string, indexName string) (bool, error) {
	var nonUnique int
	result := DB.Raw(`SELECT non_unique
		FROM information_schema.statistics
		WHERE table_schema = DATABASE() AND table_name = ? AND index_name = ?
		ORDER BY seq_in_index LIMIT 1`, tableName, indexName).Scan(&nonUnique)
	if result.Error != nil {
		return false, fmt.Errorf("inspect uniqueness of index %s on %s: %w", indexName, tableName, result.Error)
	}
	if result.RowsAffected != 1 {
		return false, nil
	}
	return nonUnique == 0, nil
}

// migrateTokenModelLimitsToLongText expands model_limits monotonically and is
// safe to run repeatedly.
func migrateTokenModelLimitsToLongText() error {
	tableName := "tokens"
	columnName := "model_limits"

	if !DB.Migrator().HasTable(tableName) {
		return nil
	}

	if !DB.Migrator().HasColumn(&Token{}, columnName) {
		return nil
	}

	var columnType string
	result := DB.Raw(`SELECT COLUMN_TYPE FROM information_schema.columns
				WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?`,
		tableName, columnName).Scan(&columnType)
	if result.Error != nil {
		return fmt.Errorf("inspect %s.%s type: %w", tableName, columnName, result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("inspect %s.%s type: expected one metadata row, got %d", tableName, columnName, result.RowsAffected)
	}
	needsMigration, err := longTextColumnMigrationRequired(columnType)
	if err != nil {
		return fmt.Errorf("inspect %s.%s type: %w", tableName, columnName, err)
	}
	if !needsMigration {
		return nil
	}

	if err := DB.Exec(fmt.Sprintf("ALTER TABLE `%s` MODIFY COLUMN `%s` LONGTEXT", tableName, columnName)).Error; err != nil {
		return fmt.Errorf("failed to migrate %s.%s to longtext: %w", tableName, columnName, err)
	}
	common.SysLog(fmt.Sprintf("Successfully migrated %s.%s to longtext", tableName, columnName))
	return nil
}

func longTextColumnMigrationRequired(columnType string) (bool, error) {
	normalized := strings.ToLower(strings.TrimSpace(columnType))
	switch {
	case normalized == "longtext":
		return false, nil
	case normalized == "tinytext", normalized == "text", normalized == "mediumtext",
		strings.HasPrefix(normalized, "varchar("), strings.HasPrefix(normalized, "char("):
		return true, nil
	default:
		return false, fmt.Errorf("unexpected source column type %q", columnType)
	}
}

func closeDB(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	err = sqlDB.Close()
	return err
}

func CloseDB() error {
	if LOG_DB != DB {
		err := closeDB(LOG_DB)
		if err != nil {
			return err
		}
	}
	return closeDB(DB)
}

// checkMySQLChineseSupport ensures the MySQL connection and current schema
// default charset/collation can store Chinese characters. It allows common
// Chinese-capable charsets (utf8mb4, utf8, gbk, big5, gb18030) and panics otherwise.
func checkMySQLChineseSupport(db *gorm.DB) error {
	// 仅检测：当前库默认字符集/排序规则 + 各表的排序规则（隐含字符集）

	// Read current schema defaults
	var schemaCharset, schemaCollation string
	err := db.Raw("SELECT DEFAULT_CHARACTER_SET_NAME, DEFAULT_COLLATION_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME = DATABASE()").Row().Scan(&schemaCharset, &schemaCollation)
	if err != nil {
		return fmt.Errorf("读取当前库默认字符集/排序规则失败 / Failed to read schema default charset/collation: %v", err)
	}

	toLower := func(s string) string { return strings.ToLower(s) }
	// Allowed charsets that can store Chinese text
	allowedCharsets := map[string]string{
		"utf8mb4": "utf8mb4_",
		"utf8":    "utf8_",
		"gbk":     "gbk_",
		"big5":    "big5_",
		"gb18030": "gb18030_",
	}
	isChineseCapable := func(cs, cl string) bool {
		csLower := toLower(cs)
		clLower := toLower(cl)
		if prefix, ok := allowedCharsets[csLower]; ok {
			if clLower == "" {
				return true
			}
			return strings.HasPrefix(clLower, prefix)
		}
		// 如果仅提供了排序规则，尝试按排序规则前缀判断
		for _, prefix := range allowedCharsets {
			if strings.HasPrefix(clLower, prefix) {
				return true
			}
		}
		return false
	}

	// 1) 当前库默认值必须支持中文
	if !isChineseCapable(schemaCharset, schemaCollation) {
		return fmt.Errorf("当前库默认字符集/排序规则不支持中文：schema(%s/%s)。请将库设置为 utf8mb4/utf8/gbk/big5/gb18030 / Schema default charset/collation is not Chinese-capable: schema(%s/%s). Please set to utf8mb4/utf8/gbk/big5/gb18030",
			schemaCharset, schemaCollation, schemaCharset, schemaCollation)
	}

	// 2) 所有物理表的排序规则（隐含字符集）必须支持中文
	type tableInfo struct {
		Name      string
		Collation *string
	}
	var tables []tableInfo
	if err := db.Raw("SELECT TABLE_NAME, TABLE_COLLATION FROM information_schema.TABLES WHERE TABLE_SCHEMA = DATABASE() AND TABLE_TYPE = 'BASE TABLE'").Scan(&tables).Error; err != nil {
		return fmt.Errorf("读取表排序规则失败 / Failed to read table collations: %v", err)
	}

	var badTables []string
	for _, t := range tables {
		// NULL 或空表示继承库默认设置，已在上面校验库默认，视为通过
		if t.Collation == nil || *t.Collation == "" {
			continue
		}
		cl := *t.Collation
		// 仅凭排序规则判断是否中文可用
		ok := false
		lower := strings.ToLower(cl)
		for _, prefix := range allowedCharsets {
			if strings.HasPrefix(lower, prefix) {
				ok = true
				break
			}
		}
		if !ok {
			badTables = append(badTables, fmt.Sprintf("%s(%s)", t.Name, cl))
		}
	}

	if len(badTables) > 0 {
		// 限制输出数量以避免日志过长
		maxShow := 20
		shown := badTables
		if len(shown) > maxShow {
			shown = shown[:maxShow]
		}
		return fmt.Errorf(
			"存在不支持中文的表，请修复其排序规则/字符集。示例（最多展示 %d 项）：%v / Found tables not Chinese-capable. Please fix their collation/charset. Examples (showing up to %d): %v",
			maxShow, shown, maxShow, shown,
		)
	}
	return nil
}

var (
	lastPingTime time.Time
	pingMutex    sync.Mutex
)

func PingDB() error {
	pingMutex.Lock()
	defer pingMutex.Unlock()

	if time.Since(lastPingTime) < time.Second*10 {
		return nil
	}

	sqlDB, err := DB.DB()
	if err != nil {
		log.Printf("Error getting sql.DB from GORM: %v", err)
		return err
	}

	err = sqlDB.Ping()
	if err != nil {
		log.Printf("Error pinging DB: %v", err)
		return err
	}

	lastPingTime = time.Now()
	common.SysLog("Database pinged successfully")
	return nil
}
