package plugins

import (
	"os"
	"testing"

	"eflow.local/new-api/pkg/jsplugin"
	"github.com/stretchr/testify/require"
)

func TestAlibabaOfflineFixture(t *testing.T) {
	source, err := Source("alibaba")
	require.NoError(t, err)
	fixture, err := os.ReadFile("testdata/alibaba-wan.json")
	require.NoError(t, err)
	report, err := jsplugin.ReplayFixture(t.Context(), source, fixture)
	require.NoError(t, err)
	require.Equal(t, 9, report.Passed)
	require.Equal(t, report.Total, report.Passed)
}
