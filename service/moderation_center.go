package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	mathrand "math/rand"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
)

// ModerationResult is the structured result returned to debug callers and
// (in summarised form) persisted to moderation_incidents. As of v3 the
// engine attaches the rule decision so the debug card can show admins which
// rules fired without re-running the upstream model.
type ModerationResult struct {
	RequestID         string                  `json:"request_id"`
	Flagged           bool                    `json:"flagged"`
	MaxScore          float64                 `json:"max_score"`
	MaxCategory       string                  `json:"max_category"`
	Categories        map[string]float64      `json:"categories"`
	AppliedTypes      map[string][]string     `json:"applied_types"`
	UpstreamLatencyMS int                     `json:"upstream_latency_ms"`
	UsedKeySuffix     string                  `json:"used_key_suffix"`
	Error             string                  `json:"error,omitempty"`
	CompletedAt       int64                   `json:"completed_at"`
	Decision          *types.ModerationDecision `json:"decision,omitempty"`
}

type moderationEvent struct {
	OccurAt        int64
	Source         string // "relay" / "debug"
	UserID         int
	TokenID        int
	Username       string
	TokenName      string
	TokenMaskedKey string
	Group          string
	RequestID      string
	TextItems      []string
	Images         []string // already https URL or data URI
	Done           chan *ModerationResult
}

type moderationCenter struct {
	started     atomic.Bool
	mu          sync.Mutex
	queue       chan *moderationEvent
	keyRing     *ModerationKeyRing
	dropCount   atomic.Int64
	httpClient  *http.Client
	debugStore  sync.Map // requestID -> *moderationDebugEntry
	workerState sync.Map // workerID -> *moderationWorkerState
	// redisQueue is a write-ahead log used so events survive process
	// restart in multi-instance deployments. Pure best-effort — Redis
	// outages never block the relay path; the in-memory channel remains
	// the source of truth for worker dispatch on each instance.
	redisQueue *moderationRedisQueue
}

type moderationWorkerState struct {
	State       string // "idle" / "processing"
	SinceUnix   int64
	LastEventAt int64
}

type moderationDebugEntry struct {
	Result    *ModerationResult
	ExpiresAt int64
}

var globalModerationCenter = &moderationCenter{}

// decodeModerationEvent rebuilds a moderationEvent from a JSON payload
// stored in the Redis WAL. The Done channel cannot survive serialisation,
// so recovered events get a fresh nil channel — the only callers that
// rely on Done are debug submissions, which never go through the WAL
// (their Source is "debug" and the relay-side enqueue path skips Redis
// for them; recovered events are always production traffic).
func decodeModerationEvent(payload string) *moderationEvent {
	if payload == "" {
		return nil
	}
	var event moderationEvent
	if err := common.UnmarshalJsonStr(payload, &event); err != nil {
		common.SysError("moderation decode WAL event failed: " + err.Error())
		return nil
	}
	event.Done = nil
	return &event
}

// StartModerationCenter is wired from main.go and reloads when config changes.
func StartModerationCenter() {
	globalModerationCenter.start()
}

// ReloadModerationConfig replaces the in-memory key ring with whatever is
// currently in operation_setting. Called from controller.UpdateModerationConfig.
func ReloadModerationConfig() {
	cfg := operation_setting.GetModerationSetting()
	globalModerationCenter.applyConfig(cfg)
}

