package middleware

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRateLimitDurationSecondsSaturates(t *testing.T) {
	require.Equal(t, int64(60), rateLimitDurationSeconds(1))
	require.Equal(t, int64(0), rateLimitDurationSeconds(0))
	require.Equal(t, int64(math.MaxInt64), rateLimitDurationSeconds(math.MaxInt))
}

func TestRateLimitCapacitySaturates(t *testing.T) {
	require.Equal(t, int64(600), rateLimitCapacity(10, 60))
	require.Equal(t, int64(0), rateLimitCapacity(0, 60))
	require.Equal(t, int64(math.MaxInt64), rateLimitCapacity(math.MaxInt, math.MaxInt64))
}

func TestRateLimitExpirationSaturates(t *testing.T) {
	require.Equal(t, time.Minute, rateLimitExpiration(60))
	require.Equal(t, time.Duration(0), rateLimitExpiration(0))
	require.Equal(t, time.Duration(math.MaxInt64), rateLimitExpiration(math.MaxInt64))
}
