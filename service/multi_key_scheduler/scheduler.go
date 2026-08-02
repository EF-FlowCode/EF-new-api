// Package multi_key_scheduler 提供 multi-key 渠道下 per-(key, model) 的调度能力。
//
// 与 channel_limiter（渠道级）正交：channel_limiter 是渠道整体 RPM / 并发，
// 本调度器精确到每把 sk 在每个 model 上的 RPM / TPM / 并发，外加 cool 兜底——
// 撞到上游 429 / Throttling 时把该 (key, model) 静默几秒，让选 key 算法绕过。
//
// 适用前提：
//   - channel.ChannelInfo.IsMultiKey = true
//   - channel.ChannelInfo.SchedulerConfig.Enabled = true
//
// 不满足时调用方应继续走 channel.GetNextEnabledKey() 原逻辑，
// **本包绝不动** channels 表 / MultiKeyStatusList / channel.Status。cool 是
// 瞬态 Redis 状态，与 admin-api BSS 余额监控管的 enabled=false 严格分层。
package multi_key_scheduler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

// 拒绝原因
const (
	ReasonRPMExceeded         = "rpm_exceeded"
	ReasonTPMExceeded         = "tpm_exceeded"
	ReasonConcurrencyExceeded = "concurrency_exceeded"
	ReasonCooling             = "cooling"
	ReasonAllUnavailable      = "all_unavailable"
)

// 默认值
const (
	DefaultCoolSeconds = 30
	// 会话级 key 亲和默认值（见 dto.SchedulerAffinity）
	DefaultAffinityTTLSeconds  = 600 // pin 存活 + 滑动续期秒数
	DefaultAffinityInFlightCap = 0   // 默认不按 in_flight 让位：in_flight 会因 hung/client-gone/重启泄漏、不可靠；>0 时运维显式开启该护栏
	rpmWindowSeconds           = 60
	tpmWindowSeconds           = 60
	defaultEntryTTL      = 300 // Redis hash key TTL
	observedMaxTTL       = 86400
)

// KeyCandidate 调用方传入的候选 key（一般是 channel 中所有 enabled 状态的 key）。
type KeyCandidate struct {
	Index int    // 在 channel.GetKeys() 中的下标
	Fp    string // 调用方可预先 KeyFingerprint(key) 算好；空字符串本包会自行计算
	raw   string // 仅在 Fp 为空时用来回填指纹，不进 Redis
}

// AcquireResult 一次选 key 的判定结果。
//
// Allowed=true 时 KeyIndex / KeyFp 有效，且并发槽位已预占——上游请求结束后
// 必须调用 ReportSuccess 或 ReportError，二者都会释放该槽位。
type AcquireResult struct {
	Allowed  bool
	KeyIndex int
	KeyFp    string
	Reason   string // 不允许时填一种 ReasonXxx
}

// StateEntry 是 SnapshotChannel 返回给 admin-api 状态面板的一行。
type StateEntry struct {
	KeyFp        string `json:"key_fp"`
	Model        string `json:"model"`
	RPMCount     int    `json:"rpm_count"`
	TPMCount     int    `json:"tpm_count"`
	InFlight     int    `json:"in_flight"`
	CoolUntilMs  int64  `json:"cool_until_ms,omitempty"`
	Last429AtMs  int64  `json:"last_429_at_ms,omitempty"`
	Last429Msg   string `json:"last_429_msg,omitempty"`
	ObservedMax  int    `json:"observed_max,omitempty"`
}

// IsEnabledFor 判定调度器是否应介入：cfg 自身允许 + 至少配了一个 model 配额。
//
// 没配 model 时本调度器不介入，让原 GetNextEnabledKey 走默认 random / polling，
// 否则用户开启 enabled 但忘了配 model 时全部走 ReasonAllUnavailable，体验崩坏。
func IsEnabledFor(cfg *dto.SchedulerConfig) bool {
	if cfg == nil || !cfg.Enabled {
		return false
	}
	return len(cfg.Models) > 0
}