func (m *moderationCenter) start() {
	if m.started.Load() {
		return
	}
	cfg := operation_setting.GetModerationSetting()
	m.queue = make(chan *moderationEvent, cfg.EventQueueSize)
	// Beefy keep-alive transport — moderation traffic is many small POSTs
	// against the same OpenAI host, so connection reuse matters more than
	// the default Transport allows.
	m.httpClient = &http.Client{
		Timeout: time.Duration(cfg.HTTPTimeoutMS) * time.Millisecond,
		Transport: &http.Transport{
			MaxIdleConns:        cfg.WorkerCount * 4,
			MaxIdleConnsPerHost: cfg.WorkerCount * 4,
			IdleConnTimeout:     90 * time.Second,
		},
	}
	m.keyRing = NewModerationKeyRing(cfg.APIKeys)
	if cfg.RedisQueueEnabled {
		m.redisQueue = newModerationRedisQueue(cfg.EventQueueSize)
		if m.redisQueue != nil {
			recovered := m.redisQueue.recoverIntoChannel(m.queue, decodeModerationEvent)
			if recovered > 0 {
				common.SysLog(fmt.Sprintf("moderation recovered %d events from Redis WAL", recovered))
			}
		}
	}
	if err := ReloadModerationRules(); err != nil {
		common.SysError("moderation reload rules failed: " + err.Error())
	}
	if common.IsMasterNode {
		if err := SeedDefaultModerationRules(); err != nil {
			common.SysError("moderation seed default rules failed: " + err.Error())
		}
		// Reload after seed in case this was a fresh install.
		if err := ReloadModerationRules(); err != nil {
			common.SysError("moderation reload rules after seed failed: " + err.Error())
		}
	}
	for i := 0; i < cfg.WorkerCount; i++ {
		workerID := i
		m.workerState.Store(workerID, &moderationWorkerState{State: "idle"})
		go m.runWorker(workerID)
	}
	if common.IsMasterNode {
		go m.runRetentionLoop()
		go m.runDebugSweeper()
	}
	m.started.Store(true)
}

func (m *moderationCenter) applyConfig(cfg *operation_setting.ModerationSetting) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.keyRing != nil {
		m.keyRing.Reset(cfg.APIKeys)
	}
	if m.httpClient != nil {
		m.httpClient.Timeout = time.Duration(cfg.HTTPTimeoutMS) * time.Millisecond
	}
}

// EnqueueModerationFromRelay is the relay-side hook; it copies the data it
// needs out of gin context and gopool.Go's the rest. Designed to be called
// from a defer block; never blocks the relay path.
//
// relayErr lets the engine drop requests that never produced output —
// upstream 4xx/5xx, timeout, channel error, etc. Such requests have no
// content the user actually saw, so moderating them wastes OpenAI tokens
// and pollutes hit counts (see DEV_GUIDE §14 "失败请求过滤"). The rule:
//
//   - relayErr == nil          ⇒ HTTP 2xx success, enqueue.
//   - SendResponseCount > 0    ⇒ stream/SSE delivered at least one chunk
//     to the client before failing — the user did see content; enqueue.
//   - otherwise                ⇒ skip.
//
// CombineText materialisation: when neither sensitive-text scanning nor
// token counting is enabled, controller/relay.go uses the lightweight
// fastTokenCountMetaForPricing path which never populates CombineText or
// Files. That left moderation silently dropping every request — usage
// logs were still written but no moderation incident ever appeared.
// We now lazily call extractLastMessagePayload inside the async gopool
// callback whenever the eagerly-extracted payload is empty, paying the
// ParseContent cost only when moderation is actually configured for the
// group. The relay client has already received its response by then, so
// the latency is invisible to users.
func EnqueueModerationFromRelay(c *gin.Context, info *relaycommon.RelayInfo, _ *types.TokenCountMeta, relayErr *types.NewAPIError) {
	if info == nil {
		return
	}
	if relayErr != nil && info.SendResponseCount == 0 {
		return
	}
	cfg := operation_setting.GetModerationSetting()
	if !operation_setting.IsModerationEnabledForGroup(cfg, info.RiskGroup) {
		return
	}
	if cfg.SamplingRatePercent <= 0 {
		return
	}
	if cfg.SamplingRatePercent < 100 {
		if mathrand.Intn(100) >= cfg.SamplingRatePercent {
			return
		}
	}
	username := ""
	tokenName := ""
	if c != nil && c.Request != nil {
		username = c.GetString("username")
		tokenName = c.GetString("token_name")
	}
	tokenMasked := model.MaskTokenKey(info.TokenKey)
	requestID := info.RequestId
	group := info.RiskGroup
	userID := info.UserId
	tokenID := info.TokenId
	occurAt := common.GetTimestamp()
	request := info.Request
	gopool.Go(func() {
		text, images := extractLastMessagePayload(request)
		if text == "" && len(images) == 0 {
			return
		}
		globalModerationCenter.enqueue(&moderationEvent{
			OccurAt:        occurAt,
			Source:         "relay",
			UserID:         userID,
			TokenID:        tokenID,
			Username:       username,
			TokenName:      tokenName,
			TokenMaskedKey: tokenMasked,
			Group:          group,
			RequestID:      requestID,
			TextItems:      []string{text},
			Images:         images,
		})
	})
}

