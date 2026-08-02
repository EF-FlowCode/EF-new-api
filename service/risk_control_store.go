package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/go-redis/redis/v8"
)

// riskMetricStore tracks per-(scope, subjectID, group) counters and block
// state for the risk control engine. All methods short-circuit on empty group
// because the engine never persists data for unscoped requests.
type riskMetricStore interface {
	GetBlock(scope string, subjectID int, group string) (*types.RiskDecision, error)
	SetBlock(scope string, subjectID int, group string, decision *types.RiskDecision) error
	ClearBlock(scope string, subjectID int, group string) error
	RecordStart(scope string, subjectID int, group, ipHash, uaHash string, now time.Time) (types.RiskMetrics, error)
	RecordFinish(scope string, subjectID int, group string, now time.Time) (int, error)
	GetRuleHitCount(scope string, subjectID int, group string, now time.Time) (int, error)
	IncrementRuleHit(scope string, subjectID int, group string, now time.Time) (int, error)
}

func newRiskMetricStore() riskMetricStore {
	if common.RedisEnabled && common.RDB != nil {
		return &redisRiskMetricStore{
			rdb: common.RDB,
		}
	}
	return newMemoryRiskMetricStore()
}

type redisRiskMetricStore struct {
	rdb *redis.Client
}

func (s *redisRiskMetricStore) timeoutContext() (context.Context, context.CancelFunc) {
	timeout := time.Duration(operation_setting.GetRiskControlSetting().RedisTimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 30 * time.Millisecond
	}
	return context.WithTimeout(context.Background(), timeout)
}

func riskBlockKey(scope, group string, subjectID int) string {
	return fmt.Sprintf("rc:block:%s:%s:%d", scope, group, subjectID)
}

func riskInflightKey(scope, group string, subjectID int) string {
	return fmt.Sprintf("rc:inflight:%s:%s:%d", scope, group, subjectID)
}

func riskReqBucketKey(scope, group string, subjectID int, bucket int64) string {
	return fmt.Sprintf("rc:req:%s:%s:%d:%d", scope, group, subjectID, bucket)
}

func riskIPMinuteBucketKey(scope, group string, subjectID int, bucket int64) string {
	return fmt.Sprintf("rc:ip:min:%s:%s:%d:%d", scope, group, subjectID, bucket)
}

func riskIPHourBucketKey(scope, group string, subjectID int, bucket int64) string {
	return fmt.Sprintf("rc:ip:hour:%s:%s:%d:%d", scope, group, subjectID, bucket)
}

func riskUAMinuteBucketKey(scope, group string, subjectID int, bucket int64) string {
	return fmt.Sprintf("rc:ua:min:%s:%s:%d:%d", scope, group, subjectID, bucket)
}

func riskIPTokenBucketKey(group, ipHash string, bucket int64) string {
	return fmt.Sprintf("rc:ip-tkn:%s:%s:%d", group, ipHash, bucket)
}

func riskRuleHitKey(scope, group string, subjectID int) string {
	return fmt.Sprintf("rc:rule-hit:%s:%s:%d", scope, group, subjectID)
}

func (s *redisRiskMetricStore) GetBlock(scope string, subjectID int, group string) (*types.RiskDecision, error) {
	if subjectID <= 0 || group == "" {
		return nil, nil
	}
	ctx, cancel := s.timeoutContext()
	defer cancel()
	value, err := s.rdb.Get(ctx, riskBlockKey(scope, group, subjectID)).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil
		}
		return nil, err
	}
	var decision types.RiskDecision
	if err = common.UnmarshalJsonStr(value, &decision); err != nil {
		return nil, err
	}
	if decision.BlockUntil > 0 && decision.BlockUntil <= time.Now().Unix() {
		_ = s.ClearBlock(scope, subjectID, group)
		return nil, nil
	}
	return &decision, nil
}

func (s *redisRiskMetricStore) SetBlock(scope string, subjectID int, group string, decision *types.RiskDecision) error {
	if subjectID <= 0 || group == "" || decision == nil {
		return nil
	}
	if decision.BlockUntil <= 0 {
		return nil
	}
	ttl := time.Until(time.Unix(decision.BlockUntil, 0))
	if ttl <= 0 {
		return nil
	}
	ctx, cancel := s.timeoutContext()
	defer cancel()
	return s.rdb.Set(ctx, riskBlockKey(scope, group, subjectID), encodeRiskJSON(decision), ttl).Err()
}

