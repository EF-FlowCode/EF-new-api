package multi_key_scheduler

import (
	"fmt"
	"strings"

	"eflow.local/new-api/dto"
)

// 满载策略，与 channel_limiter 保持一致
const (
	OnLimitSkip   = "skip"
	OnLimitQueue  = "queue"
	OnLimitReject = "reject"
)

// NormalizeConfig 对 SchedulerConfig 做服务端归一化，调用方应在「保存进
// channel_info 前」过一遍。规则：
//   - OnLimit 取值非法时回退 "skip"（与 channel_limiter 默认一致）
//   - CoolSeconds <= 0 时填 DefaultCoolSeconds(30)
//   - QueueMaxWaitMs <= 0 时填 2000（与 channel_limiter 一致，仅 OnLimit=queue 才有意义）
//   - Models / KeyOverrides 中所有 ModelQuota 的负数字段全部重置为 0
//   - 空字符串的 model name 与 keyFp 直接剔除（防止脏数据混进 Redis key）
//
// 不做的事：
//   - 不强制 Models 非空（用户可能先开关再加配置）
//   - 不做正则校验 model name（dashscope 模型名格式很自由）
func NormalizeConfig(cfg *dto.SchedulerConfig) {
	if cfg == nil {
		return
	}
	switch cfg.Policy.OnLimit {
	case OnLimitSkip, OnLimitQueue, OnLimitReject:
	default:
		cfg.Policy.OnLimit = OnLimitSkip
	}
	if cfg.Policy.CoolSeconds <= 0 {
		cfg.Policy.CoolSeconds = DefaultCoolSeconds
	}
	if cfg.Policy.QueueMaxWaitMs <= 0 {
		cfg.Policy.QueueMaxWaitMs = 2000
	}

	// 亲和负值归零，让运行时回退到 DefaultAffinity*（Disabled 为 opt-out，默认开）
	if cfg.Affinity.TTLSeconds < 0 {
		cfg.Affinity.TTLSeconds = 0
	}
	if cfg.Affinity.InFlightCap < 0 {
		cfg.Affinity.InFlightCap = 0
	}

	cfg.Models = sanitizeModels(cfg.Models)
	if cfg.KeyOverrides != nil {
		cleaned := make(map[string]map[string]dto.ModelQuota, len(cfg.KeyOverrides))
		for fp, perModel := range cfg.KeyOverrides {
			fp = strings.TrimSpace(fp)
			if fp == "" {
				continue
			}
			s := sanitizeModels(perModel)
			if len(s) > 0 {
				cleaned[fp] = s
			}
		}
		cfg.KeyOverrides = cleaned
	}
}

func sanitizeModels(in map[string]dto.ModelQuota) map[string]dto.ModelQuota {
	if in == nil {
		return nil
	}
	out := make(map[string]dto.ModelQuota, len(in))
	for name, q := range in {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if q.RPMMax < 0 {
			q.RPMMax = 0
		}
		if q.TPMMax < 0 {
			q.TPMMax = 0
		}
		if q.ConcurrencyMax < 0 {
			q.ConcurrencyMax = 0
		}
		out[name] = q
	}
	return out
}

// ValidateConfig 给 admin-api 用：返回人类可读的问题清单，保存按钮要不要拦截看调用方。
//
// Phase 1 仅做轻校验，不做严格类型/范围阻挡——上层 UI 还会再校验一次。
func ValidateConfig(cfg *dto.SchedulerConfig) []string {
	var problems []string
	if cfg == nil {
		return problems
	}
	if cfg.Policy.CoolSeconds < 0 {
		problems = append(problems, "policy.cool_seconds must be >= 0")
	}
	if cfg.Policy.CoolSeconds > 3600 {
		problems = append(problems, "policy.cool_seconds > 3600 看着不太像合理值，请确认")
	}
	switch cfg.Policy.OnLimit {
	case "", OnLimitSkip, OnLimitQueue, OnLimitReject:
	default:
		problems = append(problems, fmt.Sprintf("policy.on_limit %q invalid (允许 skip/queue/reject)", cfg.Policy.OnLimit))
	}
	for name, q := range cfg.Models {
		if name == "" {
			problems = append(problems, "models 含空 model 名")
			continue
		}
		problems = append(problems, validateQuota("models["+name+"]", q)...)
	}
	for fp, perModel := range cfg.KeyOverrides {
		for name, q := range perModel {
			problems = append(problems, validateQuota(fmt.Sprintf("key_overrides[%s][%s]", fp, name), q)...)
		}
	}
	return problems
}

func validateQuota(label string, q dto.ModelQuota) []string {
	var problems []string
	if q.RPMMax < 0 {
		problems = append(problems, label+".rpm_max < 0")
	}
	if q.TPMMax < 0 {
		problems = append(problems, label+".tpm_max < 0")
	}
	if q.ConcurrencyMax < 0 {
		problems = append(problems, label+".concurrency_max < 0")
	}
	return problems
}
