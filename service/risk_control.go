package service

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"eflow.local/new-api/common"
	gopool "eflow.local/new-api/internal/drainpool"
	"eflow.local/new-api/model"
	"eflow.local/new-api/pkg/requestip"
	relaycommon "eflow.local/new-api/relay/common"
	"eflow.local/new-api/setting/operation_setting"
	"eflow.local/new-api/types"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type localRiskGateCacheEntry struct {
	Decision  *types.RiskDecision
	ExpiresAt int64
}

type riskControlCenter struct {
	started   atomic.Bool
	store     riskMetricStore
	queue     chan *RiskEvent
	rules     atomic.Value
	cache     sync.Map
	dropCount atomic.Int64
}

var globalRiskCenter = &riskControlCenter{}

type riskMetricDefinition struct {
	AllowedScopes map[string]struct{}
}

var riskMetricDefinitions = map[string]riskMetricDefinition{
	"distinct_ip_10m":   {AllowedScopes: newRiskMetricScopeSet(RiskSubjectTypeToken, RiskSubjectTypeUser)},
	"distinct_ip_1h":    {AllowedScopes: newRiskMetricScopeSet(RiskSubjectTypeToken, RiskSubjectTypeUser)},
	"distinct_ua_10m":   {AllowedScopes: newRiskMetricScopeSet(RiskSubjectTypeToken)},
	"tokens_per_ip_10m": {AllowedScopes: newRiskMetricScopeSet(RiskSubjectTypeToken)},
	"request_count_1m":  {AllowedScopes: newRiskMetricScopeSet(RiskSubjectTypeToken, RiskSubjectTypeUser)},
	"req_1m":            {AllowedScopes: newRiskMetricScopeSet(RiskSubjectTypeToken, RiskSubjectTypeUser)},
	"request_count_10m": {AllowedScopes: newRiskMetricScopeSet(RiskSubjectTypeToken, RiskSubjectTypeUser)},
	"req_10m":           {AllowedScopes: newRiskMetricScopeSet(RiskSubjectTypeToken, RiskSubjectTypeUser)},
	"inflight_now":      {AllowedScopes: newRiskMetricScopeSet(RiskSubjectTypeToken, RiskSubjectTypeUser)},
	"rule_hit_count_24h": {AllowedScopes: newRiskMetricScopeSet(
		RiskSubjectTypeToken,
		RiskSubjectTypeUser,
	)},
	"risk_score": {AllowedScopes: newRiskMetricScopeSet(RiskSubjectTypeToken, RiskSubjectTypeUser)},
}

func StartRiskControlCenter() {
	globalRiskCenter.start()
}

func ReloadRiskRules() error {
	return globalRiskCenter.reloadRules()
}

func GetRiskControlConfig() *operation_setting.RiskControlSetting {
	return operation_setting.GetRiskControlSetting()
}

// isRiskControlActiveForGroup is the engine-side wrapper around
// operation_setting.IsRiskControlEnabledForGroup so callers don't need to
// import the setting package. It is the single gate every BeforeRelay /
// AfterRelay / enqueue path consults.
func isRiskControlActiveForGroup(cfg *operation_setting.RiskControlSetting, group string) bool {
	return operation_setting.IsRiskControlEnabledForGroup(cfg, group)
}

func newRiskMetricScopeSet(scopes ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(scopes))
	for _, scope := range scopes {
		if scope == "" {
			continue
		}
		set[scope] = struct{}{}
	}
	return set
}

func getRiskMetricDefinition(metric string) (riskMetricDefinition, bool) {
	def, ok := riskMetricDefinitions[strings.TrimSpace(metric)]
	return def, ok
}

func isRiskMetricAllowedForScope(metric string, scope string) bool {
	def, ok := getRiskMetricDefinition(metric)
	if !ok {
		return false
	}
	_, allowed := def.AllowedScopes[scope]
	return allowed
}

func riskMetricAllowedScopes(metric string) []string {
	def, ok := getRiskMetricDefinition(metric)
	if !ok {
		return nil
	}
	scopes := make([]string, 0, len(def.AllowedScopes))
	if _, ok = def.AllowedScopes[RiskSubjectTypeToken]; ok {
		scopes = append(scopes, RiskSubjectTypeToken)
	}
	if _, ok = def.AllowedScopes[RiskSubjectTypeUser]; ok {
		scopes = append(scopes, RiskSubjectTypeUser)
	}
	return scopes
}

// ruleAppliesToGroup returns true when the compiled rule is configured for the
// given group. Unconfigured rules (empty Groups set) are filtered out at reload
// time, so this only needs to test set membership.
func ruleAppliesToGroup(rule *compiledRiskRule, group string) bool {
	if rule == nil || len(rule.Groups) == 0 {
		return false
	}
	_, ok := rule.Groups[group]
	return ok
}

// RiskControlBeforeRelay is invoked from controller/relay.go after relay info
// has been resolved. It snapshots info.UsingGroup into info.RiskGroup, decides
// whether the request participates in risk control at all, and (in enforce
// mode) consults the gate cache for an existing block decision.
func RiskControlBeforeRelay(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	if info == nil {
		return nil
	}
	// Snapshot once — auto cross-group retry rewrites UsingGroup later.
	info.RiskGroup = info.UsingGroup
	cfg := operation_setting.GetRiskControlSetting()
	if !isRiskControlActiveForGroup(cfg, info.RiskGroup) {
		return nil
	}
	decision := globalRiskCenter.beforeRelay(c, info, cfg)
	if decision == nil {
		return nil
	}
	if info.RiskAudit == nil {
		info.RiskAudit = &types.RiskAudit{}
	}
	info.RiskAudit.FinalDecision = decision.Decision
	info.RiskAudit.FinalReason = decision.Reason
	if decision.Scope == RiskSubjectTypeToken {
		info.RiskAudit.TokenDecision = decision
	} else {
		info.RiskAudit.UserDecision = decision
	}
	message := normalizeRiskMessage(decision.ResponseMessage)
	statusCode := decision.StatusCode
	if statusCode <= 0 {
		statusCode = cfg.DefaultStatusCode
	}
	return types.NewErrorWithStatusCode(
		errors.New(message),
		types.ErrorCodeRiskControlBlocked,
		statusCode,
		types.ErrOptionWithSkipRetry(),
	)
}

