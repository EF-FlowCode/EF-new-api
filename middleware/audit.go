package middleware

import (
	"bytes"
	"net/http"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/model"
	"github.com/gin-gonic/gin"
)

// auditResponseMaxBytes bounds how much of a response is kept to decide
// whether the operation succeeded. The captured bytes are never persisted.
const auditResponseMaxBytes = 64 * 1024

// auditResponseWriter records the status and a bounded prefix of the body so
// the fallback audit can read the JSON success flag.
type auditResponseWriter struct {
	gin.ResponseWriter
	body    *bytes.Buffer
	maxSize int
}

func (w *auditResponseWriter) Write(b []byte) (int, error) {
	if remain := w.maxSize - w.body.Len(); remain > 0 {
		if remain >= len(b) {
			w.body.Write(b)
		} else {
			w.body.Write(b[:remain])
		}
	}
	return w.ResponseWriter.Write(b)
}

func (w *auditResponseWriter) WriteString(s string) (int, error) {
	return w.Write([]byte(s))
}

// auditRouteActions maps "METHOD route" to a stable action id for write routes
// that handlers do not audit themselves. Unlisted routes fall back to "generic"
// and the frontend shows "METHOD route".
var auditRouteActions = map[string]string{
	"POST /api/user/":                                  "user.create",
	"PUT /api/user/":                                   "user.update",
	"DELETE /api/user/:id":                             "user.delete",
	"POST /api/user/manage":                            "user.manage",
	"POST /api/user/topup/complete":                    "user.topup_complete",
	"DELETE /api/user/:id/reset_passkey":               "user.reset_passkey",
	"DELETE /api/user/:id/2fa":                         "user.2fa_disable",
	"DELETE /api/user/:id/oauth/bindings/:provider_id": "user.oauth_unbind",
	"DELETE /api/user/:id/bindings/:binding_type":      "user.binding_clear",

	"PUT /api/option/":                          "option.update",
	"POST /api/option/payment_compliance":       "option.payment_compliance",
	"POST /api/option/rest_model_ratio":         "option.reset_ratio",
	"DELETE /api/option/channel_affinity_cache": "option.clear_affinity_cache",

	"POST /api/custom-oauth-provider/":      "custom_oauth.create",
	"PUT /api/custom-oauth-provider/:id":    "custom_oauth.update",
	"DELETE /api/custom-oauth-provider/:id": "custom_oauth.delete",

	"POST /api/channel/":                 "channel.create",
	"PUT /api/channel/":                  "channel.update",
	"DELETE /api/channel/:id":            "channel.delete",
	"POST /api/channel/batch":            "channel.delete_batch",
	"DELETE /api/channel/disabled":       "channel.delete_disabled",
	"POST /api/channel/:id/key":          "channel.key_view",
	"POST /api/channel/copy/:id":         "channel.copy",
	"POST /api/channel/multi_key/manage": "channel.multi_key_manage",

	"POST /api/redemption/":          "redemption.create",
	"PUT /api/redemption/":           "redemption.update",
	"DELETE /api/redemption/:id":     "redemption.delete",
	"DELETE /api/redemption/invalid": "redemption.delete_invalid",
	"POST /api/redemption/batch":     "redemption.delete_batch",

	"POST /api/models/":      "model.create",
	"PUT /api/models/":       "model.update",
	"DELETE /api/models/:id": "model.delete",

	"POST /api/deployments/":      "deployment.create",
	"PUT /api/deployments/:id":    "deployment.update",
	"DELETE /api/deployments/:id": "deployment.delete",

	"POST /api/log/refunds/:id/execute":  "usage_refund.execute",
	"POST /api/invitation_code/":         "invitation_code.create",
	"DELETE /api/invitation_code/:id":    "invitation_code.delete",
	"PUT /api/ticket/admin/:id/status":   "ticket.status_update",
	"POST /api/ticket/admin/:id/message": "ticket.reply",
}

// beginAdminAudit wraps the response writer for staff write requests before the
// handler runs. Read requests return nil and are not audited. It is called from
// authHelper so every staff write route is covered without per-route wiring.
func beginAdminAudit(c *gin.Context) *auditResponseWriter {
	switch c.Request.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
	default:
		return nil
	}
	// Stacked auth middlewares (e.g. AdminAuth + RootAuth) audit once, in the outermost.
	if _, wrapped := c.Writer.(*auditResponseWriter); wrapped {
		return nil
	}
	writer := &auditResponseWriter{
		ResponseWriter: c.Writer,
		body:           bytes.NewBuffer(nil),
		maxSize:        auditResponseMaxBytes,
	}
	c.Writer = writer
	return writer
}

// finishAdminAudit records a fallback operation audit after the handler, unless
// the handler already recorded a more specific event (ContextKeyAuditLogged).
// Only route parameters are stored; request and response bodies never are.
func finishAdminAudit(c *gin.Context, writer *auditResponseWriter) {
	if writer == nil || common.GetContextKeyBool(c, constant.ContextKeyAuditLogged) {
		return
	}
	method := c.Request.Method
	route := c.FullPath()
	status := writer.Status()
	success := auditResponseSuccess(status, writer.body.Bytes())

	action := auditRouteActions[method+" "+route]
	opParams := map[string]any{}
	if action == "" {
		action = "generic"
		opParams["method"] = method
		opParams["route"] = route
	}
	routeParams := map[string]string{}
	for _, p := range c.Params {
		routeParams[p.Key] = p.Value
	}
	adminInfo := &model.AuditAdminInfo{
		AdminID:       c.GetInt("id"),
		AdminUsername: c.GetString("username"),
		AdminRole:     c.GetInt("role"),
		AuthMethod:    auditAuthMethod(c),
	}
	auditInfo := &model.AuditRequestInfo{
		Method:  method,
		Route:   route,
		Path:    route,
		Status:  status,
		Success: success,
		Params:  routeParams,
	}
	model.RecordOperationAuditLog(c.GetInt("id"), c.GetInt("role"), method+" "+route, action, opParams, adminInfo, auditInfo, c)
}

func auditAuthMethod(c *gin.Context) string {
	if c.GetBool("use_access_token") {
		return "access_token"
	}
	return "session"
}

// auditResponseSuccess infers success from the JSON success flag, falling back
// to the HTTP status when the body is not a JSON object or was truncated.
func auditResponseSuccess(status int, body []byte) bool {
	if status >= http.StatusBadRequest {
		return false
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) > 0 && trimmed[0] == '{' {
		var resp struct {
			Success *bool `json:"success"`
		}
		if err := common.Unmarshal(trimmed, &resp); err == nil && resp.Success != nil {
			return *resp.Success
		}
	}
	return true
}

// AuditRouteActionKeys lists the "METHOD route" keys with a dedicated audit action.
func AuditRouteActionKeys() []string {
	keys := make([]string, 0, len(auditRouteActions))
	for key := range auditRouteActions {
		keys = append(keys, key)
	}
	return keys
}
