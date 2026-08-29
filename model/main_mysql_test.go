package model

import (
	"strings"
	"testing"
)

func TestOpenMySQLDBRequiresDSN(t *testing.T) {
	t.Setenv("SQL_DSN", "")

	db, err := openMySQLDB("SQL_DSN")
	if db != nil {
		t.Fatal("expected no database connection without SQL_DSN")
	}
	if err == nil || !strings.Contains(err.Error(), "MySQL 8.0+") {
		t.Fatalf("expected clear MySQL 8.0+ requirement, got %v", err)
	}
}
