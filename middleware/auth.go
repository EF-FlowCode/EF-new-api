package middleware

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/i18n"
	"eflow.local/new-api/logger"
	"eflow.local/new-api/model"
	"eflow.local/new-api/pkg/requestip"
	"eflow.local/new-api/service"
	"eflow.local/new-api/setting/ratio_setting"
	"eflow.local/new-api/types"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func validUserInfo(username string, role int) bool {
	// check username is empty
	if strings.TrimSpace(username) == "" {
		return false
	}
	if !common.IsValidateRole(role) {
		return false
	}
	return true
}

// rejectUserAuth preserves the reason across gateways without logging credentials.
func rejectUserAuth(c *gin.Context, status int, code, messageKey string) {
	c.Header("Cache-Control", "no-store")
	logger.LogWarn(c, fmt.Sprintf("authentication rejected code=%s instance=%s path=%s", code, common.NodeName, c.Request.URL.Path))
	c.JSON(status, gin.H{"success": false, "code": code, "message": common.TranslateMessage(c, messageKey)})
	c.Abort()
}

func authHelper(c *gin.Context, minRole int) {
	session := sessions.Default(c)
	id := session.Get("id")
	useAccessToken := false
	var tokenLookup *accessTokenLookup
	if id == nil {
		// Check access token: a scoped nap_ token or a legacy system access token.
		accessToken := c.Request.Header.Get("Authorization")
		if accessToken == "" {
			// eflow: 主会话已失效（登出、改密、被踢）时一并清掉子站身份 cookie，子站随之视为未登录。
			if _, err := c.Cookie(common.EflowAuthCookieName); err == nil {
				c.SetSameSite(http.SameSiteStrictMode)
				c.SetCookie(common.EflowAuthCookieName, "", -1, "/", "", c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https"), true)
			}
			rejectUserAuth(c, http.StatusUnauthorized, "session_missing_or_invalid", i18n.MsgAuthNotLoggedIn)
			return
		}
		lookup, authErr := lookupAccessToken(accessToken, time.Now().Unix())
		if authErr != nil {
			if tokenErr, ok := asAccessTokenAuthError(authErr); ok {
				rejectAccessToken(c, tokenErr)
			} else if errors.Is(authErr, model.ErrDatabase) {
				common.SysLog("ValidateAccessToken database error: " + authErr.Error())
				rejectUserAuth(c, http.StatusServiceUnavailable, "auth_dependency_unavailable", i18n.MsgDatabaseError)
			} else {
				rejectUserAuth(c, http.StatusUnauthorized, "access_token_invalid", i18n.MsgAuthAccessTokenInvalid)
			}
			return
		}
		if !validUserInfo(lookup.user.Username, lookup.user.Role) {
			rejectUserAuth(c, http.StatusUnauthorized, "identity_invalid", i18n.MsgAuthUserInfoInvalid)
			return
		}
		id = lookup.user.Id
		useAccessToken = true
		tokenLookup = lookup
	}
	// get header New-Api-User. Scoped access tokens identify their owner, so the
	// header is optional for them; when present it must still match.
	apiUserIdStr := c.Request.Header.Get("New-Api-User")
	idInt, ok := id.(int)
	if !ok {
		rejectUserAuth(c, http.StatusUnauthorized, "user_id_mismatch", i18n.MsgAuthUserIdMismatch)
		return
	}
	if apiUserIdStr == "" {
		if tokenLookup == nil || tokenLookup.legacy {
			rejectUserAuth(c, http.StatusUnauthorized, "user_id_required", i18n.MsgAuthUserIdNotProvided)
			return
		}
	} else {
		apiUserId, err := strconv.Atoi(apiUserIdStr)
		if err != nil {
			rejectUserAuth(c, http.StatusUnauthorized, "user_id_invalid", i18n.MsgAuthUserIdFormatError)
			return
		}
		if idInt != apiUserId {
			rejectUserAuth(c, http.StatusUnauthorized, "user_id_mismatch", i18n.MsgAuthUserIdMismatch)
			return
		}
	}
	userCache, err := model.GetUserCache(idInt)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			rejectUserAuth(c, http.StatusUnauthorized, "identity_invalid", i18n.MsgAuthUserInfoInvalid)
		} else {
			rejectUserAuth(c, http.StatusServiceUnavailable, "auth_dependency_unavailable", i18n.MsgDatabaseError)
		}
		return
	}
	if userCache.Status != common.UserStatusEnabled {
		rejectUserAuth(c, http.StatusForbidden, "account_disabled", i18n.MsgAuthUserBanned)
		return
	}
	if userCache.Role < minRole {
		rejectUserAuth(c, http.StatusForbidden, "permission_denied", i18n.MsgAuthInsufficientPrivilege)
		return
	}
	if !validUserInfo(userCache.Username, userCache.Role) {
		rejectUserAuth(c, http.StatusUnauthorized, "identity_invalid", i18n.MsgAuthUserInfoInvalid)
		return
	}
	if tokenLookup == nil {
		// eflow: 登录设备列表的最近活跃时间，每会话每分钟最多写一次。
		common.TouchSession(common.CurrentSessionID(session), c.ClientIP(), 30*24*time.Hour)
	}
	if tokenLookup != nil {
		tokenLookup.user.Role = userCache.Role
		if !enforceAccessTokenRoute(c, tokenLookup, userCache.Role) {
			return
		}
		setAccessTokenContext(c, tokenLookup)
	}
	// 防止不同newapi版本冲突，导致数据不通用
	c.Header("Auth-Version", "864b7076dbcd0a3c01b5520316720ebf")
	c.Set("username", userCache.Username)
	c.Set("role", userCache.Role)
	c.Set("id", idInt)
	c.Set("group", userCache.Group)
	c.Set("user_group", userCache.Group)
	c.Set("use_access_token", useAccessToken)

	// eflow: 客服及以上角色的写操作统一在鉴权链路兜底留审计，新增接口无需单独挂中间件；
	// handler 自行记录了更细的审计时（ContextKeyAuditLogged）跳过。访问令牌请求另记一条
	// access_token 审计（写操作已由兜底审计记录时跳过，读操作按令牌和路由限频）。
	var auditWriter *auditResponseWriter
	if minRole >= common.RoleCustomerServiceUser {
		auditWriter = beginAdminAudit(c)
	}
	c.Next()
	finishAdminAudit(c, auditWriter)
	if tokenLookup != nil {
		finishAccessTokenAudit(c, tokenLookup)
	}
}

func TryUserAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		id, ok := session.Get("id").(int)
		if ok {
			if user, err := model.GetUserCache(id); err == nil && user.Status == common.UserStatusEnabled {
				c.Set("id", id)
			}
		}
		c.Next()
	}
}

func UserAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		authHelper(c, common.RoleCommonUser)
	}
}

// SessionAuth 仅基于 cookie session 鉴权，不要求 New-Api-User 请求头。
// 用于浏览器直发、无法注入自定义头的场景（如 <img src>、<a href> 下载链接）。
// 与 UserAuth 的区别：跳过 CSRF 防护层的 New-Api-User 校验；因此应只用于幂等、只读的 GET 资源。
func SessionAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		id, ok := session.Get("id").(int)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": common.TranslateMessage(c, i18n.MsgAuthNotLoggedIn),
			})
			c.Abort()
			return
		}
		user, err := model.GetUserCache(id)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": common.TranslateMessage(c, i18n.MsgAuthUserInfoInvalid),
			})
			c.Abort()
			return
		}
		if user.Status != common.UserStatusEnabled {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": common.TranslateMessage(c, i18n.MsgAuthUserBanned),
			})
			c.Abort()
			return
		}
		if user.Role < common.RoleCommonUser {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": common.TranslateMessage(c, i18n.MsgAuthInsufficientPrivilege),
			})
			c.Abort()
			return
		}
		c.Set("id", id)
		c.Set("role", user.Role)
		c.Set("username", user.Username)
		c.Set("group", user.Group)
		c.Set("user_group", user.Group)
		c.Next()
	}
}

func AdminAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		authHelper(c, common.RoleAdminUser)
	}
}

