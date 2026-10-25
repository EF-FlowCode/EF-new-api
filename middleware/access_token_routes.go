package middleware

import (
	"fmt"
	"maps"
	"slices"
)

// eflow: 访问令牌路由规则表，结构与上游 QuantumNous/new-api middleware/access_token_routes.go
// 一致（键为 "METHOD gin 完整路径"）。带范围的访问令牌只能调用这里声明过的控制台路由
// （未声明一律拒绝）；本文件放与上游同名的路由，eflow 独有路由见 access_token_routes_eflow.go。
// router/access_token_scope_test.go 遍历全部路由，缺声明或声明了不存在的路由都会失败。

type accessTokenRuleKind int

const (
	// accessTokenRuleScope requires the token to carry scope.
	accessTokenRuleScope accessTokenRuleKind = iota + 1
	// accessTokenRuleAny accepts every valid scoped token; the handler applies its own checks.
	accessTokenRuleAny
	// accessTokenRuleSession rejects every access token, legacy ones included.
	accessTokenRuleSession
)

type accessTokenRouteRule struct {
	kind  accessTokenRuleKind
	scope string
}

var (
	accessTokenAnyRule     = accessTokenRouteRule{kind: accessTokenRuleAny}
	accessTokenSessionRule = accessTokenRouteRule{kind: accessTokenRuleSession}
)

func accessTokenScopeRule(scope string) accessTokenRouteRule {
	return accessTokenRouteRule{kind: accessTokenRuleScope, scope: scope}
}