// RiskControlAfterRelay enqueues the finish event using info.RiskGroup so that
// the start/finish pair always lands on the same group bucket even if the
// engine retried into another group meanwhile.
func RiskControlAfterRelay(c *gin.Context, info *relaycommon.RelayInfo, relayErr *types.NewAPIError) {
	if info == nil {
		return
	}
	cfg := operation_setting.GetRiskControlSetting()
	if !isRiskControlActiveForGroup(cfg, info.RiskGroup) {
		return
	}
	globalRiskCenter.afterRelay(c, info, relayErr)
}

func GetRiskOverview() (map[string]any, error) {
	observed, err := model.CountRiskSubjectSnapshotsByStatus(RiskStatusObserve)
	if err != nil {
		return nil, err
	}
	blocked, err := model.CountRiskSubjectSnapshotsByStatus(RiskStatusBlocked)
	if err != nil {
		return nil, err
	}
	highRisk, err := model.CountHighRiskSubjectSnapshots(60)
	if err != nil {
		return nil, err
	}
	ruleCount, err := model.CountRiskRules()
	if err != nil {
		return nil, err
	}
	unconfigured, err := model.CountEnabledRiskRulesWithoutGroups()
	if err != nil {
		return nil, err
	}
	cfg := operation_setting.GetRiskControlSetting()
	groupUnlistedRuleCount, err := countEnabledRulesWithAllGroupsUnlisted(cfg)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"observed_subjects":         observed,
		"blocked_subjects":          blocked,
		"high_risk_subjects":        highRisk,
		"rule_count":                ruleCount,
		"unconfigured_rule_count":   unconfigured,
		"group_unlisted_rule_count": groupUnlistedRuleCount,
		"enabled_group_count":       len(cfg.EnabledGroups),
		"queue_dropped":             globalRiskCenter.dropCount.Load(),
		"mode":                      cfg.Mode,
		"enabled":                   cfg.Enabled,
	}, nil
}

// countEnabledRulesWithAllGroupsUnlisted returns the number of rules where
// Enabled=true and every entry in Groups is missing from EnabledGroups. These
// rules silently never fire and are surfaced on the overview card so admins
// can spot the misconfiguration.
func countEnabledRulesWithAllGroupsUnlisted(cfg *operation_setting.RiskControlSetting) (int64, error) {
	rules, err := model.ListEnabledRiskRulesAll()
	if err != nil {
		return 0, err
	}
	whitelist := make(map[string]struct{}, len(cfg.EnabledGroups))
	for _, g := range cfg.EnabledGroups {
		whitelist[g] = struct{}{}
	}
	var n int64
	for _, rule := range rules {
		if rule == nil {
			continue
		}
		groups := rule.ParsedGroups()
		if len(groups) == 0 {
			// already counted via CountEnabledRiskRulesWithoutGroups
			continue
		}
		anyListed := false
		for _, g := range groups {
			if _, ok := whitelist[g]; ok {
				anyListed = true
				break
			}
		}
		if !anyListed {
			n++
		}
	}
	return n, nil
}

func ListRiskRules() ([]*model.RiskRule, error) {
	return model.ListRiskRules()
}

func CreateRiskRule(rule *model.RiskRule) error {
	if err := validateRiskRule(rule); err != nil {
		return err
	}
	if err := model.CreateRiskRule(rule); err != nil {
		return err
	}
	return reloadRiskRulesAndPublish()
}

func UpdateRiskRule(rule *model.RiskRule) error {
	if err := validateRiskRule(rule); err != nil {
		return err
	}
	if err := model.UpdateRiskRule(rule); err != nil {
		return err
	}
	return reloadRiskRulesAndPublish()
}

func DeleteRiskRule(id int) error {
	if err := model.DeleteRiskRule(id); err != nil {
		return err
	}
	return reloadRiskRulesAndPublish()
}

// reloadRiskRulesAndPublish 重载本实例规则后通知其他实例；数据库已提交，
// 即使本地重载失败也要通知，其他实例各自从数据库读取。
func reloadRiskRulesAndPublish() error {
	err := ReloadRiskRules()
	publishRuntimeRevision(runtimeSyncRiskRulesKey)
	return err
}