func (s *redisRiskMetricStore) ClearBlock(scope string, subjectID int, group string) error {
	if subjectID <= 0 || group == "" {
		return nil
	}
	ctx, cancel := s.timeoutContext()
	defer cancel()
	return s.rdb.Del(ctx, riskBlockKey(scope, group, subjectID)).Err()
}

func (s *redisRiskMetricStore) RecordStart(scope string, subjectID int, group, ipHash, uaHash string, now time.Time) (types.RiskMetrics, error) {
	if subjectID <= 0 || group == "" {
		return types.RiskMetrics{}, nil
	}
	ctx, cancel := s.timeoutContext()
	defer cancel()

	minuteBucket := now.Unix() / 60
	hourBucket := now.Unix() / 600
	inflightKey := riskInflightKey(scope, group, subjectID)
	reqKey := riskReqBucketKey(scope, group, subjectID, minuteBucket)
	ipMinuteKey := riskIPMinuteBucketKey(scope, group, subjectID, minuteBucket)
	ipHourKey := riskIPHourBucketKey(scope, group, subjectID, hourBucket)
	uaMinuteKey := riskUAMinuteBucketKey(scope, group, subjectID, minuteBucket)
	ipTokenKey := ""
	if ipHash != "" && scope == RiskSubjectTypeToken {
		ipTokenKey = riskIPTokenBucketKey(group, ipHash, minuteBucket)
	}

	pipe := s.rdb.TxPipeline()
	inflightCmd := pipe.Incr(ctx, inflightKey)
	pipe.Expire(ctx, inflightKey, 2*time.Hour)
	reqCmd := pipe.Incr(ctx, reqKey)
	pipe.Expire(ctx, reqKey, 20*time.Minute)
	if ipHash != "" {
		pipe.PFAdd(ctx, ipMinuteKey, ipHash)
		pipe.Expire(ctx, ipMinuteKey, 20*time.Minute)
		pipe.PFAdd(ctx, ipHourKey, ipHash)
		pipe.Expire(ctx, ipHourKey, 2*time.Hour)
	}
	if scope == RiskSubjectTypeToken && uaHash != "" {
		pipe.PFAdd(ctx, uaMinuteKey, uaHash)
		pipe.Expire(ctx, uaMinuteKey, 20*time.Minute)
	}
	if ipTokenKey != "" {
		pipe.PFAdd(ctx, ipTokenKey, strconv.Itoa(subjectID))
		pipe.Expire(ctx, ipTokenKey, 20*time.Minute)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return types.RiskMetrics{}, err
	}

	req10M, err := s.sumRequestBuckets(ctx, scope, group, subjectID, minuteBucket, 10)
	if err != nil {
		return types.RiskMetrics{}, err
	}
	distinctIP10M, err := s.countUnion(ctx, buildScopedMinuteKeys(riskIPMinuteBucketKey, scope, group, subjectID, minuteBucket, 10))
	if err != nil {
		return types.RiskMetrics{}, err
	}
	distinctIP1H, err := s.countUnion(ctx, buildScopedMinuteKeys(riskIPHourBucketKey, scope, group, subjectID, hourBucket, 6))
	if err != nil {
		return types.RiskMetrics{}, err
	}
	distinctUA10M := 0
	if scope == RiskSubjectTypeToken {
		distinctUA10M, err = s.countUnion(ctx, buildScopedMinuteKeys(riskUAMinuteBucketKey, scope, group, subjectID, minuteBucket, 10))
		if err != nil {
			return types.RiskMetrics{}, err
		}
	}
	tokensPerIP10M := 0
	if ipTokenKey != "" {
		tokensPerIP10M, err = s.countUnion(ctx, buildIPTokenKeys(group, ipHash, minuteBucket, 10))
		if err != nil {
			return types.RiskMetrics{}, err
		}
	}
	return types.RiskMetrics{
		DistinctIP10M:   distinctIP10M,
		DistinctIP1H:    distinctIP1H,
		DistinctUA10M:   distinctUA10M,
		TokensPerIP10M:  tokensPerIP10M,
		RequestCount1M:  int(reqCmd.Val()),
		RequestCount10M: req10M,
		InflightNow:     int(inflightCmd.Val()),
	}, nil
}

func (s *redisRiskMetricStore) RecordFinish(scope string, subjectID int, group string, now time.Time) (int, error) {
	if subjectID <= 0 || group == "" {
		return 0, nil
	}
	ctx, cancel := s.timeoutContext()
	defer cancel()
	value, err := s.rdb.Decr(ctx, riskInflightKey(scope, group, subjectID)).Result()
	if err != nil {
		return 0, err
	}
	if value < 0 {
		if err = s.rdb.Set(ctx, riskInflightKey(scope, group, subjectID), 0, 2*time.Hour).Err(); err != nil {
			return 0, err
		}
		return 0, nil
	}
	return int(value), nil
}

