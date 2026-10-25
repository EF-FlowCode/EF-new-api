package common

import (
	"testing"

	"eflow.local/new-api/constant"
	"github.com/stretchr/testify/require"
)

func TestGetAnonymousRequestBodyLimitBytes(t *testing.T) {
	old := constant.AnonymousRequestBodyLimitKB
	t.Cleanup(func() { constant.AnonymousRequestBodyLimitKB = old })

	constant.AnonymousRequestBodyLimitKB = 512
	require.Equal(t, int64(512<<10), GetAnonymousRequestBodyLimitBytes())

	constant.AnonymousRequestBodyLimitKB = 0
	require.Zero(t, GetAnonymousRequestBodyLimitBytes())

	constant.AnonymousRequestBodyLimitKB = -1
	require.Equal(t, int64(512<<10), GetAnonymousRequestBodyLimitBytes())
}