// TicketStaffAuth 允许角色 >= 客服（RoleCustomerServiceUser）访问。
// 用于工单后台接口：既覆盖原有管理员（role=10+），也放行专门的客服账号（role=5）。
// 是否允许具体用户处理某张工单，由 controller / service 根据分配规则再做一次细粒度判断。
func TicketStaffAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		authHelper(c, common.RoleCustomerServiceUser)
	}
}

func RootAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		authHelper(c, common.RoleRootUser)
	}
}

// TokenOrUserAuth allows either session-based user auth or API token auth.
// Used for endpoints that need to be accessible from both the dashboard and API clients.
func TokenOrUserAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		// Try session auth first (dashboard users)
		session := sessions.Default(c)
		if id, ok := session.Get("id").(int); ok {
			if user, err := model.GetUserCache(id); err == nil && user.Status == common.UserStatusEnabled {
				c.Set("id", id)
				c.Next()
				return
			}
		}
		// Fall back to token auth (API clients)
		TokenAuth()(c)
	}
}

// TokenAuthReadOnly 宽松版本的令牌认证中间件，用于只读查询接口。
// 只验证令牌 key 是否存在，不检查令牌状态、过期时间和额度。
// 即使令牌已过期、已耗尽或已禁用，也允许访问。
// 仍然检查用户是否被封禁。
func TokenAuthReadOnly() func(c *gin.Context) {
	return func(c *gin.Context) {
		key := c.Request.Header.Get("Authorization")
		if key == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": common.TranslateMessage(c, i18n.MsgTokenNotProvided),
			})
			c.Abort()
			return
		}
		if strings.HasPrefix(key, "Bearer ") || strings.HasPrefix(key, "bearer ") {
			key = strings.TrimSpace(key[7:])
		}
		key = strings.TrimPrefix(key, "sk-")
		parts := strings.Split(key, "-")
		key = parts[0]

		token, err := model.GetTokenByKey(key, false)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				c.JSON(http.StatusUnauthorized, gin.H{
					"success": false,
					"message": common.TranslateMessage(c, i18n.MsgTokenInvalid),
				})
			} else {
				common.SysLog("TokenAuthReadOnly GetTokenByKey database error: " + err.Error())
				c.JSON(http.StatusInternalServerError, gin.H{
					"success": false,
					"message": common.TranslateMessage(c, i18n.MsgDatabaseError),
				})
			}
			c.Abort()
			return
		}

		// TokenAuthReadOnly must keep allowing other token states to query read-only
		// data, such as token usage logs; only explicitly disabled tokens are denied.
		if token.Status == common.TokenStatusDisabled {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": common.TranslateMessage(c, i18n.MsgTokenStatusUnavailable),
			})
			c.Abort()
			return
		}
		// eflow: 只读接口同样受令牌 IP 白名单约束，泄露的 key 不能从任意 IP 读取用量和日志（上游 7de10f2c6）。
		if err := checkTokenIpLimits(c, token); err != nil {
			c.JSON(http.StatusForbidden, gin.H{
				"success": false,
				"message": err.Error(),
			})
			c.Abort()
			return
		}

		userCache, err := model.GetUserCache(token.UserId)
		if err != nil {
			common.SysLog(fmt.Sprintf("TokenAuthReadOnly GetUserCache error for user %d: %v", token.UserId, err))
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"message": common.TranslateMessage(c, i18n.MsgDatabaseError),
			})
			c.Abort()
			return
		}
		if userCache.Status != common.UserStatusEnabled {
			c.JSON(http.StatusForbidden, gin.H{
				"success": false,
				"message": common.TranslateMessage(c, i18n.MsgAuthUserBanned),
			})
			c.Abort()
			return
		}

		c.Set("id", token.UserId)
		c.Set("token_id", token.Id)
		c.Set("token_key", token.Key)
		c.Next()
	}
}

var (
	errTokenIpUnparsable = errors.New("无法解析客户端 IP 地址")
	errTokenIpNotAllowed = errors.New("您的 IP 不在令牌允许访问的列表中")
)

