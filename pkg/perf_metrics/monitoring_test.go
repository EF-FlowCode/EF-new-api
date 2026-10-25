package perfmetrics

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func TestMonitoringModelsIsolationAndWeightedMetrics(t *testing.T) {
	result := buildMonitoringModels([]string{"vip", "cheap", "empty"}, 0, 7200, map[bucketKey]counters{
		{model: "a", group: "vip", bucketTs: 60}:          {requestCount: 1, successCount: 0, totalLatencyMs: 1000, ttftSumMs: 100, ttftCount: 1, outputTokens: 10, generationMs: 1000},
		{model: "a", group: "vip", bucketTs: 120}:         {requestCount: 9, successCount: 9, totalLatencyMs: 90000, ttftSumMs: 9000, ttftCount: 9, outputTokens: 900, generationMs: 9000},
		{model: "a", group: "cheap", bucketTs: 60}:        {requestCount: 2, successCount: 1},
		{model: "b", group: "vip", bucketTs: 60}:          {requestCount: 3, successCount: 3},
		{model: "private", group: "hidden", bucketTs: 60}: {requestCount: 1000, successCount: 0},
		{model: "old", group: "vip", bucketTs: -60}:       {requestCount: 1000},
		{model: "future", group: "vip", bucketTs: 7260}:   {requestCount: 1000},
	})
	if len(result.Groups) != 3 || len(result.Groups["vip"]) != 2 || len(result.Groups["cheap"]) != 1 || len(result.Groups["empty"]) != 0 {
		t.Fatalf("unexpected grouping: %+v", result.Groups)
	}
	a := result.Groups["vip"][0]
	if a.ModelName != "a" || a.SuccessRate != 90 || a.AvgLatencyMs != 9100 || a.AvgTtftMs != 910 || math.Abs(a.AvgTps-91) > 0.001 {
		t.Fatalf("metrics must be weighted by their respective counters: %+v", a)
	}
	if len(a.Series) != 1 || a.Series[0].Ts != 0 || a.Series[0].SuccessRate != 90 || a.Series[0].AvgTps != 91 {
		t.Fatalf("hourly rollup must merge counts before averaging: %+v", a.Series)
	}
	if result.Groups["cheap"][0].SuccessRate != 50 || result.Groups["vip"][1].SuccessRate != 100 {
		t.Fatal("model or group metrics leaked into another row")
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"request_count", "success_count", "output_tokens", "channel_id", "hidden", "private"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("response leaks %s", secret)
		}
	}
}

func TestMonitoringModelsEmptyAllowlist(t *testing.T) {
	// No database is installed. An empty allowlist must short-circuit the read,
	// and must not expose a request instance's in-memory metrics either.
	key := bucketKey{model: "private", group: "hidden", bucketTs: 60}
	hotBuckets.Store(key, &atomicBucket{})
	t.Cleanup(func() { hotBuckets.Delete(key) })
	for _, groups := range [][]string{nil, {}} {
		result, err := QueryMonitoringModels(context.Background(), groups)
		if err != nil || len(result.Groups) != 0 {
			t.Fatalf("empty allowlist: %+v, %v", result, err)
		}
	}
}

func TestMonitoringModelsPreservesMissingHoursAndFailures(t *testing.T) {
	result := buildMonitoringModels([]string{"vip"}, 0, 10800, map[bucketKey]counters{
		{model: "a", group: "vip", bucketTs: 60}:   {requestCount: 1, successCount: 1},
		{model: "a", group: "vip", bucketTs: 7260}: {requestCount: 1, successCount: 0},
	})
	series := result.Groups["vip"][0].Series
	if len(series) != 2 || series[0].Ts != 0 || series[1].Ts != 7200 || series[1].SuccessRate != 0 {
		t.Fatalf("empty hour must stay absent; failed hour is a real 0%% sample: %+v", series)
	}
}