func ListRiskSubjectSnapshots(query model.RiskSubjectQuery, pageInfo *common.PageInfo) ([]*model.RiskSubjectSnapshot, int64, error) {
	return model.ListRiskSubjectSnapshots(query, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
}

func ListRiskIncidents(query model.RiskIncidentQuery, pageInfo *common.PageInfo) ([]*model.RiskIncident, int64, error) {
	return model.ListRiskIncidents(query, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
}

// UnblockRiskSubject clears the (scope, subjectID, group) block and writes a
// manual_unblock incident. It deliberately does NOT validate that group is
// inside the EnabledGroups whitelist — admins must be able to clean up legacy
// blocks left behind after a group is removed from the whitelist (see
// DEV_GUIDE §5 red line "解封粒度对齐评估粒度" and §12.2).
func UnblockRiskSubject(scope string, subjectID int, group, operator string) error {
	if scope != RiskSubjectTypeToken && scope != RiskSubjectTypeUser {
		return errors.New("invalid subject scope")
	}
	if subjectID <= 0 {
		return errors.New("invalid subject id")
	}
	if strings.TrimSpace(group) == "" {
		return errors.New("group is required")
	}
	if err := globalRiskCenter.store.ClearBlock(scope, subjectID, group); err != nil {
		return err
	}
	globalRiskCenter.cache.Delete(riskMemoryKey(scope, subjectID, group))
	// 其他实例的网关缓存可能仍持有该封禁直到 BlockUntil，通知它们清空后回源。
	publishRuntimeRevision(runtimeSyncRiskBlocksKey)
	snapshot, err := model.GetRiskSubjectSnapshot(scope, subjectID, group)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	now := common.GetTimestamp()
	if snapshot != nil {
		status := RiskStatusNormal
		if snapshot.RiskScore > 0 {
			status = RiskStatusObserve
		}
		snapshot.Status = status
		snapshot.BlockUntil = 0
		snapshot.RecoverAt = now
		snapshot.LastAction = RiskActionManual
		snapshot.LastDecision = RiskDecisionAllow
		snapshot.LastReason = "管理员手动解除"
		snapshot.LastIncidentAt = now
		snapshot.LastEvaluatedAt = now
		if err = model.UpsertRiskSubjectSnapshot(snapshot); err != nil {
			return err
		}
		incident := &model.RiskIncident{
			CreatedAt:          now,
			SubjectType:        scope,
			SubjectID:          subjectID,
			UserID:             snapshot.UserID,
			TokenID:            snapshot.TokenID,
			Username:           snapshot.Username,
			TokenName:          snapshot.TokenName,
			TokenMaskedKey:     snapshot.TokenMaskedKey,
			Group:              group,
			Action:             RiskActionManual,
			Decision:           RiskDecisionAllow,
			Status:             status,
			ResponseStatusCode: 0,
			ResponseMessage:    "管理员手动解除",
			AutoRecover:        false,
			RecoverAt:          now,
			ResolvedAt:         now,
			RiskScore:          snapshot.RiskScore,
			Reason:             fmt.Sprintf("operator=%s", operator),
		}
		return model.CreateRiskIncident(incident)
	}
	return nil
}

func (r *riskControlCenter) start() {
	if r.started.Load() {
		return
	}
	r.store = newRiskMetricStore()
	cfg := operation_setting.GetRiskControlSetting()
	r.queue = make(chan *RiskEvent, cfg.EventQueueSize)
	if err := r.reloadRules(); err != nil {
		common.SysError("risk control reload rules failed: " + err.Error())
	}
	if common.IsMasterNode {
		if err := seedDefaultRiskRules(); err != nil {
			common.SysError("risk control seed rules failed: " + err.Error())
		}
		if err := r.reloadRules(); err != nil {
			common.SysError("risk control reload rules after seed failed: " + err.Error())
		}
	}
	workerCount := cfg.WorkerCount
	if workerCount <= 0 {
		workerCount = 2
	}
	for i := 0; i < workerCount; i++ {
		go r.runWorker()
	}
	if common.IsMasterNode {
		go r.runRecoveryLoop()
	}
	r.started.Store(true)
}

// reloadRules compiles model rows into compiledRiskRule. Rules without any
// configured groups are dropped (and logged) per DEV_GUIDE §5 — "未配置分组 = 不启用".
func (r *riskControlCenter) reloadRules() error {
	rules, err := model.ListEnabledRiskRules()
	if err != nil {
		return err
	}
	compiled := make([]*compiledRiskRule, 0, len(rules))
	for _, rule := range rules {
		conditions := make([]types.RiskCondition, 0)
		if strings.TrimSpace(rule.Conditions) != "" {
			if err = common.UnmarshalJsonStr(rule.Conditions, &conditions); err != nil {
				return fmt.Errorf("rule %s conditions invalid: %w", rule.Name, err)
			}
		}
		groups := rule.ParsedGroups()
		if len(groups) == 0 {
			common.SysLog(fmt.Sprintf("risk rule %q skipped: no groups configured", rule.Name))
			continue
		}
		groupSet := make(map[string]struct{}, len(groups))
		for _, g := range groups {
			groupSet[g] = struct{}{}
		}
		compiled = append(compiled, &compiledRiskRule{
			Raw:        rule,
			Conditions: conditions,
			Groups:     groupSet,
		})
	}
	r.rules.Store(compiled)
	return nil
}

func (r *riskControlCenter) currentRules() []*compiledRiskRule {
	if raw := r.rules.Load(); raw != nil {
		if rules, ok := raw.([]*compiledRiskRule); ok {
			return rules
		}
	}
	return nil
}

func (r *riskControlCenter) beforeRelay(c *gin.Context, info *relaycommon.RelayInfo, cfg *operation_setting.RiskControlSetting) *types.RiskDecision {
	mode := operation_setting.EffectiveRiskModeForGroup(cfg, info.RiskGroup)
	if mode != operation_setting.RiskControlModeEnforce {
		r.enqueueStart(c, info)
		return nil
	}
	if decision := r.getCachedDecision(RiskSubjectTypeUser, info.UserId, info.RiskGroup); decision != nil {
		return decision
	}
	if decision := r.getCachedDecision(RiskSubjectTypeToken, info.TokenId, info.RiskGroup); decision != nil {
		return decision
	}
	r.enqueueStart(c, info)
	return nil
}

func (r *riskControlCenter) afterRelay(c *gin.Context, info *relaycommon.RelayInfo, relayErr *types.NewAPIError) {
	now := common.GetTimestamp()
	statusCode := 0
	if relayErr != nil {
		statusCode = relayErr.StatusCode
	} else if c != nil && c.Writer != nil {
		statusCode = c.Writer.Status()
	}
	event := r.buildEvent(c, info, RiskEventTypeFinish, now, statusCode)
	r.enqueueEvent(event)
}

func (r *riskControlCenter) buildEvent(c *gin.Context, info *relaycommon.RelayInfo, eventType string, now int64, statusCode int) *RiskEvent {
	if info == nil {
		return nil
	}
	clientIP := ""
	userAgent := ""
	requestPath := info.RequestURLPath
	username := ""
	tokenName := ""
	if c != nil && c.Request != nil {
		clientIP = requestip.GetClientIP(c)
		userAgent = c.GetHeader("User-Agent")
		username = c.GetString("username")
		tokenName = c.GetString("token_name")
		if c.Request.URL != nil && c.Request.URL.Path != "" {
			requestPath = c.Request.URL.Path
		}
	}
	return &RiskEvent{
		Type:           eventType,
		OccurAt:        now,
		RequestID:      info.RequestId,
		RequestPath:    requestPath,
		UserID:         info.UserId,
		Username:       username,
		TokenID:        info.TokenId,
		TokenName:      tokenName,
		TokenMaskedKey: model.MaskTokenKey(info.TokenKey),
		Group:          info.RiskGroup,
		ClientIPHash:   common.GenerateHMAC(clientIP),
		UserAgentHash:  common.GenerateHMAC(normalizeUserAgent(userAgent)),
		StatusCode:     statusCode,
	}
}

func (r *riskControlCenter) enqueueStart(c *gin.Context, info *relaycommon.RelayInfo) {
	event := r.buildEvent(c, info, RiskEventTypeStart, common.GetTimestamp(), 0)
	r.enqueueEvent(event)
}

func (r *riskControlCenter) enqueueEvent(event *RiskEvent) {
	if event == nil || r.queue == nil {
		return
	}
	if event.Group == "" {
		// Defensive: never persist data against an empty group bucket.
		return
	}
	if !isRiskControlActiveForGroup(operation_setting.GetRiskControlSetting(), event.Group) {
		return
	}
	gopool.Add()
	event.drainDone = gopool.Done
	select {
	case r.queue <- event:
	default:
		event.drainDone()
		r.dropCount.Add(1)
		if r.dropCount.Load()%100 == 1 {
			common.SysLog("risk control event queue is full, dropping events")
		}
	}
}

func (r *riskControlCenter) runWorker() {
	for event := range r.queue {
		if event == nil {
			continue
		}
		switch event.Type {
		case RiskEventTypeStart:
			if err := r.handleStartEvent(event); err != nil {
				common.SysError("risk control handle start event failed: " + err.Error())
			}
		case RiskEventTypeFinish:
			if err := r.handleFinishEvent(event); err != nil {
				common.SysError("risk control handle finish event failed: " + err.Error())
			}
		}
		if event.drainDone != nil {
			event.drainDone()
		}
	}
}

func (r *riskControlCenter) handleStartEvent(event *RiskEvent) error {
	if event.Group == "" {
		return nil
	}
	now := time.Unix(event.OccurAt, 0)
	tokenMetrics, err := r.store.RecordStart(RiskSubjectTypeToken, event.TokenID, event.Group, event.ClientIPHash, event.UserAgentHash, now)
	if err != nil {
		return err
	}
	userMetrics, err := r.store.RecordStart(RiskSubjectTypeUser, event.UserID, event.Group, event.ClientIPHash, "", now)
	if err != nil {
		return err
	}
	tokenMetrics.RuleHitCount24H, _ = r.store.GetRuleHitCount(RiskSubjectTypeToken, event.TokenID, event.Group, now)
	userMetrics.RuleHitCount24H, _ = r.store.GetRuleHitCount(RiskSubjectTypeUser, event.UserID, event.Group, now)

	tokenDecision, tokenMatched, err := r.evaluateAndPersistSubject(event, RiskSubjectTypeToken, event.TokenID, event.Group, tokenMetrics)
	if err != nil {
		return err
	}
	userDecision, userMatched, err := r.evaluateAndPersistSubject(event, RiskSubjectTypeUser, event.UserID, event.Group, userMetrics)
	if err != nil {
		return err
	}

	if tokenDecision != nil && tokenDecision.Decision == RiskDecisionBlock && len(tokenMatched) > 0 {
		r.updateGateCache(tokenDecision)
	}
	if userDecision != nil && userDecision.Decision == RiskDecisionBlock && len(userMatched) > 0 {
		r.updateGateCache(userDecision)
	}
	return nil
}

func (r *riskControlCenter) handleFinishEvent(event *RiskEvent) error {
	if event.Group == "" {
		return nil
	}
	now := time.Unix(event.OccurAt, 0)
	if _, err := r.store.RecordFinish(RiskSubjectTypeToken, event.TokenID, event.Group, now); err != nil {
		return err
	}
	if _, err := r.store.RecordFinish(RiskSubjectTypeUser, event.UserID, event.Group, now); err != nil {
		return err
	}
	return nil
}

func (r *riskControlCenter) evaluateAndPersistSubject(event *RiskEvent, scope string, subjectID int, group string, metrics types.RiskMetrics) (*types.RiskDecision, []*compiledRiskRule, error) {
	if subjectID <= 0 || group == "" {
		return nil, nil, nil
	}
	rules := r.currentRules()
	matched := evaluateRiskRules(rules, scope, group, metrics)
	if len(matched) > 0 {
		hitCount, hitErr := r.store.IncrementRuleHit(scope, subjectID, group, time.Unix(event.OccurAt, 0))
		if hitErr == nil {
			metrics.RuleHitCount24H = hitCount
		}
	}
	metrics.RiskScore = computeRiskScore(metrics, matched)
	decision := buildRiskDecision(scope, subjectID, group, matched, metrics)
	previous, err := model.GetRiskSubjectSnapshot(scope, subjectID, group)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, err
	}
	mode := operation_setting.EffectiveRiskModeForGroup(operation_setting.GetRiskControlSetting(), group)
	if decision != nil && decision.Action == RiskActionBlock {
		if decision.AutoRecover {
			decision.BlockUntil = time.Unix(event.OccurAt, 0).Add(time.Duration(decision.RecoverAfterSecond) * time.Second).Unix()
		}
		if mode == operation_setting.RiskControlModeEnforce {
			if err = r.store.SetBlock(scope, subjectID, group, decision); err != nil {
				common.SysError("risk control set block failed: " + err.Error())
			}
		}
	}
	// Mark a vague pending warning on the user when an enforce-mode decision
	// is non-allow. We deliberately fire only on the user scope (token-only
	// hits would multiply notifications for the same human) and only in
	// enforce mode (observe_only hasn't actually restricted anything yet).
	if scope == RiskSubjectTypeUser &&
		decision != nil &&
		decision.Decision != RiskDecisionAllow &&
		mode == operation_setting.RiskControlModeEnforce {
		uid := subjectID
		ts := event.OccurAt
		gopool.Go(func() {
			if markErr := model.MarkUserRiskWarningPending(uid, ts); markErr != nil {
				common.SysError("risk control mark user warning failed: " + markErr.Error())
			}
		})
		// Hand off to the unified enforcement layer for email + counter +
		// auto-ban handling. Only block decisions count toward enforcement;
		// observe hits must not accumulate into an automatic ban.
		// EnforcementHit is internally async and does nothing when the
		// layer is disabled, so this is safe to call here.
		if decision.Decision == RiskDecisionBlock {
			EnforcementHit(uid, group, operation_setting.EnforcementSourceRiskDistribution,
				"distribution_"+decision.Action)
		}
	}
	snapshot := buildRiskSubjectSnapshot(event, scope, subjectID, group, metrics, decision, matched, previous)
	if err = model.UpsertRiskSubjectSnapshot(snapshot); err != nil {
		return nil, nil, err
	}
	if shouldCreateRiskIncident(previous, snapshot, decision) {
		incident := buildRiskIncident(event, snapshot, decision, matched)
		if incident != nil {
			if err = model.CreateRiskIncident(incident); err != nil {
				return nil, nil, err
			}
		}
	}
	return decision, matched, nil
}

func evaluateRiskRules(rules []*compiledRiskRule, scope, group string, metrics types.RiskMetrics) []*compiledRiskRule {
	if len(rules) == 0 || group == "" {
		return nil
	}
	matched := make([]*compiledRiskRule, 0)
	for _, rule := range rules {
		if rule == nil || rule.Raw == nil || !rule.Raw.Enabled || rule.Raw.Scope != scope {
			continue
		}
		if !ruleAppliesToGroup(rule, group) {
			continue
		}
		if matchesRiskRule(rule, metrics) {
			matched = append(matched, rule)
		}
	}
	return matched
}

func matchesRiskRule(rule *compiledRiskRule, metrics types.RiskMetrics) bool {
	if rule == nil || rule.Raw == nil {
		return false
	}
	if len(rule.Conditions) == 0 {
		return false
	}
	matchMode := strings.ToLower(strings.TrimSpace(rule.Raw.MatchMode))
	if matchMode == "" {
		matchMode = "all"
	}
	matchedCount := 0
	for _, condition := range rule.Conditions {
		actual := getMetricValue(metrics, condition.Metric)
		result := compareRiskMetric(actual, condition.Op, condition.Value)
		if result {
			matchedCount++
		}
		if matchMode == "any" && result {
			return true
		}
		if matchMode == "all" && !result {
			return false
		}
	}
	if matchMode == "any" {
		return matchedCount > 0
	}
	return matchedCount == len(rule.Conditions)
}

func getMetricValue(metrics types.RiskMetrics, metric string) float64 {
	switch metric {
	case "distinct_ip_10m":
		return float64(metrics.DistinctIP10M)
	case "distinct_ip_1h":
		return float64(metrics.DistinctIP1H)
	case "distinct_ua_10m":
		return float64(metrics.DistinctUA10M)
	case "tokens_per_ip_10m":
		return float64(metrics.TokensPerIP10M)
	case "request_count_1m", "req_1m":
		return float64(metrics.RequestCount1M)
	case "request_count_10m", "req_10m":
		return float64(metrics.RequestCount10M)
	case "inflight_now":
		return float64(metrics.InflightNow)
	case "rule_hit_count_24h":
		return float64(metrics.RuleHitCount24H)
	case "risk_score":
		return float64(metrics.RiskScore)
	default:
		return 0
	}
}

func compareRiskMetric(actual float64, op string, expected float64) bool {
	switch op {
	case ">", "gt":
		return actual > expected
	case ">=", "gte":
		return actual >= expected
	case "<", "lt":
		return actual < expected
	case "<=", "lte":
		return actual <= expected
	case "==", "=":
		return actual == expected
	case "!=", "<>":
		return actual != expected
	default:
		return false
	}
}

func computeRiskScore(metrics types.RiskMetrics, matched []*compiledRiskRule) int {
	score := 0
	score += minInt(metrics.DistinctIP10M*8, 40)
	score += minInt(metrics.DistinctIP1H*2, 10)
	score += minInt(metrics.InflightNow*4, 20)
	score += minInt(metrics.RequestCount1M/5, 15)
	score += minInt(metrics.DistinctUA10M*3, 10)
	score += minInt(metrics.TokensPerIP10M*6, 30)
	score += minInt(metrics.RuleHitCount24H*4, 15)
	for _, rule := range matched {
		if rule != nil && rule.Raw != nil && rule.Raw.ScoreWeight > 0 {
			score += rule.Raw.ScoreWeight
		}
	}
	if score > 100 {
		return 100
	}
	if score < 0 {
		return 0
	}
	return score
}

func buildRiskDecision(scope string, subjectID int, group string, matched []*compiledRiskRule, metrics types.RiskMetrics) *types.RiskDecision {
	if subjectID <= 0 {
		return nil
	}
	cfg := operation_setting.GetRiskControlSetting()
	mode := operation_setting.EffectiveRiskModeForGroup(cfg, group)
	if len(matched) == 0 {
		return &types.RiskDecision{
			Scope:           scope,
			SubjectID:       subjectID,
			Group:           group,
			Decision:        RiskDecisionAllow,
			Action:          RiskDecisionAllow,
			Status:          RiskStatusNormal,
			StatusCode:      http.StatusOK,
			ResponseMessage: "",
			RiskScore:       metrics.RiskScore,
			Metrics:         metrics,
		}
	}
	primary := matched[0]
	responseStatusCode := cfg.DefaultStatusCode
	responseMessage := cfg.DefaultResponseMessage
	autoRecover := false
	recoverMode := cfg.DefaultRecoverMode
	recoverAfter := cfg.DefaultRecoverAfterSecs
	action := RiskActionObserve
	decisionType := RiskDecisionObserve
	status := RiskStatusObserve
	matchedNames := make([]string, 0, len(matched))
	for _, rule := range matched {
		if rule == nil || rule.Raw == nil {
			continue
		}
		matchedNames = append(matchedNames, rule.Raw.Name)
		if action != RiskActionBlock && strings.EqualFold(rule.Raw.Action, RiskActionBlock) {
			action = RiskActionBlock
			decisionType = RiskDecisionBlock
			status = RiskStatusBlocked
			primary = rule
		}
	}
	if primary != nil && primary.Raw != nil {
		if primary.Raw.ResponseStatusCode > 0 {
			responseStatusCode = primary.Raw.ResponseStatusCode
		}
		if strings.TrimSpace(primary.Raw.ResponseMessage) != "" {
			responseMessage = primary.Raw.ResponseMessage
		}
		autoRecover = primary.Raw.AutoRecover
		if strings.TrimSpace(primary.Raw.RecoverMode) != "" {
			recoverMode = primary.Raw.RecoverMode
		}
		if primary.Raw.RecoverAfterSeconds > 0 {
			recoverAfter = primary.Raw.RecoverAfterSeconds
		}
		if action == RiskActionBlock && !primary.Raw.AutoBlock {
			action = RiskActionObserve
			decisionType = RiskDecisionObserve
			status = RiskStatusObserve
		}
	}
	if action == RiskActionBlock && mode != operation_setting.RiskControlModeEnforce {
		action = RiskActionObserve
		decisionType = RiskDecisionObserve
		status = RiskStatusObserve
	}
	return &types.RiskDecision{
		Scope:              scope,
		SubjectID:          subjectID,
		Group:              group,
		Decision:           decisionType,
		Action:             action,
		Status:             status,
		RuleID:             primary.Raw.Id,
		RuleName:           primary.Raw.Name,
		Detector:           primary.Raw.Detector,
		Reason:             fmt.Sprintf("matched_rules=%s", strings.Join(matchedNames, ",")),
		MatchedRules:       matchedNames,
		StatusCode:         responseStatusCode,
		ResponseMessage:    responseMessage,
		AutoRecover:        autoRecover,
		RecoverMode:        recoverMode,
		RecoverAfterSecond: recoverAfter,
		RiskScore:          metrics.RiskScore,
		Metrics:            metrics,
	}
}

func buildRiskSubjectSnapshot(event *RiskEvent, scope string, subjectID int, group string, metrics types.RiskMetrics, decision *types.RiskDecision, matched []*compiledRiskRule, previous *model.RiskSubjectSnapshot) *model.RiskSubjectSnapshot {
	status := RiskStatusNormal
	lastDecision := RiskDecisionAllow
	lastAction := RiskDecisionAllow
	lastRule := ""
	lastReason := ""
	blockUntil := int64(0)
	recoverAt := int64(0)
	autoRecover := false
	statusCode := 0
	if decision != nil {
		status = decision.Status
		lastDecision = decision.Decision
		lastAction = decision.Action
		lastRule = decision.RuleName
		lastReason = decision.Reason
		blockUntil = decision.BlockUntil
		if decision.AutoRecover && decision.BlockUntil > 0 {
			recoverAt = decision.BlockUntil
		}
		autoRecover = decision.AutoRecover
		statusCode = decision.StatusCode
	}
	if status == RiskStatusNormal && metrics.RiskScore >= 30 {
		status = RiskStatusObserve
	}
	activeRuleNames := make([]string, 0, len(matched))
	for _, rule := range matched {
		if rule != nil && rule.Raw != nil {
			activeRuleNames = append(activeRuleNames, rule.Raw.Name)
		}
	}
	snapshot := &model.RiskSubjectSnapshot{
		SubjectType:       scope,
		SubjectID:         subjectID,
		UserID:            event.UserID,
		TokenID:           event.TokenID,
		Username:          event.Username,
		TokenName:         event.TokenName,
		TokenMaskedKey:    event.TokenMaskedKey,
		Group:             group,
		Status:            status,
		RiskScore:         metrics.RiskScore,
		DistinctIP10M:     metrics.DistinctIP10M,
		DistinctIP1H:      metrics.DistinctIP1H,
		DistinctUA10M:     metrics.DistinctUA10M,
		RequestCount1M:    metrics.RequestCount1M,
		RequestCount10M:   metrics.RequestCount10M,
		InflightNow:       metrics.InflightNow,
		RuleHitCount24H:   metrics.RuleHitCount24H,
		ActiveRuleNames:   encodeRiskJSON(activeRuleNames),
		LastRuleName:      lastRule,
		LastDecision:      lastDecision,
		LastAction:        lastAction,
		LastReason:        lastReason,
		LastRequestPath:   event.RequestPath,
		LastStatusCode:    statusCode,
		BlockUntil:        blockUntil,
		RecoverAt:         recoverAt,
		AutoRecover:       autoRecover,
		LastSeenAt:        event.OccurAt,
		LastEvaluatedAt:   event.OccurAt,
		LastIncidentAt:    event.OccurAt,
		SnapshotExtraData: encodeRiskJSON(map[string]any{"request_id": event.RequestID}),
	}
	if previous != nil && previous.Id > 0 {
		snapshot.Id = previous.Id
		if snapshot.Username == "" {
			snapshot.Username = previous.Username
		}
		if snapshot.TokenName == "" {
			snapshot.TokenName = previous.TokenName
		}
		if snapshot.TokenMaskedKey == "" {
			snapshot.TokenMaskedKey = previous.TokenMaskedKey
		}
	}
	return snapshot
}

func shouldCreateRiskIncident(previous *model.RiskSubjectSnapshot, current *model.RiskSubjectSnapshot, decision *types.RiskDecision) bool {
	if current == nil {
		return false
	}
	if previous == nil {
		return current.Status != RiskStatusNormal
	}
	if current.Status == RiskStatusBlocked && previous.Status != RiskStatusBlocked {
		return true
	}
	if current.Status == RiskStatusObserve && previous.Status == RiskStatusNormal {
		return true
	}
	if decision != nil && decision.Action == RiskActionBlock && current.BlockUntil != previous.BlockUntil {
		return true
	}
	return false
}

func buildRiskIncident(event *RiskEvent, snapshot *model.RiskSubjectSnapshot, decision *types.RiskDecision, matched []*compiledRiskRule) *model.RiskIncident {
	if snapshot == nil || decision == nil || decision.Decision == RiskDecisionAllow {
		return nil
	}
	return &model.RiskIncident{
		CreatedAt:          event.OccurAt,
		SubjectType:        snapshot.SubjectType,
		SubjectID:          snapshot.SubjectID,
		UserID:             snapshot.UserID,
		TokenID:            snapshot.TokenID,
		Username:           snapshot.Username,
		TokenName:          snapshot.TokenName,
		TokenMaskedKey:     snapshot.TokenMaskedKey,
		Group:              snapshot.Group,
		RuleID:             decision.RuleID,
		RuleName:           decision.RuleName,
		Detector:           decision.Detector,
		Action:             decision.Action,
		Decision:           decision.Decision,
		Status:             snapshot.Status,
		ResponseStatusCode: decision.StatusCode,
		ResponseMessage:    decision.ResponseMessage,
		AutoRecover:        decision.AutoRecover,
		RecoverAt:          snapshot.RecoverAt,
		RequestID:          event.RequestID,
		RequestPath:        event.RequestPath,
		RiskScore:          snapshot.RiskScore,
		Reason:             decision.Reason,
		Snapshot: encodeRiskJSON(map[string]any{
			"metrics":       decision.Metrics,
			"matched_rules": decision.MatchedRules,
		}),
	}
}

// clearGateCache 丢弃本实例的网关决策缓存，下一次请求回源共享存储。
func (r *riskControlCenter) clearGateCache() {
	r.cache.Range(func(key, _ any) bool {
		r.cache.Delete(key)
		return true
	})
}

func (r *riskControlCenter) getCachedDecision(scope string, subjectID int, group string) *types.RiskDecision {
	if subjectID <= 0 || group == "" {
		return nil
	}
	key := riskMemoryKey(scope, subjectID, group)
	if cached, ok := r.cache.Load(key); ok {
		entry, ok := cached.(*localRiskGateCacheEntry)
		if ok && entry != nil && entry.ExpiresAt > time.Now().Unix() {
			if entry.Decision == nil {
				return nil
			}
			copied := *entry.Decision
			return &copied
		}
		r.cache.Delete(key)
	}
	decision, err := r.store.GetBlock(scope, subjectID, group)
	if err != nil {
		return nil
	}
	cacheSeconds := int64(operation_setting.GetRiskControlSetting().LocalCacheSeconds)
	if decision == nil {
		r.cache.Store(key, &localRiskGateCacheEntry{
			Decision:  nil,
			ExpiresAt: time.Now().Unix() + cacheSeconds,
		})
		return nil
	}
	expiresAt := decision.BlockUntil
	if expiresAt <= 0 {
		expiresAt = time.Now().Unix() + cacheSeconds
	}
	r.cache.Store(key, &localRiskGateCacheEntry{
		Decision:  decision,
		ExpiresAt: expiresAt,
	})
	return decision
}

func (r *riskControlCenter) updateGateCache(decision *types.RiskDecision) {
	if decision == nil || decision.Group == "" {
		return
	}
	r.cache.Store(riskMemoryKey(decision.Scope, decision.SubjectID, decision.Group), &localRiskGateCacheEntry{
		Decision:  decision,
		ExpiresAt: decision.BlockUntil,
	})
}

func normalizeUserAgent(ua string) string {
	ua = strings.TrimSpace(strings.ToLower(ua))
	if len(ua) > 256 {
		return ua[:256]
	}
	return ua
}

// validateRiskRule enforces the v4 invariants for stored rules:
//   - scope must be token or user
//   - conditions must be valid JSON with at least one supported metric
//   - when Enabled=true, ParsedGroups() must be non-empty (a rule cannot be
//     turned on without at least one group binding); the engine skips
//     unconfigured rules, so allowing enabled+empty would be a silent footgun
//
// validateRiskRule deliberately does NOT check whether the chosen groups are
// inside RiskControlSetting.EnabledGroups. Admins should be able to draft
// rules ahead of flipping the whitelist (see DEV_GUIDE §12.5).
func validateRiskRule(rule *model.RiskRule) error {
	if rule == nil {
		return errors.New("rule is nil")
	}
	cfg := operation_setting.GetRiskControlSetting()
	rule.Name = strings.TrimSpace(rule.Name)
	if rule.Name == "" {
		return errors.New("rule name is required")
	}
	if rule.Scope != RiskSubjectTypeToken && rule.Scope != RiskSubjectTypeUser {
		return errors.New("invalid rule scope")
	}
	if strings.TrimSpace(rule.Detector) == "" {
		rule.Detector = "distribution"
	}
	if strings.TrimSpace(rule.MatchMode) == "" {
		rule.MatchMode = "all"
	}
	if strings.TrimSpace(rule.Action) == "" {
		rule.Action = RiskActionObserve
	}
	if rule.ResponseStatusCode <= 0 {
		rule.ResponseStatusCode = cfg.DefaultStatusCode
	}
	if rule.RecoverAfterSeconds <= 0 {
		rule.RecoverAfterSeconds = cfg.DefaultRecoverAfterSecs
	}
	if strings.TrimSpace(rule.RecoverMode) == "" {
		rule.RecoverMode = cfg.DefaultRecoverMode
	}
	var conditions []types.RiskCondition
	if err := common.UnmarshalJsonStr(rule.Conditions, &conditions); err != nil {
		return errors.New("rule conditions must be valid JSON")
	}
	if len(conditions) == 0 {
		return errors.New("at least one condition is required")
	}
	for i, condition := range conditions {
		condition.Metric = strings.TrimSpace(condition.Metric)
		if condition.Metric == "" {
			return fmt.Errorf("condition %d metric is required", i+1)
		}
		if _, ok := getRiskMetricDefinition(condition.Metric); !ok {
			return fmt.Errorf("unsupported risk metric: %s", condition.Metric)
		}
		if !isRiskMetricAllowedForScope(condition.Metric, rule.Scope) {
			return fmt.Errorf(
				"metric %s is only supported for %s scope",
				condition.Metric,
				strings.Join(riskMetricAllowedScopes(condition.Metric), ", "),
			)
		}
		conditions[i] = condition
	}
	conditionsBytes, err := common.Marshal(conditions)
	if err != nil {
		return err
	}
	rule.Conditions = string(conditionsBytes)

	// Normalize Groups (trim/dedupe/drop empties) and persist back so reload
	// sees the canonical form. Do NOT validate against the whitelist — that
	// is intentional, see the function comment above.
	parsedGroups := rule.ParsedGroups()
	if rule.Enabled && len(parsedGroups) == 0 {
		return errors.New("启用规则前必须至少选择一个分组")
	}
	groupsBytes, err := common.Marshal(parsedGroups)
	if err != nil {
		return err
	}
	rule.Groups = string(groupsBytes)
	return nil
}

// seedDefaultRiskRules inserts a starter set of disabled rules on a fresh
// install. Each default rule has Enabled=false and Groups="" — operators must
// configure the whitelist (RiskControlSetting.EnabledGroups) and pick at
// least one group on each rule before turning it on. This is the v4 default
// safe upgrade behavior: zero-impact at install time.
func seedDefaultRiskRules() error {
	count, err := model.CountRiskRules()
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	cfg := operation_setting.GetRiskControlSetting()
	defaultRules := []*model.RiskRule{
		newDefaultRiskRule(
			cfg,
			"token_multi_ip_observe",
			"单个 API key 在短时间内出现多个不同 IP，先进入观察状态（启用前请绑定分组）",
			RiskSubjectTypeToken,
			RiskActionObserve,
			false,
			30,
			0,
			[]types.RiskCondition{{Metric: "distinct_ip_10m", Op: ">=", Value: 3}},
		),
		newDefaultRiskRule(
			cfg,
			"token_multi_ip_high_concurrency_block",
			"单个 API key 在短时间内出现多个不同 IP 且并发明显偏高，自动封禁 15 分钟（启用前请绑定分组）",
			RiskSubjectTypeToken,
			RiskActionBlock,
			true,
			50,
			15*60,
			[]types.RiskCondition{
				{Metric: "distinct_ip_10m", Op: ">=", Value: 5},
				{Metric: "inflight_now", Op: ">=", Value: 8},
			},
		),
		newDefaultRiskRule(
			cfg,
			"token_multi_ip_burst_block",
			"单个 API key 在 10 分钟内请求量异常且伴随多 IP 轮询，自动封禁 1 小时（启用前请绑定分组）",
			RiskSubjectTypeToken,
			RiskActionBlock,
			true,
			60,
			60*60,
			[]types.RiskCondition{
				{Metric: "distinct_ip_10m", Op: ">=", Value: 3},
				{Metric: "request_count_10m", Op: ">=", Value: 120},
			},
		),
		newDefaultRiskRule(
			cfg,
			"shared_ip_multi_token_observe",
			"同一 IP 在 10 分钟内关联多个 API key，先进入观察状态（启用前请绑定分组）",
			RiskSubjectTypeToken,
			RiskActionObserve,
			false,
			25,
			0,
			[]types.RiskCondition{{Metric: "tokens_per_ip_10m", Op: ">=", Value: 3}},
		),
		newDefaultRiskRule(
			cfg,
			"shared_ip_multi_token_high_volume_block",
			"同一 IP 在 10 分钟内关联多个 API key 且请求量明显偏高，自动封禁 15 分钟（启用前请绑定分组）",
			RiskSubjectTypeToken,
			RiskActionBlock,
			true,
			50,
			15*60,
			[]types.RiskCondition{
				{Metric: "tokens_per_ip_10m", Op: ">=", Value: 5},
				{Metric: "request_count_10m", Op: ">=", Value: 50},
			},
		),
		newDefaultRiskRule(
			cfg,
			"user_multi_ip_observe",
			"同一用户在 1 小时内出现异常多的不同 IP，进入用户级观察（启用前请绑定分组）",
			RiskSubjectTypeUser,
			RiskActionObserve,
			false,
			25,
			0,
			[]types.RiskCondition{{Metric: "distinct_ip_1h", Op: ">=", Value: 8}},
		),
	}
	for _, rule := range defaultRules {
		if err = model.CreateRiskRule(rule); err != nil {
			return err
		}
	}
	return nil
}

func newDefaultRiskRule(cfg *operation_setting.RiskControlSetting, name string, description string, scope string, action string, autoBlock bool, score int, recoverAfterSeconds int, conditions []types.RiskCondition) *model.RiskRule {
	if recoverAfterSeconds <= 0 {
		recoverAfterSeconds = cfg.DefaultRecoverAfterSecs
	}
	return &model.RiskRule{
		Name:                name,
		Description:         description,
		Enabled:             false,
		Scope:               scope,
		Detector:            "distribution",
		MatchMode:           "all",
		Priority:            score,
		Action:              action,
		AutoBlock:           autoBlock,
		AutoRecover:         true,
		RecoverMode:         cfg.DefaultRecoverMode,
		RecoverAfterSeconds: recoverAfterSeconds,
		ResponseStatusCode:  cfg.DefaultStatusCode,
		ResponseMessage:     cfg.DefaultResponseMessage,
		ScoreWeight:         score,
		Groups:              "",
		Conditions:          encodeRiskJSON(conditions),
		CreatedBy:           0,
		UpdatedBy:           0,
	}
}

func (r *riskControlCenter) runRecoveryLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	lastCleanupAt := int64(0)
	for range ticker.C {
		now := common.GetTimestamp()
		snapshots, err := model.MarkExpiredRiskSubjectSnapshotsAsRecovered(now)
		if err != nil {
			common.SysError("risk control recover expired subjects failed: " + err.Error())
			continue
		}
		for _, snapshot := range snapshots {
			if snapshot == nil {
				continue
			}
			_ = r.store.ClearBlock(snapshot.SubjectType, snapshot.SubjectID, snapshot.Group)
			r.cache.Delete(riskMemoryKey(snapshot.SubjectType, snapshot.SubjectID, snapshot.Group))
			incident := &model.RiskIncident{
				CreatedAt:      now,
				SubjectType:    snapshot.SubjectType,
				SubjectID:      snapshot.SubjectID,
				UserID:         snapshot.UserID,
				TokenID:        snapshot.TokenID,
				Username:       snapshot.Username,
				TokenName:      snapshot.TokenName,
				TokenMaskedKey: snapshot.TokenMaskedKey,
				Group:          snapshot.Group,
				RuleName:       snapshot.LastRuleName,
				Action:         RiskActionRecover,
				Decision:       RiskDecisionAllow,
				Status:         snapshot.Status,
				AutoRecover:    true,
				RecoverAt:      now,
				ResolvedAt:     now,
				RiskScore:      snapshot.RiskScore,
				Reason:         "自动恢复",
			}
			if err = model.CreateRiskIncident(incident); err != nil {
				common.SysError("risk control create auto recover incident failed: " + err.Error())
			}
		}
		if lastCleanupAt == 0 || now-lastCleanupAt >= 3600 {
			if err = cleanupExpiredRiskData(now); err != nil {
				common.SysError("risk control cleanup expired data failed: " + err.Error())
			} else {
				lastCleanupAt = now
			}
		}
	}
}

