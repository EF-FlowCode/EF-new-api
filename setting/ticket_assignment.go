package setting

import (
	"sort"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
)

// 工单自动分配策略。
const (
	TicketAssignStrategyRoundRobin  = "round_robin"  // 轮询
	TicketAssignStrategyLeastLoaded = "least_loaded" // 最少负载
	TicketAssignStrategyRandom      = "random"       // 随机
	TicketAssignStrategyManual      = "manual"       // 手动：不自动分配，由管理员事后指派
)

// 没有任何可用客服时的兜底行为。
const (
	TicketAssignFallbackNone         = "none"           // 留在待认领池，仅通知管理员
	TicketAssignFallbackGeneralGroup = "general_group"  // 回落到普通工单组
)

// TicketAssignRule 描述一种工单类型的分配规则。
//   - Users：该分组中的候选客服用户 ID 列表；一位客服可同时出现在多个规则里（一人多职）。
//   - Strategy：从候选中选一的策略。
type TicketAssignRule struct {
	Strategy string `json:"strategy"`
	Users    []int  `json:"users"`
}

// TicketAssignConfig 工单分配配置的整体结构，持久化到 OptionMap 的 "TicketAssignConfig" 键。
// Rules 的 key 为 TicketType（general / refund / invoice），未列出的类型视为"未启用自动分配"。
type TicketAssignConfig struct {
	Enabled  bool                        `json:"enabled"`
	Fallback string                      `json:"fallback"`
	Rules    map[string]TicketAssignRule `json:"rules"`
}

// 默认配置：开启自动分配（但规则里没有候选时依然会回落到"待认领"池），
// 保留三个基础规则骨架方便管理员填充。管理员只要把客服加入到对应规则里即可生效。
func defaultTicketAssignConfig() TicketAssignConfig {
	return TicketAssignConfig{
		Enabled:  true,
		Fallback: TicketAssignFallbackNone,
		Rules: map[string]TicketAssignRule{
			"general": {Strategy: TicketAssignStrategyRoundRobin, Users: []int{}},
			"refund":  {Strategy: TicketAssignStrategyRoundRobin, Users: []int{}},
			"invoice": {Strategy: TicketAssignStrategyRoundRobin, Users: []int{}},
		},
	}
}

var (
	ticketAssignConfig   = defaultTicketAssignConfig()
	ticketAssignConfigMu sync.RWMutex
)

// GetTicketAssignConfig 返回当前的分配配置副本（只读），避免调用方意外修改全局状态。
func GetTicketAssignConfig() TicketAssignConfig {
	ticketAssignConfigMu.RLock()
	defer ticketAssignConfigMu.RUnlock()
	return cloneTicketAssignConfig(ticketAssignConfig)
}

// GetTicketAssignRule 获取指定工单类型的规则；不存在时返回 zero 值与 false。
func GetTicketAssignRule(ticketType string) (TicketAssignRule, bool) {
	ticketAssignConfigMu.RLock()
	defer ticketAssignConfigMu.RUnlock()
	key := strings.ToLower(strings.TrimSpace(ticketType))
	if key == "" {
		return TicketAssignRule{}, false
	}
	rule, ok := ticketAssignConfig.Rules[key]
	if !ok {
		return TicketAssignRule{}, false
	}
	return cloneTicketAssignRule(rule), true
}

// GetTicketAssignFallbackRule 当指定类型不可分配时使用的兜底规则。
// 目前唯一支持的兜底是"回落到 general 组"；其它情况返回 false。
func GetTicketAssignFallbackRule() (TicketAssignRule, bool) {
	ticketAssignConfigMu.RLock()
	defer ticketAssignConfigMu.RUnlock()
	if ticketAssignConfig.Fallback != TicketAssignFallbackGeneralGroup {
		return TicketAssignRule{}, false
	}
	rule, ok := ticketAssignConfig.Rules["general"]
	if !ok {
		return TicketAssignRule{}, false
	}
	return cloneTicketAssignRule(rule), true
}

// IsTicketAssignEnabled 是否开启自动分配。
func IsTicketAssignEnabled() bool {
	ticketAssignConfigMu.RLock()
	defer ticketAssignConfigMu.RUnlock()
	return ticketAssignConfig.Enabled
}

// AllAssigneeIds 返回全部规则中出现过的候选客服 ID（去重），
// 主要用于客服视角的"本组池"查询。
func AllAssigneeIds() []int {
	ticketAssignConfigMu.RLock()
	defer ticketAssignConfigMu.RUnlock()
	seen := make(map[int]struct{})
	for _, rule := range ticketAssignConfig.Rules {
		for _, uid := range rule.Users {
			if uid > 0 {
				seen[uid] = struct{}{}
			}
		}
	}
	out := make([]int, 0, len(seen))
	for uid := range seen {
		out = append(out, uid)
	}
	sort.Ints(out)
	return out
}

