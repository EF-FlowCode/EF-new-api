package multi_key_scheduler

import (
	"context"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/dto"
)

func TestMemoryAcquireRPMExceeded(t *testing.T) {
	b := newMemoryBackend()
	q := dto.ModelQuota{RPMMax: 2}
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		dec := b.Acquire(ctx, 99, "fp1", "qwen-plus", q)
		if !dec.Allowed {
			t.Fatalf("Acquire #%d should be allowed, got %+v", i, dec)
		}
	}
	dec := b.Acquire(ctx, 99, "fp1", "qwen-plus", q)
	if dec.Allowed {
		t.Fatalf("third Acquire should hit RPM, got allowed")
	}
	if dec.Reason != ReasonRPMExceeded {
		t.Fatalf("expect rpm_exceeded, got %q", dec.Reason)
	}
}

func TestMemoryCoolBlocksAcquire(t *testing.T) {
	b := newMemoryBackend()
	ctx := context.Background()
	q := dto.ModelQuota{RPMMax: 100}

	b.MarkCool(ctx, 99, "fp1", "m", 30, "throttling.RateQuota")
	dec := b.Acquire(ctx, 99, "fp1", "m", q)
	if dec.Allowed || dec.Reason != ReasonCooling {
		t.Fatalf("expect cooling decision, got %+v", dec)
	}
	if !dec.Cooling {
		t.Fatalf("Decision.Cooling should be true")
	}
}

func TestMemoryReportSuccessReleasesInFlightAndAddsTPM(t *testing.T) {
	b := newMemoryBackend()
	ctx := context.Background()
	q := dto.ModelQuota{ConcurrencyMax: 2, TPMMax: 1000}

	dec := b.Acquire(ctx, 1, "fp1", "m", q)
	if !dec.Allowed {
		t.Fatalf("Acquire should be allowed, got %+v", dec)
	}
	if dec.InFlight != 1 {
		t.Fatalf("expect inFlight=1, got %d", dec.InFlight)
	}

	b.ReportSuccess(ctx, 1, "fp1", "m", 250)
	peek := b.Peek(ctx, 1, "fp1", "m", q)
	if peek.InFlight != 0 {
		t.Fatalf("expect inFlight=0 after release, got %d", peek.InFlight)
	}
	if peek.TPMCount != 250 {
		t.Fatalf("expect tpmCount=250, got %d", peek.TPMCount)
	}
}

func TestMemorySnapshot(t *testing.T) {
	b := newMemoryBackend()
	ctx := context.Background()
	q := dto.ModelQuota{RPMMax: 5}
	_ = b.Acquire(ctx, 7, "fpA", "m1", q)
	_ = b.Acquire(ctx, 7, "fpB", "m1", q)
	_ = b.Acquire(ctx, 8, "fpC", "m1", q) // 不同 channel

	entries := b.Snapshot(ctx, 7)
	if len(entries) != 2 {
		t.Fatalf("expect 2 entries for channel 7, got %d", len(entries))
	}
	for _, e := range entries {
		if e.Model != "m1" {
			t.Fatalf("expect model m1, got %q", e.Model)
		}
		if e.RPMCount != 1 {
			t.Fatalf("expect rpm=1, got %d", e.RPMCount)
		}
	}
}

func TestIsRateLimitError(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		message  string
		expected bool
	}{
		{"http 429", http.StatusTooManyRequests, "", true},
		{"throttling.RateQuota", 400, "Code: Throttling.RateQuota, Message: ...", true},
		{"rpm limit lower", 200, "rpm limit reached", true},
		{"upper case rate_limit_exceeded", 400, "RATE_LIMIT_EXCEEDED", true},
		{"too many requests", 400, "Too Many Requests, please retry later", true},
		{"5xx with empty msg", 502, "", false},
		{"unrelated 4xx", 400, "invalid api key", false},
	}
	for _, c := range cases {
		got := isRateLimitError(c.status, c.message)
		if got != c.expected {
			t.Errorf("%s: isRateLimitError(%d, %q) = %v, want %v", c.name, c.status, c.message, got, c.expected)
		}
	}
}

