package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func assertLogIndexColumns(t *testing.T, indexName string, expected []string) {
	t.Helper()
	db := useModelIntegrationTestDB(t, &Log{})
	var columns []string
	err := db.Raw(
		`SELECT COLUMN_NAME FROM information_schema.statistics
		 WHERE table_schema = DATABASE() AND table_name = ? AND index_name = ?
		 ORDER BY SEQ_IN_INDEX`,
		"logs", indexName,
	).Pluck("COLUMN_NAME", &columns).Error
	require.NoError(t, err)
	require.Equal(t, expected, columns)
}

func TestLogHasUserTimeOrderIndex(t *testing.T) {
	assertLogIndexColumns(t, "idx_logs_user_created_id", []string{"user_id", "created_at", "id"})
}

func TestLogHasModelTimeIndex(t *testing.T) {
	assertLogIndexColumns(t, "idx_logs_model_created_at", []string{"model_name", "created_at"})
}
