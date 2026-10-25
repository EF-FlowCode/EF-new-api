package model

import (
	"bytes"
	"fmt"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/internal/testdb"
	"github.com/go-sql-driver/mysql"
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
	db := testdb.OpenMySQL8(t).Session(&gorm.Session{Logger: newGormLogger(&buf)})
	db.Exec("SELECT * FROM missing_table WHERE k = ?", "secret-value")

	assert.Contains(t, buf.String(), "k = ?")
	assert.NotContains(t, buf.String(), "secret-value")
	assert.Contains(t, buf.String(), "mysql error 1146")
}