// extractLastMessagePayload extracts text and image URLs from only the
// last message in the request. This is the correct input for content
// moderation — CombineText aggregates system prompts, all history, and
// tool definitions which pollute the moderation signal.
func extractLastMessagePayload(request dto.Request) (string, []string) {
	if request == nil {
		return "", nil
	}
	switch r := request.(type) {
	case *dto.GeneralOpenAIRequest:
		if len(r.Messages) == 0 {
			return "", nil
		}
		return extractFromOpenAIMessage(r.Messages[len(r.Messages)-1])
	case *dto.ClaudeRequest:
		if len(r.Messages) == 0 {
			return "", nil
		}
		return extractFromClaudeMessage(r.Messages[len(r.Messages)-1])
	default:
		if m := request.GetTokenCountMeta(); m != nil {
			return strings.TrimSpace(m.CombineText), nil
		}
		return "", nil
	}
}

func extractFromOpenAIMessage(msg dto.Message) (string, []string) {
	parts := msg.ParseContent()
	texts := make([]string, 0, len(parts))
	var images []string
	for _, p := range parts {
		switch p.Type {
		case dto.ContentTypeText:
			if t := strings.TrimSpace(p.Text); t != "" {
				texts = append(texts, t)
			}
		case dto.ContentTypeImageURL:
			if img := p.GetImageMedia(); img != nil && img.Url != "" {
				images = append(images, img.Url)
			}
		}
	}
	return strings.Join(texts, "\n"), images
}

func extractFromClaudeMessage(msg dto.ClaudeMessage) (string, []string) {
	if msg.IsStringContent() {
		return strings.TrimSpace(msg.GetStringContent()), nil
	}
	parts, err := msg.ParseContent()
	if err != nil || len(parts) == 0 {
		return "", nil
	}
	texts := make([]string, 0, len(parts))
	var images []string
	for _, p := range parts {
		switch p.Type {
		case "text":
			if t := strings.TrimSpace(p.GetText()); t != "" {
				texts = append(texts, t)
			}
		case "image":
			if src := p.ToFileSource(); src != nil {
				if raw := src.GetRawData(); strings.TrimSpace(raw) != "" {
					images = append(images, raw)
				}
			}
		}
	}
	return strings.Join(texts, "\n"), images
}

// extractModerationPayload normalises a TokenCountMeta into the
// (text, image-uris) pair the upstream OpenAI call expects. Kept for
// the debug-card path which has no Request object.
func extractModerationPayload(meta *types.TokenCountMeta) (string, []string) {
	if meta == nil {
		return "", nil
	}
	text := strings.TrimSpace(meta.CombineText)
	images := make([]string, 0, len(meta.Files))
	for _, f := range meta.Files {
		if f == nil || f.FileType != types.FileTypeImage {
			continue
		}
		raw := f.GetRawData()
		if strings.TrimSpace(raw) == "" {
			continue
		}
		images = append(images, raw)
	}
	return text, images
}