func cleanupExpiredRiskData(now int64) error {
	retentionHours := operation_setting.GetRiskControlSetting().SnapshotRetentionHours
	if retentionHours <= 0 {
		return nil
	}
	cutoff := now - int64(retentionHours)*3600
	if err := model.DeleteExpiredRiskSubjectSnapshots(cutoff); err != nil {
		return err
	}
	return model.DeleteExpiredRiskIncidents(cutoff)
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func AppendRiskAuditToOther(other map[string]any, audit *types.RiskAudit) {
	if other == nil || audit == nil {
		return
	}
	other["risk_control"] = audit
}

func GetBlockingDecisionFromAudit(audit *types.RiskAudit) *types.RiskDecision {
	if audit == nil {
		return nil
	}
	if audit.UserDecision != nil && audit.UserDecision.Decision == RiskDecisionBlock {
		return audit.UserDecision
	}
	if audit.TokenDecision != nil && audit.TokenDecision.Decision == RiskDecisionBlock {
		return audit.TokenDecision
	}
	return nil
}

func RecordRiskBlockedAccess(ctx *gin.Context, info *relaycommon.RelayInfo, decision *types.RiskDecision) {
	if decision == nil || decision.Group == "" {
		return
	}
	gopool.Go(func() {
		event := globalRiskCenter.buildEvent(ctx, info, RiskEventTypeStart, common.GetTimestamp(), decision.StatusCode)
		if event == nil {
			return
		}
		snapshot, err := model.GetRiskSubjectSnapshot(decision.Scope, decision.SubjectID, decision.Group)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			common.SysError("risk control blocked access load snapshot failed: " + err.Error())
			return
		}
		if snapshot != nil {
			snapshot.LastSeenAt = event.OccurAt
			snapshot.LastRequestPath = event.RequestPath
			snapshot.LastStatusCode = decision.StatusCode
			snapshot.LastDecision = decision.Decision
			snapshot.LastAction = decision.Action
			snapshot.LastReason = decision.Reason
			snapshot.LastEvaluatedAt = event.OccurAt
			_ = model.UpsertRiskSubjectSnapshot(snapshot)
		}
	})
}