func (s *redisRiskMetricStore) GetRuleHitCount(scope string, subjectID int, group string, now time.Time) (int, error) {
	if subjectID <= 0 || group == "" {
		return 0, nil
	}
	ctx, cancel := s.timeoutContext()
	defer cancel()
	value, err := s.rdb.Get(ctx, riskRuleHitKey(scope, group, subjectID)).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return 0, nil
		}
		return 0, err
	}
	count, err := strconv.Atoi(value)
	if err != nil {
		return 0, nil
	}
	return count, nil
}

func (s *redisRiskMetricStore) IncrementRuleHit(scope string, subjectID int, group string, now time.Time) (int, error) {
	if subjectID <= 0 || group == "" {
		return 0, nil
	}
	ctx, cancel := s.timeoutContext()
	defer cancel()
	key := riskRuleHitKey(scope, group, subjectID)
	pipe := s.rdb.TxPipeline()
	incrCmd := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, 24*time.Hour)
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, err
	}
	return int(incrCmd.Val()), nil
}

func (s *redisRiskMetricStore) sumRequestBuckets(ctx context.Context, scope, group string, subjectID int, minuteBucket int64, size int) (int, error) {
	keys := make([]string, 0, size)
	for i := 0; i < size; i++ {
		keys = append(keys, riskReqBucketKey(scope, group, subjectID, minuteBucket-int64(i)))
	}
	values, err := s.rdb.MGet(ctx, keys...).Result()
	if err != nil {
		return 0, err
	}
	total := 0
	for _, value := range values {
		switch raw := value.(type) {
		case string:
			num, convErr := strconv.Atoi(raw)
			if convErr == nil {
				total += num
			}
		case nil:
		default:
			num, convErr := strconv.Atoi(fmt.Sprintf("%v", raw))
			if convErr == nil {
				total += num
			}
		}
	}
	return total, nil
}

func (s *redisRiskMetricStore) countUnion(ctx context.Context, keys []string) (int, error) {
	if len(keys) == 0 {
		return 0, nil
	}
	count, err := s.rdb.PFCount(ctx, keys...).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return 0, nil
		}
		return 0, err
	}
	return int(count), nil
}

func buildScopedMinuteKeys(builder func(string, string, int, int64) string, scope, group string, subjectID int, currentBucket int64, size int) []string {
	keys := make([]string, 0, size)
	for i := 0; i < size; i++ {
		keys = append(keys, builder(scope, group, subjectID, currentBucket-int64(i)))
	}
	return keys
}

func buildIPTokenKeys(group, hash string, currentBucket int64, size int) []string {
	keys := make([]string, 0, size)
	for i := 0; i < size; i++ {
		keys = append(keys, riskIPTokenBucketKey(group, hash, currentBucket-int64(i)))
	}
	return keys
}

// memoryRiskMetricStore is the fallback when Redis is not configured. The map
// keys are (scope, subjectID, group) so the same token can be tracked across
// multiple groups without contention.
type memoryRiskMetricStore struct {
	mu          sync.Mutex
	subject     map[string]*memoryRiskSubjectState
	ipTokens    map[string]map[int64]map[string]struct{} // key = group + ":" + ipHash
	lastSweepAt int64
}

type memoryRiskSubjectState struct {
	Block       *types.RiskDecision
	Inflight    int
	ReqBuckets  map[int64]int
	IP10Buckets map[int64]map[string]struct{}
	IP1HBuckets map[int64]map[string]struct{}
	UA10Buckets map[int64]map[string]struct{}
	RuleHits    []int64
}

func newMemoryRiskMetricStore() *memoryRiskMetricStore {
	return &memoryRiskMetricStore{
		subject:  make(map[string]*memoryRiskSubjectState),
		ipTokens: make(map[string]map[int64]map[string]struct{}),
	}
}

func riskMemoryKey(scope string, subjectID int, group string) string {
	return scope + ":" + group + ":" + strconv.Itoa(subjectID)
}

func memoryIPTokenKey(group, ipHash string) string {
	return group + ":" + ipHash
}

func newMemoryRiskSubjectState() *memoryRiskSubjectState {
	return &memoryRiskSubjectState{
		ReqBuckets:  make(map[int64]int),
		IP10Buckets: make(map[int64]map[string]struct{}),
		IP1HBuckets: make(map[int64]map[string]struct{}),
		UA10Buckets: make(map[int64]map[string]struct{}),
	}
}