var accessTokenCoreRouteRules = map[string]accessTokenRouteRule{
	// top level
	"GET /api/models":                     accessTokenScopeRule("profile:read"),
	"GET /api/status/test":                accessTokenScopeRule("log:read"),
	"GET /api/pricing":                    accessTokenAnyRule,
	"GET /api/perf-metrics/summary":       accessTokenAnyRule,
	"GET /api/perf-metrics":               accessTokenAnyRule,
	"POST /api/oauth/email/bind":          accessTokenScopeRule("account_security:write"),
	"POST /api/oauth/wechat/bind":         accessTokenScopeRule("account_security:write"),
	"POST /api/oauth/telegram/bind/start": accessTokenSessionRule,
	"GET /api/verify/methods":             accessTokenAnyRule,
	"POST /api/verify":                    accessTokenAnyRule,

	// /api/user (self)
	"GET /api/user/self/groups":                    accessTokenScopeRule("profile:read"),
	"GET /api/user/self":                           accessTokenScopeRule("profile:read"),
	"GET /api/user/models":                         accessTokenScopeRule("profile:read"),
	"PUT /api/user/self":                           accessTokenScopeRule("profile:write"),
	"DELETE /api/user/self":                        accessTokenScopeRule("account_security:write"),
	"GET /api/user/sessions":                       accessTokenSessionRule,
	"DELETE /api/user/sessions/:sid":               accessTokenSessionRule,
	"POST /api/user/sessions/revoke-others":        accessTokenSessionRule,
	"GET /api/user/access_tokens":                  accessTokenSessionRule,
	"GET /api/user/access_tokens/catalog":          accessTokenSessionRule,
	"GET /api/user/access_tokens/scopes":           accessTokenSessionRule,
	"POST /api/user/access_tokens":                 accessTokenSessionRule,
	"PATCH /api/user/access_tokens/:id":            accessTokenSessionRule,
	"DELETE /api/user/access_tokens/:id":           accessTokenSessionRule,
	"DELETE /api/user/access_tokens/legacy":        accessTokenSessionRule,
	"GET /api/user/passkey":                        accessTokenScopeRule("account_security:read"),
	"POST /api/user/passkey/register/begin":        accessTokenScopeRule("account_security:write"),
	"POST /api/user/passkey/register/finish":       accessTokenScopeRule("account_security:write"),
	"POST /api/user/passkey/verify/begin":          accessTokenAnyRule,
	"POST /api/user/passkey/verify/finish":         accessTokenAnyRule,
	"DELETE /api/user/passkey":                     accessTokenScopeRule("account_security:write"),
	"GET /api/user/aff":                            accessTokenScopeRule("wallet:read"),
	"GET /api/user/topup/info":                     accessTokenScopeRule("wallet:read"),
	"GET /api/user/topup/self":                     accessTokenScopeRule("wallet:read"),
	"POST /api/user/topup":                         accessTokenScopeRule("wallet:write"),
	"POST /api/user/pay":                           accessTokenScopeRule("wallet:write"),
	"POST /api/user/amount":                        accessTokenScopeRule("wallet:read"),
	"POST /api/user/stripe/pay":                    accessTokenScopeRule("wallet:write"),
	"POST /api/user/stripe/amount":                 accessTokenScopeRule("wallet:read"),
	"POST /api/user/creem/pay":                     accessTokenScopeRule("wallet:write"),
	"POST /api/user/waffo/amount":                  accessTokenScopeRule("wallet:read"),
	"POST /api/user/waffo/pay":                     accessTokenScopeRule("wallet:write"),
	"POST /api/user/waffo-pancake/amount":          accessTokenScopeRule("wallet:read"),
	"POST /api/user/waffo-pancake/pay":             accessTokenScopeRule("wallet:write"),
	"POST /api/user/aff_transfer":                  accessTokenScopeRule("wallet:write"),
	"PUT /api/user/setting":                        accessTokenScopeRule("profile:write"),
	"GET /api/user/2fa/status":                     accessTokenScopeRule("account_security:read"),
	"POST /api/user/2fa/setup":                     accessTokenScopeRule("account_security:write"),
	"POST /api/user/2fa/enable":                    accessTokenScopeRule("account_security:write"),
	"POST /api/user/2fa/disable":                   accessTokenScopeRule("account_security:write"),
	"POST /api/user/2fa/backup_codes":              accessTokenScopeRule("account_security:write"),
	"GET /api/user/checkin":                        accessTokenScopeRule("wallet:read"),
	"POST /api/user/checkin":                       accessTokenScopeRule("wallet:write"),
	"GET /api/user/oauth/bindings":                 accessTokenScopeRule("account_security:read"),
	"DELETE /api/user/oauth/bindings/:provider_id": accessTokenScopeRule("account_security:write"),

	// /api/user (admin)
	"GET /api/user/":                                   accessTokenScopeRule("user:read"),
	"GET /api/user/topup":                              accessTokenScopeRule("billing:read"),
	"POST /api/user/topup/complete":                    accessTokenScopeRule("billing:write"),
	"GET /api/user/search":                             accessTokenScopeRule("user:read"),
	"GET /api/user/:id/oauth/bindings":                 accessTokenScopeRule("user:read"),
	"DELETE /api/user/:id/oauth/bindings/:provider_id": accessTokenScopeRule("user:write"),
	"DELETE /api/user/:id/bindings/:binding_type":      accessTokenScopeRule("user:write"),
	"GET /api/user/:id":                                accessTokenScopeRule("user:read"),
	"POST /api/user/":                                  accessTokenScopeRule("user:write"),
	"POST /api/user/manage":                            accessTokenScopeRule("user:write"),
	"PUT /api/user/":                                   accessTokenScopeRule("user:write"),
	"DELETE /api/user/:id":                             accessTokenScopeRule("user:write"),
	"DELETE /api/user/:id/reset_passkey":               accessTokenScopeRule("user:write"),
	"GET /api/user/2fa/stats":                          accessTokenScopeRule("user:read"),
	"DELETE /api/user/:id/2fa":                         accessTokenScopeRule("user:write"),

	// /api/option
	"GET /api/option/":                          accessTokenScopeRule("option:read"),
	"PUT /api/option/":                          accessTokenScopeRule("option:write"),
	"POST /api/option/payment_compliance":       accessTokenSessionRule,
	"GET /api/option/channel_affinity_cache":    accessTokenScopeRule("option:read"),
	"DELETE /api/option/channel_affinity_cache": accessTokenScopeRule("option:write"),
	"POST /api/option/rest_model_ratio":         accessTokenScopeRule("option:write"),
	"POST /api/option/waffo-pancake/catalog":    accessTokenScopeRule("option:read"),
	"POST /api/option/waffo-pancake/pair":       accessTokenScopeRule("option:write"),
	"POST /api/option/waffo-pancake/save":       accessTokenScopeRule("option:write"),
	"POST /api/custom-oauth-provider/discovery": accessTokenScopeRule("option:write"),
	"GET /api/custom-oauth-provider/":           accessTokenScopeRule("option:read"),
	"GET /api/custom-oauth-provider/:id":        accessTokenScopeRule("option:read"),
	"POST /api/custom-oauth-provider/":          accessTokenScopeRule("option:write"),
	"PUT /api/custom-oauth-provider/:id":        accessTokenScopeRule("option:write"),
	"DELETE /api/custom-oauth-provider/:id":     accessTokenScopeRule("option:write"),
	"GET /api/ratio_sync/channels":              accessTokenScopeRule("option:read"),
	"POST /api/ratio_sync/fetch":                accessTokenScopeRule("option:write"),

	// /api/performance
	"GET /api/performance/stats":         accessTokenScopeRule("ops:read"),
	"DELETE /api/performance/disk_cache": accessTokenScopeRule("ops:write"),
	"POST /api/performance/reset_stats":  accessTokenScopeRule("ops:write"),
	"POST /api/performance/gc":           accessTokenScopeRule("ops:write"),
	"GET /api/performance/logs":          accessTokenScopeRule("ops:read"),

	// /api/plugin/task
	"GET /api/plugin/task":                           accessTokenScopeRule("plugin:read"),
	"POST /api/plugin/task":                          accessTokenScopeRule("plugin:write"),
	"PUT /api/plugin/task":                           accessTokenScopeRule("plugin:write"),
	"GET /api/plugin/task/runtime/status":            accessTokenScopeRule("plugin:read"),
	"GET /api/plugin/task/:key":                      accessTokenScopeRule("plugin:read"),
	"GET /api/plugin/task/:key/icon":                 accessTokenScopeRule("plugin:read"),
	"GET /api/plugin/task/:key/versions":             accessTokenScopeRule("plugin:read"),
	"POST /api/plugin/task/:key/activate":            accessTokenScopeRule("plugin:write"),
	"POST /api/plugin/task/:key/status":              accessTokenScopeRule("plugin:write"),
	"POST /api/plugin/task/:key/dryrun":              accessTokenScopeRule("plugin:write"),
	"DELETE /api/plugin/task/:key/versions/:version": accessTokenScopeRule("plugin:write"),

	// /api/channel (upstream channelPermissionRoutes)
	"GET /api/channel/":                             accessTokenScopeRule("channel:read"),
	"GET /api/channel/search":                       accessTokenScopeRule("channel:read"),
	"GET /api/channel/models":                       accessTokenScopeRule("channel:read"),
	"GET /api/channel/models_enabled":               accessTokenScopeRule("channel:read"),
	"GET /api/channel/:id":                          accessTokenScopeRule("channel:read"),
	"GET /api/channel/test":                         accessTokenScopeRule("channel:operate"),
	"GET /api/channel/test/:id":                     accessTokenScopeRule("channel:operate"),
	"GET /api/channel/update_balance":               accessTokenScopeRule("channel:operate"),
	"GET /api/channel/update_balance/:id":           accessTokenScopeRule("channel:operate"),
	"POST /api/channel/":                            accessTokenScopeRule("channel:sensitive_write"),
	"PUT /api/channel/":                             accessTokenScopeRule("channel:write"),
	"DELETE /api/channel/disabled":                  accessTokenScopeRule("channel:sensitive_write"),
	"POST /api/channel/tag/disabled":                accessTokenScopeRule("channel:operate"),
	"POST /api/channel/tag/enabled":                 accessTokenScopeRule("channel:operate"),
	"PUT /api/channel/tag":                          accessTokenScopeRule("channel:write"),
	"DELETE /api/channel/:id":                       accessTokenScopeRule("channel:sensitive_write"),
	"POST /api/channel/batch":                       accessTokenScopeRule("channel:sensitive_write"),
	"POST /api/channel/fix":                         accessTokenScopeRule("channel:operate"),
	"GET /api/channel/fetch_models/:id":             accessTokenScopeRule("channel:operate"),
	"POST /api/channel/fetch_models":                accessTokenScopeRule("channel:sensitive_write"),
	"POST /api/channel/:id/codex/refresh":           accessTokenScopeRule("channel:sensitive_write"),
	"GET /api/channel/:id/codex/usage":              accessTokenScopeRule("channel:read"),
	"POST /api/channel/ollama/pull":                 accessTokenScopeRule("channel:sensitive_write"),
	"POST /api/channel/ollama/pull/stream":          accessTokenScopeRule("channel:sensitive_write"),
	"DELETE /api/channel/ollama/delete":             accessTokenScopeRule("channel:sensitive_write"),
	"GET /api/channel/ollama/version/:id":           accessTokenScopeRule("channel:sensitive_write"),
	"POST /api/channel/batch/tag":                   accessTokenScopeRule("channel:write"),
	"GET /api/channel/tag/models":                   accessTokenScopeRule("channel:read"),
	"POST /api/channel/copy/:id":                    accessTokenScopeRule("channel:sensitive_write"),
	"POST /api/channel/multi_key/manage":            accessTokenScopeRule("channel:operate"),
	"POST /api/channel/upstream_updates/apply":      accessTokenScopeRule("channel:write"),
	"POST /api/channel/upstream_updates/apply_all":  accessTokenScopeRule("channel:write"),
	"POST /api/channel/upstream_updates/detect":     accessTokenScopeRule("channel:operate"),
	"POST /api/channel/upstream_updates/detect_all": accessTokenScopeRule("channel:operate"),
	"POST /api/channel/:id/key":                     accessTokenScopeRule("channel:secret_view"),
	"POST /api/channel/codex/oauth/start":           accessTokenSessionRule,
	"POST /api/channel/codex/oauth/complete":        accessTokenSessionRule,
	"POST /api/channel/:id/codex/oauth/start":       accessTokenSessionRule,
	"POST /api/channel/:id/codex/oauth/complete":    accessTokenSessionRule,

	// /api/token
	"GET /api/token/":            accessTokenScopeRule("api_key:read"),
	"GET /api/token/search":      accessTokenScopeRule("api_key:read"),
	"GET /api/token/:id":         accessTokenScopeRule("api_key:read"),
	"POST /api/token/:id/key":    accessTokenScopeRule("api_key:reveal"),
	"POST /api/token/":           accessTokenScopeRule("api_key:write"),
	"PUT /api/token/":            accessTokenScopeRule("api_key:write"),
	"DELETE /api/token/:id":      accessTokenScopeRule("api_key:write"),
	"POST /api/token/batch":      accessTokenScopeRule("api_key:write"),
	"POST /api/token/batch/keys": accessTokenScopeRule("api_key:reveal"),

	// /api/redemption
	"GET /api/redemption/":           accessTokenScopeRule("redemption:read"),
	"GET /api/redemption/search":     accessTokenScopeRule("redemption:read"),
	"GET /api/redemption/:id":        accessTokenScopeRule("redemption:read"),
	"POST /api/redemption/":          accessTokenScopeRule("redemption:write"),
	"PUT /api/redemption/":           accessTokenScopeRule("redemption:write"),
	"DELETE /api/redemption/invalid": accessTokenScopeRule("redemption:write"),
	"POST /api/redemption/batch":     accessTokenScopeRule("redemption:write"),
	"DELETE /api/redemption/:id":     accessTokenScopeRule("redemption:write"),

	// /api/audit, /api/log, /api/data
	"GET /api/audit":                            accessTokenScopeRule("audit:read"),
	"GET /api/audit/self":                       accessTokenScopeRule("usage:read"),
	"GET /api/log/":                             accessTokenScopeRule("log:read"),
	"GET /api/log/stat":                         accessTokenScopeRule("log:read"),
	"GET /api/log/self/stat":                    accessTokenScopeRule("usage:read"),
	"GET /api/log/channel_affinity_usage_cache": accessTokenScopeRule("log:read"),
	"GET /api/log/search":                       accessTokenScopeRule("log:read"),
	"GET /api/log/self":                         accessTokenScopeRule("usage:read"),
	"GET /api/log/self/search":                  accessTokenScopeRule("usage:read"),
	"GET /api/log/export":                       accessTokenScopeRule("log:read"),
	"GET /api/log/self/export":                  accessTokenScopeRule("usage:read"),
	"GET /api/data/":                            accessTokenScopeRule("log:read"),
	"GET /api/data/users":                       accessTokenScopeRule("log:read"),
	"GET /api/data/self":                        accessTokenScopeRule("usage:read"),

	// /api/group, /api/prefill_group
	"GET /api/group/":               accessTokenScopeRule("group:read"),
	"GET /api/prefill_group/":       accessTokenScopeRule("group:read"),
	"POST /api/prefill_group/":      accessTokenScopeRule("group:write"),
	"PUT /api/prefill_group/":       accessTokenScopeRule("group:write"),
	"DELETE /api/prefill_group/:id": accessTokenScopeRule("group:write"),

	// /api/mj, /api/task
	"GET /api/mj/self":   accessTokenScopeRule("usage:read"),
	"GET /api/mj/":       accessTokenScopeRule("log:read"),
	"GET /api/task/self": accessTokenScopeRule("usage:read"),
	"GET /api/task/":     accessTokenScopeRule("log:read"),

	// /api/vendors, /api/models (admin)
	"GET /api/vendors/":                     accessTokenScopeRule("model:read"),
	"GET /api/vendors/search":               accessTokenScopeRule("model:read"),
	"GET /api/vendors/:id":                  accessTokenScopeRule("model:read"),
	"POST /api/vendors/":                    accessTokenScopeRule("model:write"),
	"PUT /api/vendors/":                     accessTokenScopeRule("model:write"),
	"DELETE /api/vendors/:id":               accessTokenScopeRule("model:write"),
	"GET /api/models/sync_upstream/preview": accessTokenScopeRule("model:read"),
	"POST /api/models/sync_upstream":        accessTokenScopeRule("model:write"),
	"GET /api/models/missing":               accessTokenScopeRule("model:read"),
	"GET /api/models/":                      accessTokenScopeRule("model:read"),
	"GET /api/models/search":                accessTokenScopeRule("model:read"),
	"GET /api/models/:id":                   accessTokenScopeRule("model:read"),
	"POST /api/models/":                     accessTokenScopeRule("model:write"),
	"PUT /api/models/":                      accessTokenScopeRule("model:write"),
	"DELETE /api/models/:id":                accessTokenScopeRule("model:write"),

	// /api/deployments
	"GET /api/deployments/settings":                     accessTokenScopeRule("deployment:read"),
	"POST /api/deployments/settings/test-connection":    accessTokenScopeRule("deployment:write"),
	"GET /api/deployments/":                             accessTokenScopeRule("deployment:read"),
	"GET /api/deployments/search":                       accessTokenScopeRule("deployment:read"),
	"POST /api/deployments/test-connection":             accessTokenScopeRule("deployment:write"),
	"GET /api/deployments/hardware-types":               accessTokenScopeRule("deployment:read"),
	"GET /api/deployments/locations":                    accessTokenScopeRule("deployment:read"),
	"GET /api/deployments/available-replicas":           accessTokenScopeRule("deployment:read"),
	"POST /api/deployments/price-estimation":            accessTokenScopeRule("deployment:read"),
	"GET /api/deployments/check-name":                   accessTokenScopeRule("deployment:read"),
	"POST /api/deployments/":                            accessTokenScopeRule("deployment:write"),
	"GET /api/deployments/:id":                          accessTokenScopeRule("deployment:read"),
	"GET /api/deployments/:id/logs":                     accessTokenScopeRule("deployment:read"),
	"GET /api/deployments/:id/containers":               accessTokenScopeRule("deployment:read"),
	"GET /api/deployments/:id/containers/:container_id": accessTokenScopeRule("deployment:read"),
	"PUT /api/deployments/:id":                          accessTokenScopeRule("deployment:write"),
	"PUT /api/deployments/:id/name":                     accessTokenScopeRule("deployment:write"),
	"POST /api/deployments/:id/extend":                  accessTokenScopeRule("deployment:write"),
	"DELETE /api/deployments/:id":                       accessTokenScopeRule("deployment:write"),

	// router/relay-router.go
	"POST /pg/chat/completions": accessTokenSessionRule,
}