// checkTokenIpLimits 校验令牌的 IP 白名单；未配置白名单时放行。
func checkTokenIpLimits(c *gin.Context, token *model.Token) error {
	allowIps := token.GetIpLimits()
	if len(allowIps) == 0 {
		return nil
	}
	clientIp := requestip.GetClientIP(c)
	logger.LogDebug(c, "Token has IP restrictions, checking client IP %s", clientIp)
	ip := net.ParseIP(clientIp)
	if ip == nil {
		return errTokenIpUnparsable
	}
	if !common.IsIpInCIDRList(ip, allowIps) {
		return errTokenIpNotAllowed
	}
	logger.LogDebug(c, "Client IP %s passed the token IP restrictions check", clientIp)
	return nil
}

func TokenAuth() func(c *gin.Context) {
	return tokenAuth(false)
}

func KnowledgeTaskAccessAuth() func(c *gin.Context) {
	return tokenAuth(true)
}

func apiKeyFromWebSocketSubprotocol(protocols string) (string, bool) {
	const insecureAPIKeyPrefix = "openai-insecure-api-key."
	for part := range strings.SplitSeq(protocols, ",") {
		part = strings.TrimSpace(part)
		if after, ok := strings.CutPrefix(part, insecureAPIKeyPrefix); ok {
			key := after
			return key, key != ""
		}
	}
	return "", false
}

func tokenAuth(allowExhausted bool) func(c *gin.Context) {
	return func(c *gin.Context) {
		// Browsers cannot set Authorization on WebSocket upgrades, so realtime
		// clients may send the key as a subprotocol; other subprotocols (for
		// example plain "realtime") keep the Authorization header untouched.
		if key, ok := apiKeyFromWebSocketSubprotocol(strings.Join(c.Request.Header.Values("Sec-WebSocket-Protocol"), ",")); ok {
			c.Request.Header.Set("Authorization", "Bearer "+key)
		}
		// 检查path包含/v1/messages 或 /v1/models
		if strings.Contains(c.Request.URL.Path, "/v1/messages") || strings.Contains(c.Request.URL.Path, "/v1/models") {
			anthropicKey := c.Request.Header.Get("x-api-key")
			if anthropicKey != "" {
				c.Request.Header.Set("Authorization", "Bearer "+anthropicKey)
			}
		}
		// gemini api 从query中获取key
		if c.Request.URL.Path == "/v1/models" ||
			strings.HasPrefix(c.Request.URL.Path, "/v1beta/models") ||
			strings.HasPrefix(c.Request.URL.Path, "/v1beta/openai/models") ||
			strings.HasPrefix(c.Request.URL.Path, "/v1/models/") {
			skKey := c.Query("key")
			if skKey != "" {
				c.Request.Header.Set("Authorization", "Bearer "+skKey)
			}
			// 从x-goog-api-key header中获取key
			xGoogKey := c.Request.Header.Get("x-goog-api-key")
			if xGoogKey != "" {
				c.Request.Header.Set("Authorization", "Bearer "+xGoogKey)
			}
		}
		key := c.Request.Header.Get("Authorization")
		parts := make([]string, 0)
		if strings.HasPrefix(key, "Bearer ") || strings.HasPrefix(key, "bearer ") {
			key = strings.TrimSpace(key[7:])
		}
		if key == "" || key == "midjourney-proxy" {
			key = c.Request.Header.Get("mj-api-secret")
			if strings.HasPrefix(key, "Bearer ") || strings.HasPrefix(key, "bearer ") {
				key = strings.TrimSpace(key[7:])
			}
			key = strings.TrimPrefix(key, "sk-")
			parts = strings.Split(key, "-")
			key = parts[0]
		} else {
			key = strings.TrimPrefix(key, "sk-")
			parts = strings.Split(key, "-")
			key = parts[0]
		}
		validate := model.ValidateUserToken
		if allowExhausted {
			validate = model.ValidateTaskAccessToken
		}
		token, err := validate(key)
		if token != nil {
			id := c.GetInt("id")
			if id == 0 {
				c.Set("id", token.UserId)
			}
		}
		if err != nil {
			if errors.Is(err, model.ErrDatabase) {
				common.SysLog("TokenAuth ValidateUserToken database error: " + err.Error())
				abortWithOpenAiMessage(c, http.StatusInternalServerError,
					common.TranslateMessage(c, i18n.MsgDatabaseError))
			} else {
				abortWithOpenAiMessage(c, http.StatusUnauthorized,
					common.TranslateMessage(c, i18n.MsgTokenInvalid))
			}
			return
		}

		if err := checkTokenIpLimits(c, token); err != nil {
			if errors.Is(err, errTokenIpUnparsable) {
				abortWithOpenAiMessage(c, http.StatusForbidden, err.Error())
			} else {
				abortWithOpenAiMessage(c, http.StatusForbidden, err.Error(), types.ErrorCodeAccessDenied)
			}
			return
		}

		userCache, err := model.GetUserCache(token.UserId)
		if err != nil {
			common.SysLog(fmt.Sprintf("TokenAuth GetUserCache error for user %d: %v", token.UserId, err))
			abortWithOpenAiMessage(c, http.StatusInternalServerError,
				common.TranslateMessage(c, i18n.MsgDatabaseError))
			return
		}
		userEnabled := userCache.Status == common.UserStatusEnabled
		if !userEnabled {
			abortWithOpenAiMessage(c, http.StatusForbidden, common.TranslateMessage(c, i18n.MsgAuthUserBanned))
			return
		}

		userCache.WriteContext(c)

		userGroup := userCache.Group
		tokenGroup := token.Group
		usableGroups := service.GetUserEffectiveGroups(userGroup, userCache.AdditionalCustomerTiers.Values())
		if tokenGroup != "" {
			if _, ok := usableGroups[tokenGroup]; !ok {
				abortWithOpenAiMessage(c, http.StatusForbidden, fmt.Sprintf("无权访问 %s 分组", tokenGroup))
				return
			}
			// check group in common.GroupRatio
			if !ratio_setting.ContainsGroupRatio(tokenGroup) {
				if tokenGroup != "auto" {
					abortWithOpenAiMessage(c, http.StatusForbidden, fmt.Sprintf("分组 %s 已被弃用", tokenGroup))
					return
				}
			}
			userGroup = tokenGroup
		} else if _, ok := usableGroups[userGroup]; !ok {
			abortWithOpenAiMessage(c, http.StatusForbidden, fmt.Sprintf("主分组 %s 未被管理员授权，请为令牌选择可用分组", userGroup))
			return
		}
		common.SetContextKey(c, constant.ContextKeyUsingGroup, userGroup)

		err = SetupContextForToken(c, token, parts...)
		if err != nil {
			return
		}
		c.Next()
	}
}

