package multi_key_scheduler

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/dto"
)

// 默认配置（InFlightCap=0）下，亲和**不看 in_flight**——即便 pin 的 key 在途很高
// （线上实测是泄漏:hung/client-gone/重启没 release），也照样复用,不把用户踢散。
// 这是「pin 的 key 永久卡在 in_flight=cap → 亲和踢散所有人 → 缓存全失效」的回归防护。
func TestAffinityIgnoresInFlightByDefault(t *testing.T) {
	setBackendForTest(newMemoryBackend())
	defer setBackendForTest(newMemoryBackend())
	ctx := context.Background()
	// 不设 Affinity → InFlightCap 零值 0 = 关
	cfg := &dto.SchedulerConfig{
		Enabled: true,
		Models:  map[string]dto.ModelQuota{"m": {RPMMax: 500}},
	}

	r1 := Acquire(ctx, 1, "m", affinityTestCands(), cfg, "u1") // pin K
	// 把 K 的 in_flight 灌高（都不 release，模拟泄漏/突发），默认配置应忽略它
	for i := 0; i < 9; i++ {
		got := Acquire(ctx, 1, "m", affinityTestCands(), cfg, "u1")
		if got.KeyFp != r1.KeyFp {
			t.Fatalf("default affinity should ignore in_flight and stay on pin %s, drifted to %s at i=%d", r1.KeyFp, got.KeyFp, i)
		}
	}
}

func affinityTestCfg(disabled bool) *dto.SchedulerConfig {
	return &dto.SchedulerConfig{
		Enabled:  true,
		Models:   map[string]dto.ModelQuota{"m": {RPMMax: 500}},
		Affinity: dto.SchedulerAffinity{Disabled: disabled, InFlightCap: 2},
	}
}

func affinityTestCands() []KeyCandidate {
	return []KeyCandidate{{Index: 0, Fp: "fp0"}, {Index: 1, Fp: "fp1"}, {Index: 2, Fp: "fp2"}}
}

// 同一用户连续请求应粘在同一把 key（在途未超 cap 前），保住上游 prompt cache。
func TestAffinityPinsSameUserToSameKey(t *testing.T) {
	setBackendForTest(newMemoryBackend())
	defer setBackendForTest(newMemoryBackend())
	ctx := context.Background()
	cfg := affinityTestCfg(false)

	r1 := Acquire(ctx, 1, "m", affinityTestCands(), cfg, "u1")
	if !r1.Allowed {
		t.Fatalf("r1 not allowed: %+v", r1)
	}
	r2 := Acquire(ctx, 1, "m", affinityTestCands(), cfg, "u1")
	if !r2.Allowed || r2.KeyFp != r1.KeyFp {
		t.Fatalf("same user should stick to %s, got %+v", r1.KeyFp, r2)
	}
}

// 不同用户彼此独立粘，互不影响（各自有 pin）。
func TestAffinityDifferentUsersIndependentPins(t *testing.T) {
	setBackendForTest(newMemoryBackend())
	defer setBackendForTest(newMemoryBackend())
	ctx := context.Background()
	cfg := affinityTestCfg(false)

	a1 := Acquire(ctx, 1, "m", affinityTestCands(), cfg, "u1")
	a2 := Acquire(ctx, 1, "m", affinityTestCands(), cfg, "u1")
	if a2.KeyFp != a1.KeyFp {
		t.Fatalf("u1 should be sticky: %s vs %s", a1.KeyFp, a2.KeyFp)
	}
}

// pin 的 key 在途 >= cap（疑似卡死/过载）时，应让位给评分摊开，换一把 key。
func TestAffinityYieldsWhenPinnedKeyOverloaded(t *testing.T) {
	setBackendForTest(newMemoryBackend())
	defer setBackendForTest(newMemoryBackend())
	ctx := context.Background()
	cfg := affinityTestCfg(false) // cap=2；memory backend 不 ReportSuccess，故 in_flight 只增不减

	r1 := Acquire(ctx, 1, "m", affinityTestCands(), cfg, "u1") // pin fp_x，in_flight=1
	_ = Acquire(ctx, 1, "m", affinityTestCands(), cfg, "u1")   // 复用 fp_x，in_flight=2
	r3 := Acquire(ctx, 1, "m", affinityTestCands(), cfg, "u1") // fp_x in_flight=2>=cap → 让位
	if !r3.Allowed {
		t.Fatalf("r3 not allowed: %+v", r3)
	}
	if r3.KeyFp == r1.KeyFp {
		t.Fatalf("pinned key %s overloaded should have yielded, still got it", r1.KeyFp)
	}
}

// 突发并发让 pin 的 key 临时溢出分流，但 pin 不应漂移——突发过后释放在途，
// 下一次请求应回到原来那把（带缓存的）key。
func TestAffinityKeepsPinAfterTransientOverload(t *testing.T) {
	setBackendForTest(newMemoryBackend())
	defer setBackendForTest(newMemoryBackend())
	ctx := context.Background()
	cfg := affinityTestCfg(false) // cap=2

	r1 := Acquire(ctx, 1, "m", affinityTestCands(), cfg, "u1") // pin K，in_flight=1
	_ = Acquire(ctx, 1, "m", affinityTestCands(), cfg, "u1")   // 复用 K，in_flight=2
	r3 := Acquire(ctx, 1, "m", affinityTestCands(), cfg, "u1") // K 满 → 临时分流到别处
	if r3.KeyFp == r1.KeyFp {
		t.Fatalf("transient overload should route elsewhere, still got %s", r1.KeyFp)
	}
	// 突发过后释放 K 的两笔在途
	be := getBackend()
	be.ReportSuccess(ctx, 1, r1.KeyFp, "m", 10)
	be.ReportSuccess(ctx, 1, r1.KeyFp, "m", 10)

	r4 := Acquire(ctx, 1, "m", affinityTestCands(), cfg, "u1") // pin 没漂移 → 回到 K
	if r4.KeyFp != r1.KeyFp {
		t.Fatalf("pin drifted after overload subsided: want %s, got %s", r1.KeyFp, r4.KeyFp)
	}
}

// 关闭亲和（Disabled=true）时不记 pin，行为退回纯评分。
func TestAffinityDisabledRecordsNoPin(t *testing.T) {
	be := newMemoryBackend()
	setBackendForTest(be)
	defer setBackendForTest(newMemoryBackend())
	ctx := context.Background()
	cfg := affinityTestCfg(true)

	r := Acquire(ctx, 1, "m", affinityTestCands(), cfg, "u1")
	if !r.Allowed {
		t.Fatalf("not allowed: %+v", r)
	}
	if fp, ok := be.GetAffinity(ctx, 1, "m", "u1"); ok {
		t.Fatalf("affinity disabled but pin recorded: %s", fp)
	}
}

// 空 affinityValue（如内部无登录态任务）不记 pin、不影响选 key。
func TestAffinityEmptyValueNoPin(t *testing.T) {
	be := newMemoryBackend()
	setBackendForTest(be)
	defer setBackendForTest(newMemoryBackend())
	ctx := context.Background()
	cfg := affinityTestCfg(false)

	r := Acquire(ctx, 1, "m", affinityTestCands(), cfg, "")
	if !r.Allowed {
		t.Fatalf("not allowed: %+v", r)
	}
	if _, ok := be.GetAffinity(ctx, 1, "m", ""); ok {
		t.Fatalf("empty affinity value should not record a pin")
	}
}
