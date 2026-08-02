package model

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSanitizeDBErrorStripsDriverMessage(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"mysql", &mysql.MySQLError{Number: 1062, Message: "secret-value"}, "mysql error 1062"},
		{"postgres", &pgconn.PgError{Code: "23505", Detail: "secret-value"}, "postgres error SQLSTATE 23505"},
		{"wrapped", fmt.Errorf("exec failed: %w", &mysql.MySQLError{Number: 1064, Message: "secret-value"}), "mysql error 1064"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := sanitizeDBError(test.err)
			require.Error(t, got)
			assert.Equal(t, test.want, got.Error())
			assert.NotContains(t, got.Error(), "secret-value")
		})
	}
}

func TestGormLoggerParameterizedOutput(t *testing.T) {
	previousDebug := common.DebugEnabled
	common.DebugEnabled = false
	t.Cleanup(func() { common.DebugEnabled = previousDebug })

	var buf bytes.Buffer
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: newGormLogger(&buf)})
	require.NoError(t, err)
	db.Exec("SELECT * FROM missing_table WHERE k = ?", "secret-value")

	assert.Contains(t, buf.String(), "k = ?")
	assert.NotContains(t, buf.String(), "secret-value")
	assert.Contains(t, buf.String(), "sqlite error")
}
