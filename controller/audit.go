package controller

import (
	"fmt"
	"os"
	"strconv"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/model"
	"github.com/gin-gonic/gin"
)

// auditContentTemplates maps a stable action id to its English fallback text,
// rendered into AuditLog.Content for exports. Placeholders are ${name} and
// come from the action's params. Localized text lives in the frontend.
var auditContentTemplates = map[string]string{
	"user.login":                "Signed in with ${login_method}",
	"user.create":               "Created user ${username} (role ${role})",
	"user.update":               "Updated user ${username} (ID: ${id})",
	"user.delete":               "Deleted user ${username} (ID: ${id})",
	"user.account_delete":       "Account deletion",
	"user.manage":               "Performed ${action} on user ${username} (ID: ${id})",
	"user.binding_clear":        "Cleared ${bindingType} binding for user ${username}",
	"user.2fa_disable":          "Force-disabled two-factor authentication for the user",
	"user.reset_passkey":        "Reset the user passkey",
	"user.passkey_register":     "Registered a passkey",
	"user.passkey_delete":       "Deleted a passkey",
	"user.2fa_setup":            "Started two-factor authentication setup",
	"user.2fa_enable":           "Enabled two-factor authentication",
	"user.2fa_disable_self":     "Disabled two-factor authentication",
	"user.2fa_backup_codes":     "Regenerated two-factor backup codes",
	"user.security_verify":      "Completed security verification",
	"user.password_change":      "Account password change",
	"user.binding_start":        "Account binding request",
	"user.binding_bind":         "Account binding",
	"user.binding_unbind":       "Account unlinking",
	"user.email_binding_resend": "Email confirmation code resend",
	"user.session_revoke":       "Signed out a device",
	"user.session_revoke_other": "Signed out other devices",
	"access_token.generate":     "Generated an access token",
	"access_token.revoke":       "Revoked an access token",
	"access_token.rename":       "Renamed an access token",
	"access_token.update":       "Changed access token permissions",
	"channel.key_view":          "Viewed channel key ${name} (ID: ${id})",
	"redemption.delete_batch":   "Batch deleted ${count} redemption codes",
}

// auditContentEN renders the English fallback for action; unknown actions fall back to the id.
func auditContentEN(action string, params map[string]any) string {
	tmpl, ok := auditContentTemplates[action]
	if !ok {
		return action
	}
	return os.Expand(tmpl, func(key string) string {
		if v, ok := params[key]; ok {
			return fmt.Sprintf("%v", v)
		}
		return ""
	})
}

// auditOperatorInfo describes the acting administrator from the auth context.
func auditOperatorInfo(c *gin.Context) *model.AuditAdminInfo {
	return &model.AuditAdminInfo{
		AdminID:       c.GetInt("id"),
		AdminUsername: c.GetString("username"),
		AdminRole:     c.GetInt("role"),
		AuthMethod:    auditAuthMethod(c),
	}
}

func auditAuthMethod(c *gin.Context) string {
	if c.GetBool("use_access_token") {
		return "access_token"
	}
	return "session"
}

// markAuditLogged tells the auth-chain fallback audit that the handler already recorded this request.
func markAuditLogged(c *gin.Context) {
	common.SetContextKey(c, constant.ContextKeyAuditLogged, true)
}

// recordManageAudit records a management event owned by the acting operator.
func recordManageAudit(c *gin.Context, action string, params map[string]any) {
	recordManageAuditFor(c, c.GetInt("id"), action, params)
}

// recordManageAuditFor records a management event owned by the operator;
// targetUserId only adds the managed user to the structured params.
func recordManageAuditFor(c *gin.Context, targetUserId int, action string, params map[string]any) {
	if params == nil {
		params = map[string]any{}
	}
	operatorUserId := c.GetInt("id")
	if _, ok := params["target_user_id"]; !ok && targetUserId > 0 && targetUserId != operatorUserId {
		params["target_user_id"] = targetUserId
	}
	model.RecordOperationAuditLog(operatorUserId, c.GetInt("role"), auditContentEN(action, params), action, params, auditOperatorInfo(c), nil, c)
	markAuditLogged(c)
}

