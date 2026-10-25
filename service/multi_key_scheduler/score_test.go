package multi_key_scheduler

import (
	"context"
	"testing"

	"eflow.local/new-api/dto"
)

// mapPeekBackend 是只为 pickBest 测试服务的假后端：Peek 按 fp 返回预设 in-flight，
// 其余方法都是 no-op。RPMCount/TPMCount 恒 0，所以每个 fp 的 headroom 都是 1.0，
// 选择完全由 in-flight + 随机平手决定，便于断言均衡行为。
type mapPeekBackend struct {
	inflight map[string]int
}

func (b *mapPeekBackend) Peek(_ context.Context, _ int, fp, _ string, _ dto.ModelQuota) Decision {
	return Decision{Allowed: true, InFlight: b.inflight[fp]}
}
func (b *mapPeekBackend) Acquire(_ context.Context, _ int, _, _ string, _ dto.ModelQuota) Decision {
	return Decision{Allowed: true}
}
func (b *mapPeekBackend) ReportSuccess(_ context.Context, _ int, _, _ string, _ int)      {}
func (b *mapPeekBackend) ReportError(_ context.Context, _ int, _, _ string)               {}
func (b *mapPeekBackend) MarkCool(_ context.Context, _ int, _, _ string, _ int, _ string) {}
func (b *mapPeekBackend) Snapshot(_ context.Context, _ int) []StateEntry                  { return nil }
func (b *mapPeekBackend) GetAffinity(_ context.Context, _ int, _, _ string) (string, bool) {
	return "", false
}
func (b *mapPeekBackend) SetAffinity(_ context.Context, _ int, _, _, _ string, _ int) {}

func glmTestCfg() *dto.SchedulerConfig {
	return &dto.SchedulerConfig{
		Enabled: true,
		Models:  map[string]dto.ModelQuota{"m": {RPMMax: 500, TPMMax: 1000000}},
	}
}

// 健康度相同但 fp0 在途多 → 永远不该选 fp0（least-in-flight 绕开 hung key）。
func TestPickBestAvoidsHighInFlight(t *testing.T) {
	be := &mapPeekBackend{inflight: map[string]int{"fp0": 3, "fp1": 0, "fp2": 0}}
	cands := []KeyCandidate{{Index: 0, Fp: "fp0"}, {Index: 1, Fp: "fp1"}, {Index: 2, Fp: "fp2"}}
	for i := range 100 {
		best := pickBest(context.Background(), be, 1, "m", cands, glmTestCfg())
		if best == nil {
			t.Fatal("pickBest returned nil")
		}
		if best.Fp == "fp0" {
			t.Fatalf("iteration %d picked fp0 (in_flight=3); should be avoided", i)
		}
	}
}

// 全健康全在途 0 → 应在所有 key 间铺开，而非永远落在列表第一把（旧 strict-max 行为）。
func TestPickBestSpreadsOnTie(t *testing.T) {
	be := &mapPeekBackend{inflight: map[string]int{"fp0": 0, "fp1": 0, "fp2": 0, "fp3": 0}}
	cands := []KeyCandidate{
		{Index: 0, Fp: "fp0"}, {Index: 1, Fp: "fp1"},
		{Index: 2, Fp: "fp2"}, {Index: 3, Fp: "fp3"},
	}
	counts := map[string]int{}
	for range 400 {
		best := pickBest(context.Background(), be, 1, "m", cands, glmTestCfg())
		counts[best.Fp]++
	}
	for _, fp := range []string{"fp0", "fp1", "fp2", "fp3"} {
		if counts[fp] == 0 {
			t.Fatalf("key %s never picked over 400 draws — not spreading: %+v", fp, counts)
		}
	}
}