// SubmitModerationDebug is invoked from the admin debug card. It enqueues a
// debug event with a Done channel and returns the request_id immediately so
// the frontend can poll GetModerationDebugResult.
//
// group selects the group context for rule evaluation:
//   - non-empty:  evaluate against that group's bound rules (mirrors how the
//                 production relay path would judge the same input). The
//                 group does NOT need to be inside ModerationSetting
//                 .EnabledGroups — debug bypasses the whitelist gate so
//                 admins can rehearse before flipping it on.
//   - empty:      fall back to the legacy preview behavior, which scans
//                 every enabled rule regardless of group bindings — useful
//                 for "would any rule have fired?" rehearsals before any
//                 group is bound.
func SubmitModerationDebug(text string, images []string, group string) (string, error) {
	cfg := operation_setting.GetModerationSetting()
	if !cfg.Enabled {
		return "", errors.New("moderation is disabled in global config")
	}
	if globalModerationCenter.keyRing == nil || globalModerationCenter.keyRing.Size() == 0 {
		return "", errors.New("no moderation api key configured")
	}
	text = strings.TrimSpace(text)
	cleanedImages := make([]string, 0, len(images))
	for _, img := range images {
		img = strings.TrimSpace(img)
		if img == "" {
			continue
		}
		cleanedImages = append(cleanedImages, img)
	}
	if text == "" && len(cleanedImages) == 0 {
		return "", errors.New("text and images cannot both be empty")
	}
	requestID := "mod-debug-" + common.GetTimeString() + common.GetRandomString(6)
	occurAt := common.GetTimestamp()
	effectiveGroup := strings.TrimSpace(group)
	if effectiveGroup == "" {
		effectiveGroup = "__debug__"
	}
	event := &moderationEvent{
		OccurAt:   occurAt,
		Source:    "debug",
		Group:     effectiveGroup,
		RequestID: requestID,
		TextItems: []string{text},
		Images:    cleanedImages,
		Done:      make(chan *ModerationResult, 1),
	}
	if err := globalModerationCenter.enqueueDirect(event); err != nil {
		return "", err
	}
	return requestID, nil
}

// GetModerationDebugResult returns the result for the given debug request_id
// once the worker has produced it, or (nil, false) while still pending.
func GetModerationDebugResult(requestID string) (*ModerationResult, bool) {
	v, ok := globalModerationCenter.debugStore.Load(requestID)
	if !ok {
		return nil, false
	}
	entry, ok := v.(*moderationDebugEntry)
	if !ok || entry == nil {
		return nil, false
	}
	return entry.Result, true
}

// PreflightModerationHook is a placeholder for future enforce-mode support.
// In v2 it always allows; future versions may wait briefly on a debug-style
// Done channel before deciding. The signature is locked in now so callers in
// controller/relay.go don't need to change later.
func PreflightModerationHook(ctx context.Context, info *relaycommon.RelayInfo, meta *types.TokenCountMeta) (allow bool, reason string) {
	_ = ctx
	_ = info
	_ = meta
	return true, ""
}

// enqueue uses ring-buffer semantics — when the queue is full we discard
// the OLDEST queued event so the most recent moderation observation always
// has a chance to be processed.
//
// When the Redis write-ahead log is enabled the event is also pushed to
// rc:mod:queue so it survives a restart. Redis push errors do not affect
// the in-memory enqueue path — persistence is best-effort.
func (m *moderationCenter) enqueue(event *moderationEvent) {
	if event == nil || m.queue == nil {
		return
	}
	if m.redisQueue != nil {
		if payload, err := common.Marshal(event); err == nil {
			if err := m.redisQueue.enqueue(string(payload)); err != nil {
				common.SysError("moderation redis enqueue failed: " + err.Error())
			}
		}
	}
	select {
	case m.queue <- event:
		return
	default:
	}
	// Drop one old event then retry. The two select-defaults handle the
	// rare but possible race where another worker drains the queue
	// between our two attempts.
	select {
	case <-m.queue:
		m.dropCount.Add(1)
	default:
	}
	select {
	case m.queue <- event:
	default:
		m.dropCount.Add(1)
	}
	if m.dropCount.Load()%100 == 1 {
		common.SysLog("moderation event queue is full, ring-buffer dropping oldest")
	}
}

// enqueueDirect blocks up to a short window for debug events so admins don't
// hit "queue full" in the debug UI. Production traffic still uses enqueue().
func (m *moderationCenter) enqueueDirect(event *moderationEvent) error {
	if event == nil || m.queue == nil {
		return errors.New("moderation center not started")
	}
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case m.queue <- event:
		return nil
	case <-timer.C:
		return errors.New("moderation queue is full, please retry")
	}
}

