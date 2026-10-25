package middleware

// accessTokenEflowRouteRules declares the token rule of eflow-only dashboard
// routes (tickets, refunds, risk center, monitoring, invitation codes, task
// plugin runtime, scheduler, and other private endpoints).
var accessTokenEflowRouteRules = map[string]accessTokenRouteRule{
	// self-service extras
	"GET /api/user/self/usage-summary":     accessTokenScopeRule("usage:read"),
	"GET /api/user/self/refund-records":    accessTokenScopeRule("usage:read"),
	"POST /api/user/risk_warning/ack":      accessTokenScopeRule("profile:write"),
	"GET /api/user/invitation_codes":       accessTokenScopeRule("wallet:read"),
	"GET /api/user/invitation_codes/quota": accessTokenScopeRule("wallet:read"),
	"POST /api/user/invitation_codes":      accessTokenScopeRule("wallet:write"),
	"GET /api/user/verification":           accessTokenScopeRule("account_security:write"),
	"POST /api/oauth/email/bind/old_code":  accessTokenScopeRule("account_security:write"),
	"GET /api/log/self/export-async":       accessTokenScopeRule("usage:read"),

	// customer service (role >= support) read views
	"GET /api/user/:id/profile-summary": accessTokenScopeRule("user:read"),
	"GET /api/user/distinct_groups":     accessTokenScopeRule("user:read"),
	"GET /api/user/group_options":       accessTokenScopeRule("user:read"),

	// tickets (self)
	"GET /api/ticket/self":                    accessTokenScopeRule("ticket:read"),
	"GET /api/ticket/self/:id":                accessTokenScopeRule("ticket:read"),
	"GET /api/ticket/invoice/eligible_orders": accessTokenScopeRule("ticket:read"),
	"POST /api/ticket/":                       accessTokenScopeRule("ticket:write"),
	"POST /api/ticket/invoice/":               accessTokenScopeRule("ticket:write"),
	"POST /api/ticket/refund/":                accessTokenScopeRule("ticket:write"),
	"POST /api/ticket/self/:id/message":       accessTokenScopeRule("ticket:write"),
	"PUT /api/ticket/self/:id/close":          accessTokenScopeRule("ticket:write"),
	"POST /api/ticket/attachment":             accessTokenScopeRule("ticket:write"),
	"DELETE /api/ticket/attachment/:id":       accessTokenScopeRule("ticket:write"),

	// tickets (staff)
	"GET /api/ticket/admin/":                   accessTokenScopeRule("ticket_admin:read"),
	"GET /api/ticket/admin/staff":              accessTokenScopeRule("ticket_admin:read"),
	"GET /api/ticket/admin/:id":                accessTokenScopeRule("ticket_admin:read"),
	"GET /api/ticket/admin/:id/invoice":        accessTokenScopeRule("ticket_admin:read"),
	"GET /api/ticket/admin/:id/refund":         accessTokenScopeRule("ticket_admin:read"),
	"GET /api/ticket/admin/:id/user-profile":   accessTokenScopeRule("ticket_admin:read"),
	"POST /api/ticket/admin/invoice-export":    accessTokenScopeRule("ticket_admin:read"),
	"POST /api/ticket/admin/:id/message":       accessTokenScopeRule("ticket_admin:write"),
	"PUT /api/ticket/admin/:id/status":         accessTokenScopeRule("ticket_admin:write"),
	"PUT /api/ticket/admin/batch-status":       accessTokenScopeRule("ticket_admin:write"),
	"PUT /api/ticket/admin/:id/assign":         accessTokenScopeRule("ticket_admin:write"),
	"PUT /api/ticket/admin/:id/invoice/status": accessTokenScopeRule("ticket_admin:write"),
	"PUT /api/ticket/admin/:id/refund/status":  accessTokenScopeRule("ticket_admin:write"),

	// channel scheduler (admin-api reads it)
	"GET /api/channel/:id/scheduler/state": accessTokenScopeRule("channel:read"),

	// invitation codes (admin)
	"GET /api/invitation_code/":           accessTokenScopeRule("invitation_code:read"),
	"GET /api/invitation_code/search":     accessTokenScopeRule("invitation_code:read"),
	"GET /api/invitation_code/:id":        accessTokenScopeRule("invitation_code:read"),
	"GET /api/invitation_code/:id/usages": accessTokenScopeRule("invitation_code:read"),
	"POST /api/invitation_code/":          accessTokenScopeRule("invitation_code:write"),
	"PUT /api/invitation_code/":           accessTokenScopeRule("invitation_code:write"),
	"DELETE /api/invitation_code/invalid": accessTokenScopeRule("invitation_code:write"),
	"DELETE /api/invitation_code/:id":     accessTokenScopeRule("invitation_code:write"),

	// risk center
	"GET /api/risk/config":                               accessTokenScopeRule("risk:read"),
	"PUT /api/risk/config":                               accessTokenScopeRule("risk:write"),
	"GET /api/risk/detect-ip":                            accessTokenScopeRule("risk:read"),
	"GET /api/risk/groups":                               accessTokenScopeRule("risk:read"),
	"GET /api/risk/overview":                             accessTokenScopeRule("risk:read"),
	"GET /api/risk/incidents":                            accessTokenScopeRule("risk:read"),
	"GET /api/risk/subjects":                             accessTokenScopeRule("risk:read"),
	"POST /api/risk/subjects/:scope/:id/unblock":         accessTokenScopeRule("risk:write"),
	"GET /api/risk/rules":                                accessTokenScopeRule("risk:read"),
	"POST /api/risk/rules":                               accessTokenScopeRule("risk:write"),
	"PUT /api/risk/rules/:id":                            accessTokenScopeRule("risk:write"),
	"DELETE /api/risk/rules/:id":                         accessTokenScopeRule("risk:write"),
	"GET /api/risk/moderation/config":                    accessTokenScopeRule("risk:read"),
	"PUT /api/risk/moderation/config":                    accessTokenScopeRule("risk:write"),
	"GET /api/risk/moderation/categories":                accessTokenScopeRule("risk:read"),
	"GET /api/risk/moderation/overview":                  accessTokenScopeRule("risk:read"),
	"GET /api/risk/moderation/queue_stats":               accessTokenScopeRule("risk:read"),
	"GET /api/risk/moderation/incidents":                 accessTokenScopeRule("risk:read"),
	"GET /api/risk/moderation/incidents/:id":             accessTokenScopeRule("risk:read"),
	"GET /api/risk/moderation/rules":                     accessTokenScopeRule("risk:read"),
	"POST /api/risk/moderation/rules":                    accessTokenScopeRule("risk:write"),
	"PUT /api/risk/moderation/rules/:id":                 accessTokenScopeRule("risk:write"),
	"DELETE /api/risk/moderation/rules/:id":              accessTokenScopeRule("risk:write"),
	"POST /api/risk/moderation/debug":                    accessTokenScopeRule("risk:write"),
	"GET /api/risk/moderation/debug/:id":                 accessTokenScopeRule("risk:read"),
	"GET /api/risk/enforcement/config":                   accessTokenScopeRule("risk:read"),
	"PUT /api/risk/enforcement/config":                   accessTokenScopeRule("risk:write"),
	"GET /api/risk/enforcement/overview":                 accessTokenScopeRule("risk:read"),
	"GET /api/risk/enforcement/incidents":                accessTokenScopeRule("risk:read"),
	"GET /api/risk/enforcement/counters":                 accessTokenScopeRule("risk:read"),
	"POST /api/risk/enforcement/users/:id/unban":         accessTokenScopeRule("risk:write"),
	"POST /api/risk/enforcement/users/:id/reset_counter": accessTokenScopeRule("risk:write"),
	"POST /api/risk/enforcement/test_email":              accessTokenScopeRule("risk:write"),

	// group monitoring (admin)
	"GET /api/monitoring/admin/groups":                   accessTokenScopeRule("monitoring:read"),
	"GET /api/monitoring/admin/groups/:group":            accessTokenScopeRule("monitoring:read"),
	"GET /api/monitoring/admin/groups/:group/history":    accessTokenScopeRule("monitoring:read"),
	"GET /api/monitoring/admin/models":                   accessTokenScopeRule("monitoring:read"),
	"POST /api/monitoring/admin/refresh":                 accessTokenScopeRule("monitoring:write"),
	"DELETE /api/monitoring/admin/groups/:group/records": accessTokenScopeRule("monitoring:write"),

	// options (root) private to eflow
	"PUT /api/option/bulk":                     accessTokenScopeRule("option:write"),
	"GET /api/option/email_templates":          accessTokenScopeRule("option:read"),
	"POST /api/option/email_templates/preview": accessTokenScopeRule("option:read"),
	"POST /api/option/email_templates/reset":   accessTokenScopeRule("option:write"),
	"POST /api/option/test_email":              accessTokenScopeRule("option:write"),
	"POST /api/option/migrate_console_setting": accessTokenScopeRule("option:write"),
	"GET /api/option/task_pricing_migration":   accessTokenScopeRule("option:read"),
	"POST /api/option/task_pricing_migration":  accessTokenScopeRule("option:write"),
	"GET /api/option/task_pricing_usage":       accessTokenScopeRule("option:read"),

	// logs (root)
	"GET /api/log/export-async": accessTokenScopeRule("log:read"),

	// usage refunds (root)
	"POST /api/log/refunds/preview":     accessTokenScopeRule("usage_refund:read"),
	"POST /api/log/refunds/candidates":  accessTokenScopeRule("usage_refund:read"),
	"POST /api/log/refunds/anomalies":   accessTokenScopeRule("usage_refund:read"),
	"GET /api/log/refunds/":             accessTokenScopeRule("usage_refund:read"),
	"GET /api/log/refunds/:id":          accessTokenScopeRule("usage_refund:read"),
	"POST /api/log/refunds/:id/execute": accessTokenScopeRule("usage_refund:write"),

	// task plugin runtime (root)
	"GET /api/plugin/task/runtime/shadow/bindings":                    accessTokenScopeRule("plugin:read"),
	"PUT /api/plugin/task/runtime/shadow/bindings/:channel_id":        accessTokenScopeRule("plugin:write"),
	"DELETE /api/plugin/task/runtime/shadow/bindings/:channel_id":     accessTokenScopeRule("plugin:write"),
	"GET /api/plugin/task/runtime/shadow/reports":                     accessTokenScopeRule("plugin:read"),
	"GET /api/plugin/task/runtime/production/bindings":                accessTokenScopeRule("plugin:read"),
	"PUT /api/plugin/task/runtime/production/bindings/:channel_id":    accessTokenScopeRule("plugin:write"),
	"DELETE /api/plugin/task/runtime/production/bindings/:channel_id": accessTokenScopeRule("plugin:write"),
	"GET /api/plugin/task/runtime/settings":                           accessTokenScopeRule("plugin:read"),
	"PUT /api/plugin/task/runtime/settings":                           accessTokenScopeRule("plugin:write"),
	"POST /api/plugin/task/runtime/tasks/:task_id/adopt":              accessTokenScopeRule("plugin:write"),
	"GET /api/plugin/task/runtime/reviews":                            accessTokenScopeRule("plugin:read"),
	"GET /api/plugin/task/runtime/tasks/:task_id/review":              accessTokenScopeRule("plugin:read"),
	"POST /api/plugin/task/runtime/tasks/:task_id/review/preview":     accessTokenScopeRule("plugin:read"),
	"POST /api/plugin/task/runtime/reviews/:operation/confirm":        accessTokenScopeRule("plugin:write"),
}