func SetupContextForToken(c *gin.Context, token *model.Token, parts ...string) error {
	if token == nil {
		return fmt.Errorf("token is nil")
	}
	c.Set("id", token.UserId)
	c.Set("token_id", token.Id)
	c.Set("token_key", token.Key)
	c.Set("token_name", token.Name)
	c.Set("token_unlimited_quota", token.UnlimitedQuota)
	if !token.UnlimitedQuota {
		c.Set("token_quota", token.RemainQuota)
	}
	if token.ModelLimitsEnabled {
		c.Set("token_model_limit_enabled", true)
		c.Set("token_model_limit", token.GetModelLimitsMap())
	} else {
		c.Set("token_model_limit_enabled", false)
	}
	common.SetContextKey(c, constant.ContextKeyTokenGroup, token.Group)
	common.SetContextKey(c, constant.ContextKeyTokenCrossGroupRetry, token.CrossGroupRetry)
	if len(parts) > 1 {
		if model.IsAdmin(token.UserId) {
			c.Set("specific_channel_id", parts[1])
		} else {
			c.Header("specific_channel_version", "701e3ae1dc3f7975556d354e0675168d004891c8")
			abortWithOpenAiMessage(c, http.StatusForbidden, "普通用户不支持指定渠道")
			return fmt.Errorf("普通用户不支持指定渠道")
		}
	}
	return nil
}