func (m *moderationCenter) runWorker(workerID int) {
	for event := range m.queue {
		if event == nil {
			continue
		}
		m.markWorker(workerID, "processing", common.GetTimestamp())
		result := m.processEvent(event)
		m.recordResult(event, result)
		// Acknowledge the Redis write-ahead log entry. Best-effort — a
		// stale entry just gets retried on next startup, which is the
		// correct failure mode.
		if m.redisQueue != nil {
			if payload, err := common.Marshal(event); err == nil {
				_ = m.redisQueue.complete(string(payload))
			}
		}
		m.markWorker(workerID, "idle", common.GetTimestamp())
	}
}

func (m *moderationCenter) markWorker(workerID int, state string, ts int64) {
	v, ok := m.workerState.Load(workerID)
	if !ok {
		ws := &moderationWorkerState{State: state, SinceUnix: ts, LastEventAt: ts}
		m.workerState.Store(workerID, ws)
		return
	}
	ws := v.(*moderationWorkerState)
	ws.State = state
	ws.SinceUnix = ts
	if state == "idle" {
		ws.LastEventAt = ts
	}
}

// QueueStats powers the UI live status card. Snapshot is best-effort —
// reads are unsynchronised against worker mutations because operators
// only need ballpark numbers, not transactional accuracy.
func QueueStats() map[string]any {
	m := globalModerationCenter
	if m == nil {
		return map[string]any{"queue_depth_memory": 0, "worker_count": 0}
	}
	memDepth := 0
	if m.queue != nil {
		memDepth = len(m.queue)
	}
	redisDepth := int64(0)
	redisAvailable := false
	if m.redisQueue != nil {
		redisDepth = m.redisQueue.depth()
		redisAvailable = true
	}
	workerCount := 0
	workers := make([]map[string]any, 0)
	m.workerState.Range(func(key, value any) bool {
		ws, ok := value.(*moderationWorkerState)
		if !ok || ws == nil {
			return true
		}
		workerCount++
		workers = append(workers, map[string]any{
			"id":            key,
			"state":         ws.State,
			"since":         ws.SinceUnix,
			"last_event_at": ws.LastEventAt,
		})
		return true
	})
	out := map[string]any{
		"queue_depth_memory": memDepth,
		"queue_depth_redis":  redisDepth,
		"redis_available":    redisAvailable,
		"worker_count":       workerCount,
		"worker_state":       workers,
		"drop_count_total":   m.dropCount.Load(),
	}
	return out
}