// QuotaFor 取 (keyFp, model) 的有效配额：先看 KeyOverrides，再回退 Models。
// 命中不到时返回零值（三上限都 0），仍允许 Acquire 但不限 RPM/TPM/并发——只剩 cool 兜底。
func QuotaFor(cfg *dto.SchedulerConfig, keyFp, model string) dto.ModelQuota {
	if cfg == nil {
		return dto.ModelQuota{}
	}
	if perKey, ok := cfg.KeyOverrides[keyFp]; ok {
		if q, ok2 := perKey[model]; ok2 {
			return q
		}
	}
	if q, ok := cfg.Models[model]; ok {
		return q
	}
	return dto.ModelQuota{}
}

// Acquire 在 candidates 中挑一把 key 并预占。
//
// 流程：
//  0. 会话级亲和（affinityValue 非空且未禁用）：先看有没有 pin，pin 的 key 仍健康
//     （未 cool / 未撞限 / 在途 < cap）就直接复用，保住该账号上游的 prompt cache；
//  1. 无 pin / pin 不健康 / pin 抢占失败 → 过滤未 cool 的 candidate；
//  2. 按 score.go 评分（healthy band 内 least-in-flight）挑一把并原子预占；
//  3. 亲和开启时把这次选中的 key 记成新 pin（滑动续期）。
//
// affinityValue 一般是 user_id（调用方从 gin context 取）；空串 = 不走亲和、行为同纯评分。
// 注：candidates 应只包含 ChannelStatusEnabled 的 key——本包不读 MultiKeyStatusList。
func Acquire(ctx context.Context, channelID int, model string, candidates []KeyCandidate, cfg *dto.SchedulerConfig, affinityValue string) AcquireResult {
	if !IsEnabledFor(cfg) || len(candidates) == 0 {
		return AcquireResult{Allowed: false, Reason: ReasonAllUnavailable}
	}
	defer func() {
		// 任何 panic 都不能拖垮调度路径——调用方上层会 fallback 原 GetNextEnabledKey
		if r := recover(); r != nil {
			common.SysError(fmt.Sprintf("multi_key_scheduler.Acquire panic recovered channel=%d model=%s err=%v", channelID, model, r))
		}
	}()

	prepared := prepareCandidates(candidates)
	if len(prepared) == 0 {
		return AcquireResult{Allowed: false, Reason: ReasonAllUnavailable}
	}
	be := getBackend()
	useAffinity := affinityEnabled(cfg) && affinityValue != ""

	// keepPin：pin 的 key 只是「此刻并发忙」(临时突发) 而非失效时置 true——本次临时分流到
	// 别的 key，但**保留原 pin**，突发过后下次请求仍回到这把带缓存的 key（避免 pin 漂移、
	// 丢掉好不容易建起来的 prompt cache）。
	keepPin := false

	// 0) 亲和优先：pin 命中且健康 → 直接复用，保住该账号的 prompt cache。
	if useAffinity {
		if pinnedFp, ok := be.GetAffinity(ctx, channelID, model, affinityValue); ok {
			if cand := findCandidateByFp(prepared, pinnedFp); cand != nil {
				quota := QuotaFor(cfg, cand.Fp, model)
				dec := be.Peek(ctx, channelID, cand.Fp, model, quota)
				inflightCap := affinityInFlightCap(cfg)
				switch {
				case dec.Cooling || !dec.Allowed:
					// pin 的 key 暂时 cool / 撞限(RPM/TPM) → 本次临时分流，但**保留 pin**：
					// 冷却/限流是瞬态的，过后用户回到这把带缓存的 key，不漂移。若该 key 真
					// 坏了会被禁用 → 从候选里消失 → findCandidateByFp 取不到 → 那时才重钉。
					keepPin = true
				case inflightCap > 0 && dec.InFlight >= inflightCap:
					// 仅当运维显式设了 in_flight 上限(>0)才生效。默认 0=关——in_flight 会因
					// hung/client-gone/容器重启泄漏，拿它当默认护栏会把 pin 永久判“忙”、踢散所有人。
					keepPin = true
				default:
					if acq := be.Acquire(ctx, channelID, cand.Fp, model, quota); acq.Allowed {
						be.SetAffinity(ctx, channelID, model, affinityValue, cand.Fp, affinityTTL(cfg)) // 滑动续期
						return AcquireResult{Allowed: true, KeyIndex: cand.Index, KeyFp: cand.Fp}
					}
					// 抢占失败(竞态撞限) → fallback，允许重钉
				}
			}
		}
	}

	// 1+2) 评分挑（least-in-flight 摊开）。
	best := pickBest(ctx, be, channelID, model, prepared, cfg)
	if best == nil {
		return AcquireResult{Allowed: false, Reason: ReasonAllUnavailable}
	}

	quota := QuotaFor(cfg, best.Fp, model)
	dec := be.Acquire(ctx, channelID, best.Fp, model, quota)
	if !dec.Allowed {
		return AcquireResult{Allowed: false, Reason: dec.Reason}
	}
	// 3) 记/换 pin——但若只是 pin 的 key 临时并发忙(keepPin)，保留原 pin 不漂移。
	if useAffinity && !keepPin {
		be.SetAffinity(ctx, channelID, model, affinityValue, best.Fp, affinityTTL(cfg))
	}
	return AcquireResult{Allowed: true, KeyIndex: best.Index, KeyFp: best.Fp}
}

