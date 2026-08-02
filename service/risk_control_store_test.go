package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/types"
)

const (
	testGroupVip  = "vip"
	testGroupFree = "free"
)

func TestMemoryRiskMetricStoreReadPathDoesNotCreateState(t *testing.T) {
	store := newMemoryRiskMetricStore()
	now := time.Unix(1713175200, 0)

	block, err := store.GetBlock(RiskSubjectTypeToken, 1001, testGroupVip)
	if err != nil {
		t.Fatalf("GetBlock returned error: %v", err)
	}
	if block != nil {
		t.Fatalf("GetBlock returned unexpected block: %#v", block)
	}
	if err = store.ClearBlock(RiskSubjectTypeToken, 1001, testGroupVip); err != nil {
		t.Fatalf("ClearBlock returned error: %v", err)
	}
	inflight, err := store.RecordFinish(RiskSubjectTypeToken, 1001, testGroupVip, now)
	if err != nil {
		t.Fatalf("RecordFinish returned error: %v", err)
	}
	if inflight != 0 {
		t.Fatalf("RecordFinish returned unexpected inflight: %d", inflight)
	}
	hitCount, err := store.GetRuleHitCount(RiskSubjectTypeToken, 1001, testGroupVip, now)
	if err != nil {
		t.Fatalf("GetRuleHitCount returned error: %v", err)
	}
	if hitCount != 0 {
		t.Fatalf("GetRuleHitCount returned unexpected count: %d", hitCount)
	}
	if got := len(store.subject); got != 0 {
		t.Fatalf("read-only paths should not create memory states, got %d", got)
	}
}

func TestMemoryRiskMetricStoreSweepsIdleSubjectState(t *testing.T) {
	store := newMemoryRiskMetricStore()
	now := time.Unix(1713175200, 0)

	metrics, err := store.RecordStart(RiskSubjectTypeToken, 1001, testGroupVip, "ip-1", "ua-1", now)
	if err != nil {
		t.Fatalf("RecordStart returned error: %v", err)
	}
	if metrics.RequestCount1M != 1 {
		t.Fatalf("RecordStart returned unexpected metrics: %#v", metrics)
	}
	if metrics.TokensPerIP10M != 1 {
		t.Fatalf("expected tokens_per_ip_10m to be 1, got %#v", metrics)
	}
	if _, err = store.RecordFinish(RiskSubjectTypeToken, 1001, testGroupVip, now); err != nil {
		t.Fatalf("RecordFinish returned error: %v", err)
	}
	if got := len(store.subject); got != 1 {
		t.Fatalf("expected one active memory state, got %d", got)
	}
	if got := len(store.ipTokens); got != 1 {
		t.Fatalf("expected one active ip->tokens state, got %d", got)
	}

	// Trigger a global sweep from another key before the original subject goes idle.
	if _, err = store.GetRuleHitCount(RiskSubjectTypeToken, 2002, testGroupVip, now); err != nil {
		t.Fatalf("GetRuleHitCount before idle window returned error: %v", err)
	}
	if got := len(store.subject); got != 1 {
		t.Fatalf("state should still exist before retention windows expire, got %d", got)
	}

	later := now.Add(2 * time.Hour)
	if _, err = store.GetRuleHitCount(RiskSubjectTypeToken, 3003, testGroupVip, later); err != nil {
		t.Fatalf("GetRuleHitCount during sweep returned error: %v", err)
	}
	if got := len(store.subject); got != 0 {
		t.Fatalf("idle subject state should be swept after retention windows expire, got %d", got)
	}
	if got := len(store.ipTokens); got != 0 {
		t.Fatalf("idle ip->tokens state should be swept after retention windows expire, got %d", got)
	}
}

func TestMemoryRiskMetricStoreCountsTokensPerIPWithinGroup(t *testing.T) {
	store := newMemoryRiskMetricStore()
	now := time.Unix(1713175200, 0)

	metrics1, err := store.RecordStart(RiskSubjectTypeToken, 1001, testGroupVip, "ip-shared", "ua-1", now)
	if err != nil {
		t.Fatalf("RecordStart token 1001 returned error: %v", err)
	}
	if metrics1.TokensPerIP10M != 1 {
		t.Fatalf("expected first token to see tokens_per_ip_10m=1, got %#v", metrics1)
	}

	metrics2, err := store.RecordStart(RiskSubjectTypeToken, 1002, testGroupVip, "ip-shared", "ua-2", now.Add(time.Minute))
	if err != nil {
		t.Fatalf("RecordStart token 1002 returned error: %v", err)
	}
	if metrics2.TokensPerIP10M != 2 {
		t.Fatalf("expected second token to see tokens_per_ip_10m=2, got %#v", metrics2)
	}

	userMetrics, err := store.RecordStart(RiskSubjectTypeUser, 2001, testGroupVip, "ip-shared", "", now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("RecordStart user returned error: %v", err)
	}
	if userMetrics.TokensPerIP10M != 0 {
		t.Fatalf("expected user scope not to populate tokens_per_ip_10m, got %#v", userMetrics)
	}
}