// TicketTypesForUser 返回某位客服在分配规则中出现过的工单类型列表。
// 用于"待认领池"筛选：该客服只应该看到自己负责类型的未分配工单。
// 若客服不在任何规则中，返回空切片；调用方应把这视为"没有可认领的工单"。
func TicketTypesForUser(userId int) []string {
	ticketAssignConfigMu.RLock()
	defer ticketAssignConfigMu.RUnlock()
	types := make([]string, 0, len(ticketAssignConfig.Rules))
	for typeKey, rule := range ticketAssignConfig.Rules {
		for _, uid := range rule.Users {
			if uid == userId {
				types = append(types, typeKey)
				break
			}
		}
	}
	sort.Strings(types)
	return types
}

// AssigneeIdsForUser 返回一位客服所属的"同组同事"的 ID 集合（包含自身）。
// 计算规则：把此用户出现过的所有规则的 users 并集起来。未在任何规则中出现时返回 {userId}。
// 用于构造"本组待认领池"的查询条件，让客服可以看到自己所在组的未分配工单。
func AssigneeIdsForUser(userId int) []int {
	ticketAssignConfigMu.RLock()
	defer ticketAssignConfigMu.RUnlock()
	seen := map[int]struct{}{userId: {}}
	for _, rule := range ticketAssignConfig.Rules {
		member := false
		for _, uid := range rule.Users {
			if uid == userId {
				member = true
				break
			}
		}
		if member {
			for _, uid := range rule.Users {
				if uid > 0 {
					seen[uid] = struct{}{}
				}
			}
		}
	}
	out := make([]int, 0, len(seen))
	for uid := range seen {
		out = append(out, uid)
	}
	sort.Ints(out)
	return out
}

// UpdateTicketAssignConfigFromJSON 从 JSON 字符串加载配置，用于 OptionMap 热更新。
// 解析失败时保持旧配置不变并返回错误。
func UpdateTicketAssignConfigFromJSON(raw string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		ticketAssignConfigMu.Lock()
		ticketAssignConfig = defaultTicketAssignConfig()
		ticketAssignConfigMu.Unlock()
		return nil
	}
	var cfg TicketAssignConfig
	if err := common.UnmarshalJsonStr(trimmed, &cfg); err != nil {
		return err
	}
	normalized := normalizeTicketAssignConfig(cfg)
	ticketAssignConfigMu.Lock()
	ticketAssignConfig = normalized
	ticketAssignConfigMu.Unlock()
	return nil
}

// TicketAssignConfig2JSONString 输出当前配置的 JSON 表示，用于 InitOptionMap。
func TicketAssignConfig2JSONString() string {
	cfg := GetTicketAssignConfig()
	b, err := common.Marshal(cfg)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// normalizeTicketAssignConfig 清洗配置：去重用户、校验策略与兜底、补全默认规则键。
func normalizeTicketAssignConfig(in TicketAssignConfig) TicketAssignConfig {
	out := TicketAssignConfig{
		Enabled:  in.Enabled,
		Fallback: in.Fallback,
		Rules:    make(map[string]TicketAssignRule, len(in.Rules)),
	}
	if !isValidFallback(out.Fallback) {
		out.Fallback = TicketAssignFallbackNone
	}
	// 保留 general/refund/invoice 三个基本键始终存在，避免前端取值时为空。
	base := defaultTicketAssignConfig().Rules
	for k, v := range base {
		out.Rules[k] = v
	}
	for rawKey, rule := range in.Rules {
		key := strings.ToLower(strings.TrimSpace(rawKey))
		if key == "" {
			continue
		}
		out.Rules[key] = normalizeRule(rule)
	}
	return out
}

func normalizeRule(rule TicketAssignRule) TicketAssignRule {
	strategy := strings.ToLower(strings.TrimSpace(rule.Strategy))
	if !isValidStrategy(strategy) {
		strategy = TicketAssignStrategyRoundRobin
	}
	seen := make(map[int]struct{}, len(rule.Users))
	users := make([]int, 0, len(rule.Users))
	for _, uid := range rule.Users {
		if uid <= 0 {
			continue
		}
		if _, ok := seen[uid]; ok {
			continue
		}
		seen[uid] = struct{}{}
		users = append(users, uid)
	}
	sort.Ints(users)
	return TicketAssignRule{Strategy: strategy, Users: users}
}

func isValidStrategy(s string) bool {
	switch s {
	case TicketAssignStrategyRoundRobin,
		TicketAssignStrategyLeastLoaded,
		TicketAssignStrategyRandom,
		TicketAssignStrategyManual:
		return true
	default:
		return false
	}
}

func isValidFallback(s string) bool {
	switch s {
	case TicketAssignFallbackNone, TicketAssignFallbackGeneralGroup:
		return true
	default:
		return false
	}
}

func cloneTicketAssignConfig(cfg TicketAssignConfig) TicketAssignConfig {
	rules := make(map[string]TicketAssignRule, len(cfg.Rules))
	for k, v := range cfg.Rules {
		rules[k] = cloneTicketAssignRule(v)
	}
	return TicketAssignConfig{
		Enabled:  cfg.Enabled,
		Fallback: cfg.Fallback,
		Rules:    rules,
	}
}

func cloneTicketAssignRule(rule TicketAssignRule) TicketAssignRule {
	users := make([]int, len(rule.Users))
	copy(users, rule.Users)
	return TicketAssignRule{Strategy: rule.Strategy, Users: users}
}