func (m *moderationCenter) recordResult(event *moderationEvent, result *ModerationResult) {
	cfg := operation_setting.GetModerationSetting()
	// Run the rule engine against the API response (no-op for debug events
	// that hit a non-existent group). Decision is attached to the result
	// before notifying any waiter so the debug card can render it.
	if result != nil && result.Error == "" && event.Group != "" && event.Group != "__debug__" {
		matched := EvaluateModerationRules(event.Group, result)
		decision := BuildModerationDecision(matched)
		result.Decision = &decision
	} else if result != nil && event.Source == "debug" {
		// Debug runs evaluate against EVERY enabled rule by default (group
		// is "__debug__", which never matches a configured rule). Provide a
		// "preview" view by re-running against all whitelisted groups so
		// the admin sees what would have happened in production.
		preview := previewModerationDecision(result, cfg)
		result.Decision = &preview
	}
	if event.Done != nil {
		select {
		case event.Done <- result:
		default:
		}
	}
	if event.Source == "debug" {
		ttl := time.Duration(cfg.DebugResultRetainMin) * time.Minute
		m.debugStore.Store(event.RequestID, &moderationDebugEntry{
			Result:    result,
			ExpiresAt: time.Now().Add(ttl).Unix(),
		})
	}
	if result == nil || result.Error != "" {
		// Failures are not persisted for relay traffic; debug callers see
		// the error via the polled Done channel.
		return
	}
	// Every successfully scored request gets an incident row regardless of
	// whether a rule fired — operators need a complete audit trail to
	// answer "did moderation actually run on this request" without ambiguity.
	// flagged distinguishes rule hits from benign rows; the two-tier
	// retention policy (BenignRetentionHours / FlaggedRetentionHours) keeps
	// the table from growing unbounded by aging benign rows out faster.
	matchedJSON := ""
	primaryRule := ""
	decision := ModerationDecisionAllow
	flagged := false
	if result.Decision != nil {
		matchedJSON = encodeRiskJSON(result.Decision.MatchedRules)
		primaryRule = result.Decision.PrimaryRuleName
		decision = result.Decision.Decision
		flagged = decision != ModerationDecisionAllow
	}
	if !flagged && !cfg.RecordUnmatchedInputs {
		return
	}
	incident := &model.ModerationIncident{
		CreatedAt:         event.OccurAt,
		UserID:            event.UserID,
		TokenID:           event.TokenID,
		Username:          event.Username,
		TokenName:         event.TokenName,
		TokenMaskedKey:    event.TokenMaskedKey,
		Group:             event.Group,
		RequestID:         event.RequestID,
		Model:             cfg.Model,
		Flagged:           flagged,
		MaxScore:          result.MaxScore,
		MaxCategory:       result.MaxCategory,
		Categories:        encodeRiskJSON(result.Categories),
		AppliedTypes:      encodeRiskJSON(result.AppliedTypes),
		InputSummary:      buildModerationInputSummary(event, flagged),
		UpstreamLatencyMS: result.UpstreamLatencyMS,
		Source:            event.Source,
		Decision:          decision,
		PrimaryRule:       primaryRule,
		MatchedRules:      matchedJSON,
	}
	// 17 RPS sampled traffic ⇒ a few INSERTs/sec at worst, so we write
	// directly without a batcher.
	if err := model.CreateModerationIncident(incident); err != nil {
		common.SysError("moderation create incident failed: " + err.Error())
	}
	// Forward to the unified enforcement layer once the rule decision is
	// not allow. Debug events stay local — they are admin-driven and
	// shouldn't bump production counters or send users emails.
	if event.Source != "debug" && event.UserID > 0 && event.Group != "" &&
		decision != ModerationDecisionAllow {
		EnforcementHit(event.UserID, event.Group, operation_setting.EnforcementSourceModeration,
			"moderation_"+decision)
	}
}

// previewModerationDecision powers the debug card: try evaluating the
// current API response against every enabled rule (regardless of group)
// and return the worst decision. The admin gets to see what would have
// fired, even though no real production group is involved.
func previewModerationDecision(result *ModerationResult, cfg *operation_setting.ModerationSetting) types.ModerationDecision {
	if result == nil {
		return types.ModerationDecision{Decision: ModerationDecisionAllow}
	}
	rules := currentModerationRules()
	matched := make([]types.ModerationMatchedRule, 0)
	for _, rule := range rules {
		if !rule.Raw.Enabled {
			continue
		}
		hit, conds := matchesModerationRule(rule, result)
		if !hit {
			continue
		}
		matched = append(matched, types.ModerationMatchedRule{
			RuleID:            rule.Raw.Id,
			Name:              rule.Raw.Name,
			Action:            rule.Raw.Action,
			ScoreWeight:       rule.Raw.ScoreWeight,
			MatchedConditions: conds,
		})
	}
	return BuildModerationDecision(matched)
}

func buildModerationInputSummary(event *moderationEvent, fullText bool) string {
	if event == nil {
		return ""
	}
	var sb strings.Builder
	for _, t := range event.TextItems {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if !fullText && len(t) > 200 {
			t = t[:200] + "..."
		}
		sb.WriteString(t)
		sb.WriteString(" ")
	}
	if len(event.Images) > 0 {
		sb.WriteString(fmt.Sprintf("[+%d image(s)]", len(event.Images)))
	}
	return strings.TrimSpace(sb.String())
}

