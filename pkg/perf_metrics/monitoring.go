package perfmetrics

import (
	"context"
	"sort"
	"time"

	"eflow.local/new-api/model"
)

type MonitoringModel struct {
	ModelName string `json:"model_name"`
	GroupResult
}

type MonitoringModelsResult struct {
	Groups        map[string][]MonitoringModel `json:"groups"`
	StartTime     int64                        `json:"start_time"`
	EndTime       int64                        `json:"end_time"`
	BucketSeconds int64                        `json:"bucket_seconds"`
}

// QueryMonitoringModels uses the same persisted + local unflushed counters as
// Query. Hourly display buckets are merged before calculating rates/averages.
func QueryMonitoringModels(ctx context.Context, groups []string) (MonitoringModelsResult, error) {
	endTs := time.Now().Unix()
	startTs := endTs - 24*3600
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	rows, err := model.GetMonitoringPerfMetrics(ctx, groups, startTs, endTs)
	if err != nil {
		return MonitoringModelsResult{}, err
	}
	merged := map[bucketKey]counters{}
	for _, row := range rows {
		mergeCounters(merged, bucketKey{model: row.ModelName, group: row.Group, bucketTs: row.BucketTs}, counters{
			requestCount: row.RequestCount, successCount: row.SuccessCount,
			totalLatencyMs: row.TotalLatencyMs, ttftSumMs: row.TtftSumMs, ttftCount: row.TtftCount,
			outputTokens: row.OutputTokens, generationMs: row.GenerationMs,
		})
	}
	allowed := allowedGroupSet(groups)
	hotBuckets.Range(func(key, value any) bool {
		k := key.(bucketKey)
		if _, ok := allowed[k.group]; ok && k.bucketTs >= startTs && k.bucketTs <= endTs {
			mergeCounters(merged, k, value.(*atomicBucket).snapshot())
		}
		return true
	})
	return buildMonitoringModels(groups, startTs, endTs, merged), nil
}

func buildMonitoringModels(groups []string, startTs, endTs int64, merged map[bucketKey]counters) MonitoringModelsResult {
	result := MonitoringModelsResult{
		Groups:    make(map[string][]MonitoringModel, len(groups)),
		StartTime: startTs, EndTime: endTs, BucketSeconds: 3600,
	}
	for _, group := range groups {
		result.Groups[group] = []MonitoringModel{}
	}
	models := map[string]map[bucketKey]counters{}
	for key, value := range merged {
		if _, ok := result.Groups[key.group]; !ok || key.bucketTs < startTs || key.bucketTs > endTs || key.model == "" || value.requestCount <= 0 {
			continue
		}
		if models[key.model] == nil {
			models[key.model] = map[bucketKey]counters{}
		}
		key.bucketTs -= key.bucketTs % result.BucketSeconds
		mergeCounters(models[key.model], key, value)
	}
	names := make([]string, 0, len(models))
	for name := range models {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		for _, group := range buildQueryResult(name, models[name]).Groups {
			result.Groups[group.Group] = append(result.Groups[group.Group], MonitoringModel{ModelName: name, GroupResult: group})
		}
	}
	return result
}