// accessTokenRouteRules is the merged core + eflow table.
var accessTokenRouteRules = mergeAccessTokenRouteRules(accessTokenCoreRouteRules, accessTokenEflowRouteRules)

func mergeAccessTokenRouteRules(tables ...map[string]accessTokenRouteRule) map[string]accessTokenRouteRule {
	merged := map[string]accessTokenRouteRule{}
	for _, table := range tables {
		for key, rule := range table {
			if _, exists := merged[key]; exists {
				panic(fmt.Sprintf("access token route %s is declared twice", key))
			}
			merged[key] = rule
		}
	}
	return merged
}

// AccessTokenRouteRule returns the rule for "METHOD /full/path".
func AccessTokenRouteRule(key string) (accessTokenRouteRule, bool) {
	rule, ok := accessTokenRouteRules[key]
	return rule, ok
}

// AccessTokenRouteRuleKeys lists every declared route key, sorted.
func AccessTokenRouteRuleKeys() []string {
	keys := slices.Collect(maps.Keys(accessTokenRouteRules))
	slices.Sort(keys)
	return keys
}

// Scope returns the token scope the rule requires, or "" for any/session rules.
func (rule accessTokenRouteRule) Scope() string {
	return rule.scope
}

// Kind names the rule category: "scope", "any" or "session".
func (rule accessTokenRouteRule) Kind() string {
	switch rule.kind {
	case accessTokenRuleScope:
		return "scope"
	case accessTokenRuleAny:
		return "any"
	case accessTokenRuleSession:
		return "session"
	}
	return ""
}
