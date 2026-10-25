package testdb

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"os"
	"strings"
	"testing"
	"time"

	"eflow.local/new-api/internal/mysqlversion"
	mysqldriver "github.com/go-sql-driver/mysql"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
)

const databasePrefix = "new_api_test_"

// OpenMySQL8 creates an isolated database on the explicitly configured MySQL
// test server. Cleanup only drops the database created by this invocation.
func OpenMySQL8(t testing.TB, models ...any) *gorm.DB {
	t.Helper()

	dsn := strings.TrimSpace(os.Getenv("TEST_MYSQL_DSN"))
	if dsn == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("TEST_MYSQL_DSN is required in CI for MySQL 8 integration tests")
		}
		t.Skip("set TEST_MYSQL_DSN to run MySQL 8 integration tests")
	}

	config, err := mysqldriver.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("parse TEST_MYSQL_DSN: %v", err)
	}
	adminConfig := *config
	adminConfig.DBName = ""

	adminDB, err := sql.Open("mysql", adminConfig.FormatDSN())
	if err != nil {
		t.Fatalf("open MySQL test server: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err = adminDB.PingContext(ctx); err != nil {
		_ = adminDB.Close()
		t.Fatalf("connect to MySQL test server: %v", err)
	}

	if err = mysqlversion.Require8Server(ctx, adminDB); err != nil {
		_ = adminDB.Close()
		t.Fatalf("TEST_MYSQL_DSN must target MySQL 8.x: %v", err)
	}

	databaseName := uniqueDatabaseName(t)
	if _, err = adminDB.ExecContext(ctx, "CREATE DATABASE `"+databaseName+"` CHARACTER SET utf8mb4"); err != nil {
		_ = adminDB.Close()
		t.Fatalf("create isolated MySQL test database: %v", err)
	}

	var testSQLDB *sql.DB
	t.Cleanup(func() {
		if testSQLDB != nil {
			_ = testSQLDB.Close()
		}
		if isManagedDatabaseName(databaseName) {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cleanupCancel()
			if _, dropErr := adminDB.ExecContext(cleanupCtx, "DROP DATABASE `"+databaseName+"`"); dropErr != nil {
				t.Errorf("drop isolated MySQL test database %s: %v", databaseName, dropErr)
			}
		}
		_ = adminDB.Close()
	})

	testConfig := *config
	testConfig.DBName = databaseName
	db, err := gorm.Open(gormmysql.Open(testConfig.FormatDSN()), &gorm.Config{})
	if err != nil {
		t.Fatalf("open isolated MySQL test database: %v", err)
	}
	testSQLDB, err = db.DB()
	if err != nil {
		t.Fatalf("get isolated MySQL sql.DB: %v", err)
	}
	if len(models) > 0 {
		if err = db.AutoMigrate(models...); err != nil {
			t.Fatalf("migrate isolated MySQL test database: %v", err)
		}
	}
	return db
}

func uniqueDatabaseName(t testing.TB) string {
	t.Helper()
	random := make([]byte, 12)
	if _, err := rand.Read(random); err != nil {
		t.Fatalf("generate MySQL test database name: %v", err)
	}
	return databasePrefix + hex.EncodeToString(random)
}

func isManagedDatabaseName(name string) bool {
	suffix, ok := strings.CutPrefix(name, databasePrefix)
	if !ok || len(suffix) != 24 {
		return false
	}
	_, err := hex.DecodeString(suffix)
	return err == nil
}
