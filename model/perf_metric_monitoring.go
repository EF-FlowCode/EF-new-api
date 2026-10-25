package model

import "context"

// GetMonitoringPerfMetrics reads all monitored groups in one bounded query.
// An empty allowlist must never become an unrestricted query.
func GetMonitoringPerfMetrics(ctx context.Context, groups []string, startTs, endTs int64) ([]PerfMetric, error) {
	metrics := make([]PerfMetric, 0)
	if len(groups) == 0 {
		return metrics, nil
	}
	err := DB.WithContext(ctx).Model(&PerfMetric{}).
		Where(commonGroupCol+" IN ? AND bucket_ts >= ? AND bucket_ts <= ? AND request_count > 0", groups, startTs, endTs).
		Find(&metrics).Error
	return metrics, err
}
