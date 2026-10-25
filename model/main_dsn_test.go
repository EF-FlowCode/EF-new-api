package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnsureMySQLParseTime(t *testing.T) {
	cases := map[string]string{
		"u:p@tcp(db:3306)/app":                             "u:p@tcp(db:3306)/app?parseTime=true",
		"u:p@tcp(db:3306)/app?charset=utf8mb4":             "u:p@tcp(db:3306)/app?charset=utf8mb4&parseTime=true",
		"u:p@tcp(db:3306)/app?parseTime=True&loc=Local":    "u:p@tcp(db:3306)/app?parseTime=True&loc=Local",
		"u:p@tcp(db:3306)/app?charset=utf8mb4&parsetime=1": "u:p@tcp(db:3306)/app?charset=utf8mb4&parsetime=1",
	}
	for input, want := range cases {
		got, err := ensureMySQLParseTime(input)
		require.NoError(t, err, input)
		require.Equal(t, want, got)
	}
	for _, input := range []string{
		"u:p@tcp(db:3306)/app?parseTime=false",
		"u:p@tcp(db:3306)/app?charset=utf8mb4&parseTime=0",
	} {
		_, err := ensureMySQLParseTime(input)
		require.Error(t, err, input)
	}
}