// ── 会话级 key 亲和 helpers ───────────────────────────────────────────────────

// affinityEnabled：默认开（零值 Disabled=false），显式 Disabled=true 才关。
func affinityEnabled(cfg *dto.SchedulerConfig) bool {
	return cfg != nil && !cfg.Affinity.Disabled
}

func affinityTTL(cfg *dto.SchedulerConfig) int {
	if cfg != nil && cfg.Affinity.TTLSeconds > 0 {
		return cfg.Affinity.TTLSeconds
	}
	return DefaultAffinityTTLSeconds
}

func affinityInFlightCap(cfg *dto.SchedulerConfig) int {
	if cfg != nil && cfg.Affinity.InFlightCap > 0 {
		return cfg.Affinity.InFlightCap
	}
	return DefaultAffinityInFlightCap
}

func findCandidateByFp(cands []KeyCandidate, fp string) *KeyCandidate {
	if fp == "" {
		return nil
	}
	for i := range cands {
		if cands[i].Fp == fp {
			return &cands[i]
		}
	}
	return nil
}

// ReportSuccess 上游成功返回时调用。释放并发槽位、累加 TPM 计数。
// tokens<=0 表示本次未消费 token（如视频任务提交阶段），仅释放并发。
func ReportSuccess(ctx context.Context, channelID int, keyFp, model string, tokens int, cfg *dto.SchedulerConfig) {
	if !IsEnabledFor(cfg) || keyFp == "" || model == "" {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			common.SysError(fmt.Sprintf("multi_key_scheduler.ReportSuccess panic channel=%d model=%s err=%v", channelID, model, r))
		}
	}()
	getBackend().ReportSuccess(ctx, channelID, keyFp, model, tokens)
}

// ReportError 上游失败时调用：撞 429 或匹配到 RPM/TPM 关键词就给 (key,model) 写 cool；
// 其它错误仅释放并发槽位（不写 cool，避免扩大故障面）。
func ReportError(ctx context.Context, channelID int, keyFp, model string, statusCode int, message string, cfg *dto.SchedulerConfig) {
	if !IsEnabledFor(cfg) || keyFp == "" || model == "" {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			common.SysError(fmt.Sprintf("multi_key_scheduler.ReportError panic channel=%d model=%s err=%v", channelID, model, r))
		}
	}()

	be := getBackend()
	if isRateLimitError(statusCode, message) {
		cool := cfg.Policy.CoolSeconds
		if cool <= 0 {
			cool = DefaultCoolSeconds
		}
		be.MarkCool(ctx, channelID, keyFp, model, cool, message)
	}
	be.ReportError(ctx, channelID, keyFp, model)
}