// recordUserSecurityAudit records a user's own security-sensitive operation.
// It has no administrator operator and does not rely on the AdminAuth fallback.
func recordUserSecurityAudit(c *gin.Context, userId int, action string, params map[string]any) {
	if code := c.GetString("security_error_code"); code != "" {
		if params == nil {
			params = map[string]any{}
		}
		params["code"] = code
	}
	if c.GetBool("use_access_token") {
		if params == nil {
			params = map[string]any{}
		}
		params["token_ref"] = c.GetString("access_token_ref")
	}
	var auditInfo *model.AuditRequestInfo
	if success, ok := params["success"].(bool); ok {
		auditInfo = &model.AuditRequestInfo{
			Method: c.Request.Method, Route: c.FullPath(), Path: c.FullPath(),
			Status: c.Writer.Status(), Success: success,
		}
	}
	model.RecordOperationAuditLog(userId, c.GetInt("role"), auditContentEN(action, params), action, params, nil, auditInfo, c)
}

// recordLoginAudit records a completed sign-in for user.
func recordLoginAudit(c *gin.Context, user *model.User, loginMethod string) {
	if user == nil || user.Id == 0 {
		return
	}
	params := map[string]any{"login_method": loginMethod}
	other := model.AuditOther{LoginMethod: loginMethod}
	if c != nil && c.Request != nil {
		other.UserAgent = c.Request.UserAgent()
	}
	model.RecordLoginLog(user.Id, user.Role, user.Username, auditContentEN("user.login", params), "user.login", params, other, c)
}

// GetAuditLogs serves both GET /api/audit (administrators) and GET /api/audit/self.
// The self route always projects the caller's own events with user-level visibility.
func GetAuditLogs(c *gin.Context) {
	page := common.GetPageQuery(c)
	if page.Page < 1 || page.PageSize < 1 || page.Page > 100000000 {
		common.ApiErrorMsg(c, "Invalid audit pagination")
		return
	}
	filter := model.AuditLogFilter{Username: c.Query("username"), Category: c.Query("category"), TokenRef: c.Query("token_ref"), ExcludeTokenRef: c.Query("exclude_token_ref"), RequestId: c.Query("request_id")}
	viewerRole := c.GetInt("role")
	if c.FullPath() == "/api/audit/self" {
		filter.UserId = c.GetInt("id")
		filter.Username = ""
		filter.SelfView = true
	}
	if !model.ValidAuditCategory(filter.Category) || !model.ValidTokenFingerprint(filter.TokenRef) || !model.ValidTokenFingerprint(filter.ExcludeTokenRef) {
		common.ApiErrorMsg(c, "Invalid audit filters")
		return
	}
	for name, target := range map[string]*int64{"start_timestamp": &filter.StartTimestamp, "end_timestamp": &filter.EndTimestamp} {
		if raw := c.Query(name); raw != "" {
			parsed, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || parsed < 0 {
				common.ApiErrorMsg(c, "Invalid audit time range")
				return
			}
			*target = parsed
		}
	}
	if filter.EndTimestamp > 0 && filter.EndTimestamp < filter.StartTimestamp {
		common.ApiErrorMsg(c, "Invalid audit time range")
		return
	}
	if raw := c.Query("success"); raw != "" {
		if raw != "true" && raw != "false" {
			common.ApiErrorMsg(c, "Invalid audit result")
			return
		}
		success := raw == "true"
		filter.Success = &success
	}
	logs, total, err := model.GetAuditLogs(filter, page.GetStartIdx(), page.GetPageSize(), viewerRole)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	page.SetItems(logs)
	page.SetTotal(int(total))
	common.ApiSuccess(c, page)
}
