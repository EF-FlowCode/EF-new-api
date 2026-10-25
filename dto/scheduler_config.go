package dto

// SchedulerConfig 是 multi-key 调度器的 per-channel 配置，
// 序列化为 channels.channel_info.scheduler_config。
//
// 设计目标：在 multi-key 渠道（is_multi_key=true）下，按 (key, model) 维度
// 单独记账 RPM/TPM/并发，撞 429 时把该 (key,model) 临时 cool 几秒，
// 选 key 时按 headroom 评分挑余量最大的。**不与 channel-级限流冲突**：
// channel_limiter 是渠道维度，本配置是 sk 维度，两道闸串联。
type SchedulerConfig struct {
	Enabled      bool                             `json:"enabled,omitempty"`
	Policy       SchedulerPolicy                  `json:"policy,omitempty"`
	Models       map[string]ModelQuota            `json:"models,omitempty"`        // model -> 默认配额
	KeyOverrides map[string]map[string]ModelQuota `json:"key_overrides,omitempty"` // keyFp -> model -> 配额（覆盖默认）
	Affinity     SchedulerAffinity                `json:"affinity,omitempty"`      // 会话级 key 亲和（保 prompt cache）
}

// SchedulerAffinity 会话级 key 亲和：把同一来源（默认 user_id）的请求粘在同一把 sk 上，
// 保住上游按账号隔离的 prompt cache——换号 = prefix cache 全 miss、又贵又慢。亲和是「软」的：
// pin 的 key cooling / 撞限 / 在途过高时自动让位给评分摊开（score.go 的 least-in-flight），
// 兼顾"缓存命中"与"别堆在卡死的 key 上"。
//
// 默认开启：零值 Disabled=false 即生效，无需重存渠道配置。
type SchedulerAffinity struct {
	Disabled    bool `json:"disabled,omitempty"`      // 显式置 true 才关闭；默认开
	TTLSeconds  int  `json:"ttl_seconds,omitempty"`   // pin 存活 + 每次命中滑动续期秒数，<=0 用默认 600
	InFlightCap int  `json:"in_flight_cap,omitempty"` // 运维显式 >0 才按在途让位；默认 0=关（in_flight 会泄漏、不可靠，不作默认护栏）
}

// SchedulerPolicy 调度器行为策略。
//
// OnLimit 取值与 channel_limiter 保持一致：
//   - "skip"   => 当前 (key,model) 满载就跳过，去试下一把 sk（默认）
//   - "queue"  => 串行排队等最多 QueueMaxWaitMs（Phase 1 暂不实现）
//   - "reject" => 直接 429，不再 retry
type SchedulerPolicy struct {
	OnLimit        string `json:"on_limit,omitempty"`
	QueueMaxWaitMs int    `json:"queue_max_wait_ms,omitempty"`
	CoolSeconds    int    `json:"cool_seconds,omitempty"`          // 撞 429 后该 (key,model) 静默秒数，默认 30
	SelfLearning   bool   `json:"self_learning_enabled,omitempty"` // 实测撞限 RPM 写 observed_max，仅用于显示
}

// ModelQuota 一个模型在某个 sk 上的配额上限。
//
// 三个上限均 0 表示"该 model 在调度器作用域内但暂不限"，仍享受 cool 兜底。
type ModelQuota struct {
	RPMMax         int `json:"rpm_max,omitempty"`
	TPMMax         int `json:"tpm_max,omitempty"`
	ConcurrencyMax int `json:"concurrency_max,omitempty"`
}