func (s *memoryRiskMetricStore) getState(key string) *memoryRiskSubjectState {
	state := s.subject[key]
	if state == nil {
		state = newMemoryRiskSubjectState()
		s.subject[key] = state
	}
	return state
}

func (s *memoryRiskMetricStore) cleanState(state *memoryRiskSubjectState, now time.Time) {
	if state == nil {
		return
	}
	currentMinute := now.Unix() / 60
	currentTenMinute := now.Unix() / 600
	for bucket := range state.ReqBuckets {
		if bucket < currentMinute-10 {
			delete(state.ReqBuckets, bucket)
		}
	}
	for bucket := range state.IP10Buckets {
		if bucket < currentMinute-10 {
			delete(state.IP10Buckets, bucket)
		}
	}
	for bucket := range state.UA10Buckets {
		if bucket < currentMinute-10 {
			delete(state.UA10Buckets, bucket)
		}
	}
	for bucket := range state.IP1HBuckets {
		if bucket < currentTenMinute-6 {
			delete(state.IP1HBuckets, bucket)
		}
	}
	filtered := make([]int64, 0, len(state.RuleHits))
	cutoff := now.Add(-24 * time.Hour).Unix()
	for _, ts := range state.RuleHits {
		if ts >= cutoff {
			filtered = append(filtered, ts)
		}
	}
	state.RuleHits = filtered
	if state.Block != nil && state.Block.BlockUntil > 0 && state.Block.BlockUntil <= now.Unix() {
		state.Block = nil
	}
}

func (s *memoryRiskMetricStore) cleanIPTokenState(key string, now time.Time) map[int64]map[string]struct{} {
	buckets := s.ipTokens[key]
	if buckets == nil {
		return nil
	}
	currentMinute := now.Unix() / 60
	for bucket := range buckets {
		if bucket < currentMinute-10 {
			delete(buckets, bucket)
		}
	}
	if len(buckets) == 0 {
		delete(s.ipTokens, key)
		return nil
	}
	return buckets
}

func (s *memoryRiskMetricStore) maybeSweep(now time.Time) {
	const sweepInterval = 5 * time.Minute
	if s.lastSweepAt > 0 && now.Unix()-s.lastSweepAt < int64(sweepInterval.Seconds()) {
		return
	}
	for key, state := range s.subject {
		s.cleanState(state, now)
		if isMemoryRiskStateIdle(state) {
			delete(s.subject, key)
		}
	}
	for key := range s.ipTokens {
		s.cleanIPTokenState(key, now)
	}
	s.lastSweepAt = now.Unix()
}

func isMemoryRiskStateIdle(state *memoryRiskSubjectState) bool {
	if state == nil {
		return true
	}
	return state.Block == nil &&
		state.Inflight <= 0 &&
		len(state.ReqBuckets) == 0 &&
		len(state.IP10Buckets) == 0 &&
		len(state.IP1HBuckets) == 0 &&
		len(state.UA10Buckets) == 0 &&
		len(state.RuleHits) == 0
}

func (s *memoryRiskMetricStore) GetBlock(scope string, subjectID int, group string) (*types.RiskDecision, error) {
	if subjectID <= 0 || group == "" {
		return nil, nil
	}
	now := time.Now()
	key := riskMemoryKey(scope, subjectID, group)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.maybeSweep(now)
	state := s.subject[key]
	if state == nil {
		return nil, nil
	}
	s.cleanState(state, now)
	if isMemoryRiskStateIdle(state) {
		delete(s.subject, key)
		return nil, nil
	}
	if state.Block == nil {
		return nil, nil
	}
	decision := *state.Block
	return &decision, nil
}

func (s *memoryRiskMetricStore) SetBlock(scope string, subjectID int, group string, decision *types.RiskDecision) error {
	if subjectID <= 0 || group == "" || decision == nil {
		return nil
	}
	key := riskMemoryKey(scope, subjectID, group)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.maybeSweep(time.Now())
	state := s.getState(key)
	copied := *decision
	state.Block = &copied
	return nil
}

func (s *memoryRiskMetricStore) ClearBlock(scope string, subjectID int, group string) error {
	if subjectID <= 0 || group == "" {
		return nil
	}
	now := time.Now()
	key := riskMemoryKey(scope, subjectID, group)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.maybeSweep(now)
	state := s.subject[key]
	if state == nil {
		return nil
	}
	state.Block = nil
	if isMemoryRiskStateIdle(state) {
		delete(s.subject, key)
	}
	return nil
}

