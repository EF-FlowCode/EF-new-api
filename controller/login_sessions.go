package controller

import (
	"net/http"
	"sort"

	"eflow.local/new-api/common"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

// eflow: 登录设备管理。会话存在 Redis（session:<sid>，用户集合 session:user:<uid>），
// 返回结构对齐上游 LoginSessionView；对外只给 sid 的 HMAC，原始 sid 等同凭证，从不返回。
// 只允许浏览器会话调用（路由规则表为 session-only）。

const (
	loginMethodSessionKey = "login_method"
	loginIPSessionKey     = "login_ip"
	loginUASessionKey     = "login_ua"
	maxLoginUserAgentLen  = 255
)

type loginSessionView struct {
	SID          string `json:"sid"`
	Current      bool   `json:"current"`
	LoginMethod  string `json:"login_method"`
	IP           string `json:"ip"`
	UserAgent    string `json:"user_agent"`
	CreatedAt    int64  `json:"created_at"`
	LastActiveAt int64  `json:"last_active_at"`
	LastActiveIP string `json:"last_active_ip"`
}

// recordLoginMetadata stores what the device list shows about a new sign-in.
func recordLoginMetadata(session sessions.Session, c *gin.Context, loginMethod string) {
	ua := []rune(c.Request.UserAgent())
	if len(ua) > maxLoginUserAgentLen {
		ua = ua[:maxLoginUserAgentLen]
	}
	session.Set(loginMethodSessionKey, loginMethod)
	session.Set(loginIPSessionKey, c.ClientIP())
	session.Set(loginUASessionKey, string(ua))
}

func currentSessionID(c *gin.Context) string {
	if _, ok := c.Get(sessions.DefaultKey); !ok {
		return ""
	}
	return common.CurrentSessionID(sessions.Default(c))
}

func GetLoginSessions(c *gin.Context) {
	if _, ok := requireBrowserSession(c); !ok {
		return
	}
	records, err := common.ListUserSessions(c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	current := currentSessionID(c)
	items := make([]loginSessionView, 0, len(records)+1)
	seenCurrent := false
	for _, record := range records {
		view := loginSessionView{SID: common.SessionPublicID(record.ID), Current: record.ID == current}
		view.LoginMethod, _ = record.Values[loginMethodSessionKey].(string)
		view.IP, _ = record.Values[loginIPSessionKey].(string)
		view.UserAgent, _ = record.Values[loginUASessionKey].(string)
		view.CreatedAt, _ = record.Values[LoginAtSessionKey].(int64)
		view.LastActiveAt, view.LastActiveIP = record.LastActiveAt, record.LastActiveIP
		seenCurrent = seenCurrent || view.Current
		items = append(items, view)
	}
	if !seenCurrent {
		// Cookie-store deployments (no Redis) can only describe the current session.
		session := sessions.Default(c)
		view := loginSessionView{SID: common.SessionPublicID(current), Current: true}
		view.LoginMethod, _ = session.Get(loginMethodSessionKey).(string)
		view.IP, _ = session.Get(loginIPSessionKey).(string)
		view.UserAgent, _ = session.Get(loginUASessionKey).(string)
		view.CreatedAt, _ = session.Get(LoginAtSessionKey).(int64)
		items = append(items, view)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Current != items[j].Current {
			return items[i].Current
		}
		return items[i].CreatedAt > items[j].CreatedAt
	})
	common.ApiSuccess(c, gin.H{"items": items})
}

func DeleteLoginSession(c *gin.Context) {
	if _, ok := requireBrowserSession(c); !ok {
		return
	}
	userID := c.GetInt("id")
	target := c.Param("sid")
	records, err := common.ListUserSessions(userID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	current := currentSessionID(c)
	for _, record := range records {
		if common.SessionPublicID(record.ID) != target {
			continue
		}
		if _, err := common.RevokeUserSession(userID, record.ID); err != nil {
			common.ApiError(c, err)
			return
		}
		recordUserSecurityAudit(c, userID, "user.session_revoke", map[string]any{"session": target, "current": record.ID == current})
		if record.ID == current {
			clearSignedInCookies(c)
		}
		common.ApiSuccess(c, nil)
		return
	}
	writeSecurityStatusError(c, http.StatusNotFound, "SESSION_NOT_FOUND", "This sign-in session no longer exists.")
}

func RevokeOtherLoginSessions(c *gin.Context) {
	if _, ok := requireBrowserSession(c); !ok {
		return
	}
	userID := c.GetInt("id")
	revokeUserSessions(c, userID, true)
	recordUserSecurityAudit(c, userID, "user.session_revoke_other", nil)
	common.ApiSuccess(c, nil)
}

// clearSignedInCookies signs the current browser out after its own session was revoked.
func clearSignedInCookies(c *gin.Context) {
	session := sessions.Default(c)
	session.Clear()
	_ = session.Save()
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(common.EflowAuthCookieName, "", -1, "/", "", isHTTPSRequest(c), true)
}