func TestNormalizeConfigDefaults(t *testing.T) {
	cfg := &dto.SchedulerConfig{
		Enabled: true,
		Models: map[string]dto.ModelQuota{
			"":          {RPMMax: 10}, // 空 model 应被剔除
			"qwen-plus": {RPMMax: -1, TPMMax: 500, ConcurrencyMax: -3},
		},
	}
	NormalizeConfig(cfg)

	if cfg.Policy.OnLimit != OnLimitSkip {
		t.Errorf("OnLimit default = %q, want skip", cfg.Policy.OnLimit)
	}
	if cfg.Policy.CoolSeconds != DefaultCoolSeconds {
		t.Errorf("CoolSeconds default = %d, want %d", cfg.Policy.CoolSeconds, DefaultCoolSeconds)
	}
	if _, ok := cfg.Models[""]; ok {
		t.Errorf("empty-name model should be dropped")
	}
	q := cfg.Models["qwen-plus"]
	if q.RPMMax != 0 || q.ConcurrencyMax != 0 {
		t.Errorf("negative quota fields should be reset to 0, got %+v", q)
	}
	if q.TPMMax != 500 {
		t.Errorf("positive quota field should be preserved, got %+v", q)
	}
}

func TestComputeHeadroomTakesMin(t *testing.T) {
	q := dto.ModelQuota{RPMMax: 100, TPMMax: 1000, ConcurrencyMax: 10}
	// rpm 90% used -> 10% headroom，应是评分瓶颈
	dec := Decision{RPMCount: 90, TPMCount: 100, InFlight: 1}
	score := computeHeadroom(q, dec)
	if score < 0.09 || score > 0.11 {
		t.Errorf("headroom = %f, expected ~0.10 (rpm 是瓶颈)", score)
	}
}

func TestQuotaForKeyOverrideTakesPrecedence(t *testing.T) {
	cfg := &dto.SchedulerConfig{
		Models: map[string]dto.ModelQuota{
			"qwen-plus": {RPMMax: 100},
		},
		KeyOverrides: map[string]map[string]dto.ModelQuota{
			"fpVIP": {"qwen-plus": {RPMMax: 500}},
		},
	}
	if got := QuotaFor(cfg, "fpNormal", "qwen-plus"); got.RPMMax != 100 {
		t.Errorf("normal fp RPMMax = %d, want 100", got.RPMMax)
	}
	if got := QuotaFor(cfg, "fpVIP", "qwen-plus"); got.RPMMax != 500 {
		t.Errorf("VIP fp RPMMax = %d, want 500 (override)", got.RPMMax)
	}
}

func TestKeyFingerprintStable(t *testing.T) {
	a := KeyFingerprint("sk-test-1234")
	b := KeyFingerprint("sk-test-1234")
	if a != b {
		t.Errorf("fingerprint should be deterministic")
	}
	if len(a) != 16 {
		t.Errorf("fingerprint length = %d, want 16", len(a))
	}
	if KeyFingerprint("") != "" {
		t.Errorf("empty key should yield empty fingerprint")
	}
}

func TestAcquireFallsThroughToBackend(t *testing.T) {
	// 验证 setBackendForTest helper 能注入假后端，整条 Acquire 走通且返回的 KeyIndex 正确。
	stub := &stubBackend{
		decision: Decision{Allowed: true},
	}
	setBackendForTest(stub)
	defer setBackendForTest(newMemoryBackend())

	cfg := &dto.SchedulerConfig{
		Enabled: true,
		Models:  map[string]dto.ModelQuota{"qwen-plus": {RPMMax: 10}},
	}
	candidates := []KeyCandidate{
		{Index: 0, Fp: "fp0"},
		{Index: 1, Fp: "fp1"},
	}
	res := Acquire(context.Background(), 1, "qwen-plus", candidates, cfg, "")
	if !res.Allowed {
		t.Fatalf("expect allowed, got %+v", res)
	}
	if stub.acquireCalls == 0 {
		t.Fatalf("backend.Acquire should be called")
	}
}

type stubBackend struct {
	decision     Decision
	acquireCalls int
}

func (s *stubBackend) Acquire(_ context.Context, _ int, _, _ string, _ dto.ModelQuota) Decision {
	s.acquireCalls++
	return s.decision
}
func (s *stubBackend) Peek(_ context.Context, _ int, _, _ string, _ dto.ModelQuota) Decision {
	return Decision{Allowed: true}
}
func (s *stubBackend) ReportSuccess(_ context.Context, _ int, _, _ string, _ int) {}
func (s *stubBackend) ReportError(_ context.Context, _ int, _, _ string)          {}
func (s *stubBackend) MarkCool(_ context.Context, _ int, _, _ string, _ int, _ string) {
}
func (s *stubBackend) Snapshot(_ context.Context, _ int) []StateEntry { return nil }
func (s *stubBackend) GetAffinity(_ context.Context, _ int, _, _ string) (string, bool) {
	return "", false
}
func (s *stubBackend) SetAffinity(_ context.Context, _ int, _, _, _ string, _ int) {}