func (s *memoryRiskMetricStore) RecordStart(scope string, subjectID int, group, ipHash, uaHash string, now time.Time) (types.RiskMetrics, error) {
	if subjectID <= 0 || group == "" {
		return types.RiskMetrics{}, nil
	}
	key := riskMemoryKey(scope, subjectID, group)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.maybeSweep(now)
	state := s.getState(key)
	s.cleanState(state, now)
	minuteBucket := now.Unix() / 60
	hourBucket := now.Unix() / 600
	state.Inflight++
	state.ReqBuckets[minuteBucket]++
	tokensPerIP10M := 0
	if ipHash != "" {
		if state.IP10Buckets[minuteBucket] == nil {
			state.IP10Buckets[minuteBucket] = make(map[string]struct{})
		}
		state.IP10Buckets[minuteBucket][ipHash] = struct{}{}
		if state.IP1HBuckets[hourBucket] == nil {
			state.IP1HBuckets[hourBucket] = make(map[string]struct{})
		}
		state.IP1HBuckets[hourBucket][ipHash] = struct{}{}
	}
	if scope == RiskSubjectTypeToken && ipHash != "" {
		ipTokenK := memoryIPTokenKey(group, ipHash)
		ipBuckets := s.cleanIPTokenState(ipTokenK, now)
		if ipBuckets == nil {
			ipBuckets = make(map[int64]map[string]struct{})
			s.ipTokens[ipTokenK] = ipBuckets
		}
		if ipBuckets[minuteBucket] == nil {
			ipBuckets[minuteBucket] = make(map[string]struct{})
		}
		ipBuckets[minuteBucket][strconv.Itoa(subjectID)] = struct{}{}
		tokensPerIP10M = countUniqueFromBuckets(ipBuckets)
	}
	if scope == RiskSubjectTypeToken && uaHash != "" {
		if state.UA10Buckets[minuteBucket] == nil {
			state.UA10Buckets[minuteBucket] = make(map[string]struct{})
		}
		state.UA10Buckets[minuteBucket][uaHash] = struct{}{}
	}
	return types.RiskMetrics{
		DistinctIP10M:   countUniqueFromBuckets(state.IP10Buckets),
		DistinctIP1H:    countUniqueFromBuckets(state.IP1HBuckets),
		DistinctUA10M:   countUniqueFromBuckets(state.UA10Buckets),
		TokensPerIP10M:  tokensPerIP10M,
		RequestCount1M:  state.ReqBuckets[minuteBucket],
		RequestCount10M: countSumFromBuckets(state.ReqBuckets),
		InflightNow:     state.Inflight,
		RuleHitCount24H: len(state.RuleHits),
	}, nil
}

func (s *memoryRiskMetricStore) RecordFinish(scope string, subjectID int, group string, now time.Time) (int, error) {
	if subjectID <= 0 || group == "" {
		return 0, nil
	}
	key := riskMemoryKey(scope, subjectID, group)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.maybeSweep(now)
	state := s.subject[key]
	if state == nil {
		return 0, nil
	}
	s.cleanState(state, now)
	if state.Inflight > 0 {
		state.Inflight--
	}
	if isMemoryRiskStateIdle(state) {
		delete(s.subject, key)
		return 0, nil
	}
	return state.Inflight, nil
}

func (s *memoryRiskMetricStore) GetRuleHitCount(scope string, subjectID int, group string, now time.Time) (int, error) {
	if subjectID <= 0 || group == "" {
		return 0, nil
	}
	key := riskMemoryKey(scope, subjectID, group)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.maybeSweep(now)
	state := s.subject[key]
	if state == nil {
		return 0, nil
	}
	s.cleanState(state, now)
	if isMemoryRiskStateIdle(state) {
		delete(s.subject, key)
		return 0, nil
	}
	return len(state.RuleHits), nil
}

func (s *memoryRiskMetricStore) IncrementRuleHit(scope string, subjectID int, group string, now time.Time) (int, error) {
	if subjectID <= 0 || group == "" {
		return 0, nil
	}
	key := riskMemoryKey(scope, subjectID, group)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.maybeSweep(now)
	state := s.getState(key)
	s.cleanState(state, now)
	state.RuleHits = append(state.RuleHits, now.Unix())
	return len(state.RuleHits), nil
}

func countUniqueFromBuckets[T comparable](buckets map[int64]map[T]struct{}) int {
	set := make(map[T]struct{})
	for _, bucket := range buckets {
		for value := range bucket {
			set[value] = struct{}{}
		}
	}
	return len(set)
}

func countSumFromBuckets(buckets map[int64]int) int {
	total := 0
	for _, value := range buckets {
		total += value
	}
	return total
}