// TestStoreTripleKeyIsolationAcrossGroups verifies the v4 invariant that the
// same (scope, subjectID) tracked under different groups must not bleed
// metrics into each other (DEV_GUIDE §5 red line "三元组隔离").
func TestStoreTripleKeyIsolationAcrossGroups(t *testing.T) {
	store := newMemoryRiskMetricStore()
	now := time.Unix(1713175200, 0)

	mVip, err := store.RecordStart(RiskSubjectTypeToken, 7000, testGroupVip, "ipA", "uaA", now)
	if err != nil {
		t.Fatalf("vip RecordStart: %v", err)
	}
	mFree, err := store.RecordStart(RiskSubjectTypeToken, 7000, testGroupFree, "ipB", "uaB", now)
	if err != nil {
		t.Fatalf("free RecordStart: %v", err)
	}
	if mVip.RequestCount1M != 1 || mFree.RequestCount1M != 1 {
		t.Fatalf("expected each group to see its own RequestCount1M=1, got vip=%d free=%d", mVip.RequestCount1M, mFree.RequestCount1M)
	}
	if mVip.DistinctIP10M != 1 || mFree.DistinctIP10M != 1 {
		t.Fatalf("expected DistinctIP10M=1 in each group; got vip=%d free=%d", mVip.DistinctIP10M, mFree.DistinctIP10M)
	}
	if mVip.InflightNow != 1 || mFree.InflightNow != 1 {
		t.Fatalf("expected InflightNow=1 in each group; got vip=%d free=%d", mVip.InflightNow, mFree.InflightNow)
	}
}

// TestStoreSetBlockScopedToGroup pins block isolation: blocking the vip group
// must not affect free group lookups for the same subject.
func TestStoreSetBlockScopedToGroup(t *testing.T) {
	store := newMemoryRiskMetricStore()
	decision := &types.RiskDecision{
		Scope:      RiskSubjectTypeToken,
		SubjectID:  9000,
		Group:      testGroupVip,
		Decision:   RiskDecisionBlock,
		Action:     RiskActionBlock,
		BlockUntil: time.Now().Add(time.Hour).Unix(),
	}
	if err := store.SetBlock(RiskSubjectTypeToken, 9000, testGroupVip, decision); err != nil {
		t.Fatalf("SetBlock vip: %v", err)
	}
	gotVip, err := store.GetBlock(RiskSubjectTypeToken, 9000, testGroupVip)
	if err != nil {
		t.Fatalf("GetBlock vip: %v", err)
	}
	if gotVip == nil {
		t.Fatal("expected vip block to be present")
	}
	gotFree, err := store.GetBlock(RiskSubjectTypeToken, 9000, testGroupFree)
	if err != nil {
		t.Fatalf("GetBlock free: %v", err)
	}
	if gotFree != nil {
		t.Fatalf("expected free group to be unblocked, got %#v", gotFree)
	}
	if err := store.ClearBlock(RiskSubjectTypeToken, 9000, testGroupVip); err != nil {
		t.Fatalf("ClearBlock vip: %v", err)
	}
	cleared, err := store.GetBlock(RiskSubjectTypeToken, 9000, testGroupVip)
	if err != nil {
		t.Fatalf("GetBlock after clear: %v", err)
	}
	if cleared != nil {
		t.Fatal("expected vip block to be cleared")
	}
}

// TestStoreEmptyGroupIsNoop documents the defensive short-circuit: store
// methods called with an empty group return zero values without mutating any
// internal state. Engine-side guards make this unreachable in practice, but
// the contract keeps regressions safe.
func TestStoreEmptyGroupIsNoop(t *testing.T) {
	store := newMemoryRiskMetricStore()
	now := time.Unix(1713175200, 0)
	metrics, err := store.RecordStart(RiskSubjectTypeToken, 1, "", "ip", "ua", now)
	if err != nil {
		t.Fatalf("RecordStart empty group: %v", err)
	}
	if metrics.RequestCount1M != 0 {
		t.Fatalf("expected empty-group to return zero metrics, got %#v", metrics)
	}
	if got := len(store.subject); got != 0 {
		t.Fatalf("empty-group RecordStart must not allocate state, got %d", got)
	}
	if err := store.SetBlock(RiskSubjectTypeToken, 1, "", &types.RiskDecision{BlockUntil: now.Add(time.Hour).Unix()}); err != nil {
		t.Fatalf("SetBlock empty group: %v", err)
	}
	if got := len(store.subject); got != 0 {
		t.Fatalf("empty-group SetBlock must not allocate state, got %d", got)
	}
}
