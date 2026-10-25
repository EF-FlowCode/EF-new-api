package jsplugin

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestValidateUsageFactsCanonicalSchema(t *testing.T) {
	schema := map[string]UsageFieldSchema{"seconds": {Type: "number", Unit: "second"}, "resolution": {Enum: []string{"720p"}}, "audio": {Type: "boolean"}}
	require.NoError(t, ValidateUsageFacts(schema, map[string]any{"seconds": 5.0, "resolution": "720p", "audio": false}))
	for _, facts := range []map[string]any{{"resolution": "bad"}, {"resolution": 720.0}, {"seconds": float64(MaxTaskDurationSeconds + 1)}, {"audio": 0.0}, {"unknown": 1.0}} {
		require.Error(t, ValidateUsageFacts(schema, facts))
	}
}