// SnapshotChannel 给 admin-api 用：返回某 channel 下全部 (keyFp, model) 的实时状态。
// 只读，不会修改 Redis。Redis 不可用时返回 nil。
func SnapshotChannel(ctx context.Context, channelID int) []StateEntry {
	be := getBackend()
	if be == nil {
		return nil
	}
	return be.Snapshot(ctx, channelID)
}

// KeyFingerprint 把明文 key 哈希成 16 字节十六进制串，用作 Redis key 的一部分。
// 明文 key 永远不应进 Redis。
func KeyFingerprint(rawKey string) string {
	if rawKey == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(rawKey))
	return hex.EncodeToString(sum[:8])
}

// prepareCandidates 给 Fp 留空的 candidate 自动算指纹。raw 字段调用方填的话才算，
// 否则保留空 Fp 让 backend 跳过该 candidate（防止误把空指纹写入 Redis）。
func prepareCandidates(in []KeyCandidate) []KeyCandidate {
	out := make([]KeyCandidate, 0, len(in))
	for _, k := range in {
		if k.Fp == "" && k.raw != "" {
			k.Fp = KeyFingerprint(k.raw)
		}
		if k.Fp == "" {
			continue
		}
		out = append(out, k)
	}
	return out
}

// NewCandidate 建议调用方用这个构造 candidate，避免明文 key 漏到 Redis。
func NewCandidate(index int, rawKey string) KeyCandidate {
	return KeyCandidate{Index: index, Fp: KeyFingerprint(rawKey)}
}

// backend 抽象，与 channel_limiter 的双后端模式对齐。
type backend interface {
	Acquire(ctx context.Context, channelID int, keyFp, model string, quota dto.ModelQuota) Decision
	Peek(ctx context.Context, channelID int, keyFp, model string, quota dto.ModelQuota) Decision
	ReportSuccess(ctx context.Context, channelID int, keyFp, model string, tokens int)
	ReportError(ctx context.Context, channelID int, keyFp, model string)
	MarkCool(ctx context.Context, channelID int, keyFp, model string, seconds int, message string)
	Snapshot(ctx context.Context, channelID int) []StateEntry
	// 会话级 key 亲和：把 (channel, model, affinityValue) 钉到某个 keyFp，带 TTL。
	GetAffinity(ctx context.Context, channelID int, model, affinityValue string) (keyFp string, found bool)
	SetAffinity(ctx context.Context, channelID int, model, affinityValue, keyFp string, ttlSeconds int)
}

// Decision backend 的判定输出。InFlight 仅 Peek 时填，用于 score.go 评分。
type Decision struct {
	Allowed  bool
	Reason   string
	RPMCount int
	TPMCount int
	InFlight int
	Cooling  bool
}

var (
	backendOnce sync.Once
	backendImpl backend
)

func getBackend() backend {
	backendOnce.Do(func() {
		if common.RedisEnabled {
			impl, err := newRedisBackend()
			if err != nil {
				common.SysError(fmt.Sprintf("multi_key_scheduler: redis backend init failed, fallback to memory: %v", err))
				backendImpl = newMemoryBackend()
				return
			}
			backendImpl = impl
			common.SysLog("multi_key_scheduler: using redis backend")
		} else {
			backendImpl = newMemoryBackend()
			common.SysLog("multi_key_scheduler: redis disabled, using in-memory backend (single-instance only)")
		}
	})
	return backendImpl
}

// 让单测能注入假后端而不打破 sync.Once。
func setBackendForTest(b backend) {
	backendOnce.Do(func() {})
	backendImpl = b
}