// processEvent issues the actual OpenAI call with retry/backoff. It always
// returns a non-nil *ModerationResult, populating Error on failure rather
// than nil so callers can distinguish "queue dropped" from "API failed".
func (m *moderationCenter) processEvent(event *moderationEvent) *ModerationResult {
	cfg := operation_setting.GetModerationSetting()
	result := &ModerationResult{
		RequestID:   event.RequestID,
		Categories:  map[string]float64{},
		AppliedTypes: map[string][]string{},
		CompletedAt: time.Now().Unix(),
	}
	if m.keyRing == nil || m.keyRing.Size() == 0 {
		result.Error = "no api key configured"
		return result
	}
	body, err := buildModerationRequest(cfg.Model, event.TextItems, event.Images)
	if err != nil {
		result.Error = "build request failed: " + err.Error()
		return result
	}
	maxAttempts := cfg.MaxRetries + 1
	for attempt := 0; attempt < maxAttempts; attempt++ {
		key, idx, ok := m.keyRing.NextKey()
		if !ok {
			result.Error = "all api keys are cooling down"
			return result
		}
		started := time.Now()
		resp, callErr := m.callOpenAI(cfg.BaseURL, key, body)
		latencyMS := int(time.Since(started).Milliseconds())
		result.UpstreamLatencyMS = latencyMS
		result.UsedKeySuffix = operation_setting.MaskModerationKey(key)
		if callErr != nil {
			// network / timeout — cool the key briefly and retry next.
			m.keyRing.MarkFailed(idx, time.Second*time.Duration(1<<attempt))
			result.Error = callErr.Error()
			continue
		}
		switch {
		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			parseErr := parseModerationResponse(resp.Body, result)
			_ = resp.Body.Close()
			if parseErr != nil {
				result.Error = "parse response failed: " + parseErr.Error()
				return result
			}
			result.Error = ""
			return result
		case resp.StatusCode == 429:
			retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
			if retryAfter <= 0 {
				retryAfter = time.Second * time.Duration(1<<attempt)
			}
			m.keyRing.MarkFailed(idx, retryAfter)
			_ = resp.Body.Close()
			result.Error = "upstream 429"
			continue
		case resp.StatusCode >= 500:
			_ = resp.Body.Close()
			m.keyRing.MarkFailed(idx, time.Second*time.Duration(1<<attempt))
			result.Error = "upstream " + strconv.Itoa(resp.StatusCode)
			continue
		default:
			// 4xx other than 429 — input issue, do not retry.
			respBody, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			result.Error = fmt.Sprintf("upstream %d: %s", resp.StatusCode, truncateModerationBody(string(respBody), 240))
			return result
		}
	}
	if result.Error == "" {
		result.Error = "max retries exhausted"
	}
	return result
}

func (m *moderationCenter) callOpenAI(baseURL, key string, body []byte) (*http.Response, error) {
	url := baseURL + "/v1/moderations"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	if m.httpClient == nil {
		m.httpClient = &http.Client{Timeout: 5 * time.Second}
	}
	return m.httpClient.Do(req)
}

func parseRetryAfter(h string) time.Duration {
	h = strings.TrimSpace(h)
	if h == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(h); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if t, err := http.ParseTime(h); err == nil {
		d := time.Until(t)
		if d > 0 {
			return d
		}
	}
	return 0
}

