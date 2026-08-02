package service

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/operation_setting"
)

func TestIsAvailabilityExcluded(t *testing.T) {
	cfg := operation_setting.GroupMonitoringSetting{
		AvailabilityExcludeStatusCodes: []int{400, 413, 429},
		AvailabilityExcludeKeywords:    []string{"field messages is required", "not implemented"},
		AvailabilityExcludeModels:      []string{"banned-model"},
	}

	tests := []struct {
		name       string
		statusCode int
		modelName  string
		content    string
		want       bool
	}{
		{"keyword substring case-insensitive", 500, "gpt-5.5", "status_code=500, NOT IMPLEMENTED", true},
		{"keyword exact phrase", 500, "gpt-5.5", "status_code=500, not implemented", true},
		{"status code match", 429, "gpt-5.5", "rate limited", true},
		{"model match", 0, "banned-model", "", true},
		{"success no exclusion", 0, "gpt-5.5", "", false},
		{"real 500 not a keyword stays counted", 500, "gpt-5.5", "upstream connection reset", false},
		{"503 not in exclude codes stays counted", 503, "gpt-5.5", "service unavailable", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isAvailabilityExcluded(cfg, tt.statusCode, tt.modelName, tt.content); got != tt.want {
				t.Errorf("isAvailabilityExcluded(%d, %q, %q) = %v, want %v",
					tt.statusCode, tt.modelName, tt.content, got, tt.want)
			}
		})
	}
}

func TestParseMonitoringKey_Valid(t *testing.T) {
	bucketTs, group, channelId, ok := ParseMonitoringKey("gm:b:1745740800:vip:42")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if bucketTs != 1745740800 {
		t.Errorf("bucketTs = %d, want 1745740800", bucketTs)
	}
	if group != "vip" {
		t.Errorf("group = %q, want %q", group, "vip")
	}
	if channelId != 42 {
		t.Errorf("channelId = %d, want 42", channelId)
	}
}

func TestParseMonitoringKey_Invalid(t *testing.T) {
	tests := []struct {
		name string
		key  string
	}{
		{"empty", ""},
		{"wrong prefix", "rc:b:123:grp:1"},
		{"missing parts", "gm:b:123"},
		{"bad bucket", "gm:b:abc:grp:1"},
		{"bad channel", "gm:b:123:grp:xyz"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, _, ok := ParseMonitoringKey(tt.key)
			if ok {
				t.Errorf("expected ok=false for key %q", tt.key)
			}
		})
	}
}

func TestParseBucketValues(t *testing.T) {
	vals := map[string]string{
		"t":  "100",
		"s":  "95",
		"e":  "5",
		"ct": "5000",
		"pt": "20000",
		"rt": "250000",
		"fs": "85000",
		"fc": "95",
	}
	bd := ParseBucketValues(vals)
	if bd.Total != 100 {
		t.Errorf("Total = %d, want 100", bd.Total)
	}
	if bd.Success != 95 {
		t.Errorf("Success = %d, want 95", bd.Success)
	}
	if bd.Error != 5 {
		t.Errorf("Error = %d, want 5", bd.Error)
	}
	if bd.CacheTokens != 5000 {
		t.Errorf("CacheTokens = %d, want 5000", bd.CacheTokens)
	}
	if bd.PromptTokens != 20000 {
		t.Errorf("PromptTokens = %d, want 20000", bd.PromptTokens)
	}
	if bd.RespTimeMs != 250000 {
		t.Errorf("RespTimeMs = %d, want 250000", bd.RespTimeMs)
	}
	if bd.FRTSumMs != 85000 {
		t.Errorf("FRTSumMs = %d, want 85000", bd.FRTSumMs)
	}
	if bd.FRTCount != 95 {
		t.Errorf("FRTCount = %d, want 95", bd.FRTCount)
	}
}

func TestParseBucketValues_Empty(t *testing.T) {
	bd := ParseBucketValues(map[string]string{})
	if bd.Total != 0 || bd.Success != 0 || bd.Error != 0 {
		t.Errorf("expected all zeros for empty map, got %+v", bd)
	}
}

func TestIsGroupMonitored_Empty(t *testing.T) {
	monitoredGroupsMu.Lock()
	monitoredGroupsCache = nil
	monitoredGroupsMu.Unlock()

	if isGroupMonitored("vip") {
		t.Error("expected false when cache is nil")
	}
}

func TestIsGroupMonitored_Set(t *testing.T) {
	monitoredGroupsMu.Lock()
	monitoredGroupsCache = map[string]struct{}{
		"vip":  {},
		"free": {},
	}
	monitoredGroupsMu.Unlock()

	if !isGroupMonitored("vip") {
		t.Error("expected true for 'vip'")
	}
	if !isGroupMonitored("free") {
		t.Error("expected true for 'free'")
	}
	if isGroupMonitored("premium") {
		t.Error("expected false for 'premium'")
	}
}

func TestRecordMonitoringMetric_SkipsAutoGroup(t *testing.T) {
	// Should not panic even with no Redis
	RecordMonitoringMetric("auto", 1, true, 100, 50, 2000, 800, "gpt-4", 0, "")
	RecordMonitoringMetric("", 1, true, 100, 50, 2000, 800, "gpt-4", 0, "")
}

func TestTriggerAggregationRefresh(t *testing.T) {
	// Drain the channel first
	select {
	case <-triggerCh:
	default:
	}

	ok := TriggerAggregationRefresh()
	if !ok {
		t.Error("expected TriggerAggregationRefresh to return true")
	}

	// Drain the triggered signal
	select {
	case <-triggerCh:
	default:
		t.Error("expected trigger signal in channel")
	}
}
