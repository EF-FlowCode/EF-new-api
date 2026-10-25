package service

import (
	"errors"
	"testing"

	"eflow.local/new-api/common"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func useMonitoringRefreshRedis(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	server := miniredis.RunT(t)
	previous, enabled, master := common.RDB, common.RedisEnabled, common.IsMasterNode
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	common.RDB, common.RedisEnabled, common.IsMasterNode = client, true, false
	t.Cleanup(func() {
		_ = client.Close()
		common.RDB, common.RedisEnabled, common.IsMasterNode = previous, enabled, master
	})
	return server
}

func TestMonitoringRefreshCrossInstanceCoalescesAndReloadsFirst(t *testing.T) {
	server := useMonitoringRefreshRedis(t)
	for range 5 {
		accepted, err := TriggerAggregationRefresh()
		require.NoError(t, err)
		require.True(t, accepted)
	}
	var calls []string
	reload := func() error { calls = append(calls, "reload"); return nil }
	aggregate := func() { calls = append(calls, "aggregate") }
	require.NoError(t, processPendingMonitoringRefresh(reload, aggregate))
	require.Empty(t, calls, "web must not consume global work")
	worker := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = worker.Close() })
	common.RDB, common.IsMasterNode = worker, true
	require.NoError(t, processPendingMonitoringRefresh(reload, aggregate))
	require.Equal(t, []string{"reload", "aggregate"}, calls)
	require.False(t, server.Exists(monitoringRefreshKey))
	require.NoError(t, processPendingMonitoringRefresh(reload, aggregate))
	require.Len(t, calls, 2)
}

func TestMonitoringRefreshRetainsNewSignalDuringAggregation(t *testing.T) {
	server := useMonitoringRefreshRedis(t)
	_, err := TriggerAggregationRefresh()
	require.NoError(t, err)
	common.IsMasterNode = true
	require.NoError(t, processPendingMonitoringRefresh(func() error { return nil }, func() {
		accepted, err := TriggerAggregationRefresh()
		require.NoError(t, err)
		require.True(t, accepted)
	}))
	require.True(t, server.Exists(monitoringRefreshKey))
	require.NoError(t, processPendingMonitoringRefresh(func() error { return nil }, func() {}))
	require.False(t, server.Exists(monitoringRefreshKey))
}

func TestMonitoringRefreshFailuresAreNotAcknowledged(t *testing.T) {
	server := useMonitoringRefreshRedis(t)
	_, err := TriggerAggregationRefresh()
	require.NoError(t, err)
	common.IsMasterNode = true
	loadErr := errors.New("config unavailable")
	require.ErrorIs(t, processPendingMonitoringRefresh(func() error { return loadErr }, func() { t.Fatal("must not aggregate stale config") }), loadErr)
	require.True(t, server.Exists(monitoringRefreshKey))
	server.SetError("ERR unavailable")
	accepted, err := TriggerAggregationRefresh()
	require.ErrorIs(t, err, ErrMonitoringRefreshUnavailable)
	require.False(t, accepted)
	require.Error(t, processPendingMonitoringRefresh(func() error { t.Fatal("must not reload without a signal"); return nil }, func() {}))
}