func truncateModerationBody(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// buildModerationRequest builds the OpenAI /v1/moderations payload. The
// official schema accepts:
//
//	{ "model": "...", "input": [
//	    {"type":"text", "text":"..."},
//	    {"type":"image_url", "image_url": {"url": "..."}}
//	]}
//
// We always emit the multi-modal array form (even for text-only) so the
// upstream parser is consistent.
func buildModerationRequest(model string, texts, images []string) ([]byte, error) {
	type imageURL struct {
		URL string `json:"url"`
	}
	type inputItem struct {
		Type     string    `json:"type"`
		Text     string    `json:"text,omitempty"`
		ImageURL *imageURL `json:"image_url,omitempty"`
	}
	items := make([]inputItem, 0, len(texts)+len(images))
	for _, t := range texts {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		items = append(items, inputItem{Type: "text", Text: t})
	}
	for _, img := range images {
		img = strings.TrimSpace(img)
		if img == "" {
			continue
		}
		items = append(items, inputItem{Type: "image_url", ImageURL: &imageURL{URL: img}})
	}
	if len(items) == 0 {
		return nil, errors.New("empty input")
	}
	payload := map[string]any{
		"model": model,
		"input": items,
	}
	return common.Marshal(payload)
}

// parseModerationResponse unwraps the official response schema:
//
//	{ "id": "...", "model": "...", "results": [
//	    { "flagged": bool,
//	      "categories": { "harassment": bool, ... },
//	      "category_scores": { "harassment": 0.123, ... },
//	      "category_applied_input_types": { "harassment": ["text"], ... } } ] }
//
// Multiple input items collapse into a single results entry; we fold the
// scores by max and bubble up the worst category so admins see the most
// alarming signal.
func parseModerationResponse(body io.Reader, out *ModerationResult) error {
	raw, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	type respShape struct {
		Results []struct {
			Flagged                   bool                `json:"flagged"`
			CategoryScores            map[string]float64  `json:"category_scores"`
			CategoryAppliedInputTypes map[string][]string `json:"category_applied_input_types"`
		} `json:"results"`
	}
	var resp respShape
	if err := common.UnmarshalJsonStr(string(raw), &resp); err != nil {
		return err
	}
	if len(resp.Results) == 0 {
		return errors.New("empty results array")
	}
	maxScore := 0.0
	maxCategory := ""
	flagged := false
	categories := map[string]float64{}
	for _, result := range resp.Results {
		if result.Flagged {
			flagged = true
		}
		for cat, score := range result.CategoryScores {
			if score > categories[cat] {
				categories[cat] = score
			}
			if score > maxScore {
				maxScore = score
				maxCategory = cat
			}
		}
		for cat, types_ := range result.CategoryAppliedInputTypes {
			out.AppliedTypes[cat] = types_
		}
	}
	out.Flagged = flagged
	out.MaxScore = maxScore
	out.MaxCategory = maxCategory
	out.Categories = categories
	return nil
}

func (m *moderationCenter) runRetentionLoop() {
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		cfg := operation_setting.GetModerationSetting()
		now := common.GetTimestamp()
		flaggedCutoff := now - int64(cfg.FlaggedRetentionHours)*3600
		benignCutoff := now - int64(cfg.BenignRetentionHours)*3600
		if err := model.DeleteExpiredModerationIncidents(flaggedCutoff, benignCutoff); err != nil {
			common.SysError("moderation cleanup failed: " + err.Error())
		}
	}
}

func (m *moderationCenter) runDebugSweeper() {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now().Unix()
		m.debugStore.Range(func(key, value any) bool {
			entry, ok := value.(*moderationDebugEntry)
			if !ok || entry == nil || entry.ExpiresAt <= now {
				m.debugStore.Delete(key)
			}
			return true
		})
	}
}

// ModerationOverview powers the admin overview card in the new tab.
func ModerationOverview() (map[string]any, error) {
	cfg := operation_setting.GetModerationSetting()
	since := time.Now().Add(-24 * time.Hour).Unix()
	flagged24h, err := model.CountFlaggedModerationIncidentsSince(since)
	if err != nil {
		return nil, err
	}
	ruleCount, err := model.CountModerationRules()
	if err != nil {
		return nil, err
	}
	unconfigured, err := model.CountEnabledModerationRulesWithoutGroups()
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"enabled":                 cfg.Enabled,
		"mode":                    cfg.Mode,
		"key_count":               len(cfg.APIKeys),
		"queue_dropped":           globalModerationCenter.dropCount.Load(),
		"flagged_24h":             flagged24h,
		"sampling_rate_percent":   cfg.SamplingRatePercent,
		"enabled_group_count":     len(cfg.EnabledGroups),
		"rule_count":              ruleCount,
		"unconfigured_rule_count": unconfigured,
	}, nil
}
