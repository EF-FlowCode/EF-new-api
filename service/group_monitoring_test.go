package service

import (
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/model"
	"eflow.local/new-api/setting/operation_setting"
)

func TestCountFilteredFinalErrors(t *testing.T) {
	cfg := operation_setting.GroupMonitoringSetting{}
	monitored := map[string]struct{}{"vip": {}}
	rows := []model.MonitoringErrorRow{
		{RequestId: "failed", Group: "vip", ChannelId: 44, ModelName: "gpt", Content: "first", Other: `{"status_code":503}`},
		{RequestId: "failed", Group: "vip", ChannelId: 110, ModelName: "gpt", Content: "last", Other: `{"status_code":502}`},
		{RequestId: "recovered", Group: "vip", ChannelId: 44, ModelName: "gpt", Content: "retry", Other: `{"status_code":503}`},
	}
	succeeded := map[string]struct{}{"recovered": {}}

	got := countFilteredFinalErrors(cfg, rows, succeeded, monitored)

	if got[channelKey{Group: "vip", ChannelId: 44}] != 0 {
		t.Fatalf("retried or recovered attempts must not be counted: %#v", got)
	}
	if got[channelKey{Group: "vip", ChannelId: 110}] != 1 {
		t.Fatalf("final failed request must be counted once on its last channel: %#v", got)
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
	previousRedis, previousMaster := common.RedisEnabled, common.IsMasterNode
	common.RedisEnabled, common.IsMasterNode = false, true
	t.Cleanup(func() { common.RedisEnabled, common.IsMasterNode = previousRedis, previousMaster })
	// Drain the channel first
	select {
	case <-triggerCh:
	default:
	}

	ok, err := TriggerAggregationRefresh()
	if !ok || err != nil {
		t.Error("expected TriggerAggregationRefresh to return true")
	}

	// Drain the triggered signal
	select {
	case <-triggerCh:
	default:
		t.Error("expected trigger signal in channel")
	}
}

func TestParseBucketValues_ResponseTimeUsesAllRequests(t *testing.T) {
	// 两条请求，其中一条命中可用率排除（不计入 t）：均值应为 (1000+3000)/2。
	bd := ParseBucketValues(map[string]string{"t": "1", "s": "1", "rt": "4000", "n": "2"})
	if bd.RespCount != 2 || bd.RespTimeMs/bd.RespCount != 2000 {
		t.Fatalf("avg response time = %d/%d, want 4000/2", bd.RespTimeMs, bd.RespCount)
	}
	// 旧版本桶没有 n：退回 t。
	legacy := ParseBucketValues(map[string]string{"t": "3", "rt": "300"})
	if legacy.RespCount != 3 {
		t.Fatalf("legacy RespCount = %d, want 3", legacy.RespCount)
	}
}
