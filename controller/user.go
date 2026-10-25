package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"eflow.local/new-api/common"
	"eflow.local/new-api/dto"
	"eflow.local/new-api/i18n"
	"eflow.local/new-api/logger"
	"eflow.local/new-api/middleware"
	"eflow.local/new-api/model"
	"eflow.local/new-api/service"
	"eflow.local/new-api/setting"
	"eflow.local/new-api/setting/operation_setting"

	"eflow.local/new-api/constant"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

var (
	errUserPasswordUnset    = errors.New("user password is not set")
	errPasswordUnchanged    = errors.New("New password must be different from current password")
	errOriginalPasswordFail = errors.New("original password is incorrect")
)

func Login(c *gin.Context) {
	if !common.GetAuthSettings().PasswordLoginEnabled {
		common.ApiErrorI18n(c, i18n.MsgUserPasswordLoginDisabled)
		return
	}
	var loginRequest LoginRequest
	err := json.NewDecoder(c.Request.Body).Decode(&loginRequest)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	username := loginRequest.Username
	password := loginRequest.Password
	if username == "" || password == "" {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	user := model.User{
		Username: username,
		Password: password,
	}
	err = user.ValidateAndFill()
	if err != nil {
		switch {
		case errors.Is(err, model.ErrDatabase):
			common.SysLog(fmt.Sprintf("Login database error for user %s: %v", username, err))
			common.ApiErrorI18n(c, i18n.MsgDatabaseError)
		case errors.Is(err, model.ErrUserEmptyCredentials):
			common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		default:
			common.ApiErrorI18n(c, i18n.MsgUserUsernameOrPasswordError)
		}
		return
	}

	completeLogin(&user, c, loginMethodPassword)
}

// completeLogin 是密码、OAuth、微信、Telegram 登录的共同出口。
// eflow: 绑定了两步验证或 Passkey 的账号一律先进入待验证状态，任何主登录方式都不能绕过
// 第二因子（上游 6f2333990 的意图）。无密码 Passkey 登录本身即强认证，直接调用 setupLogin。
func completeLogin(user *model.User, c *gin.Context, loginMethod string) {
	methods, err := loginVerificationMethods(user.Id)
	if err != nil {
		common.SysLog(fmt.Sprintf("Login failed to load verification factors for user %d: %v", user.Id, err))
		common.ApiErrorI18n(c, i18n.MsgDatabaseError)
		return
	}
	if len(methods) == 0 {
		setupLogin(user, c, loginMethod)
		return
	}
	startPendingLogin(c, user, loginMethod, methods)
}

// Login methods recorded in the login audit (AuditOther.login_method).
const (
	loginMethodPassword    = "password"
	loginMethodPasskey     = "passkey"
	loginMethodWeChat      = "wechat"
	loginMethodTelegram    = "telegram"
	loginMethodOAuthPrefix = "oauth:"
)

// setup session & cookies and then return user info
func setupLogin(user *model.User, c *gin.Context, loginMethod string) {
	model.UpdateUserLastLoginAt(user.Id)
	session := sessions.Default(c)
	// eflow: 登录时换发会话 ID，登录前被人塞进浏览器的会话 ID 不能沿用为登录态。
	if err := common.RegenerateSession(session); err != nil {
		common.ApiErrorI18n(c, i18n.MsgUserSessionSaveFailed)
		return
	}
	session.Set("id", user.Id)
	session.Set("username", user.Username)
	session.Set("role", user.Role)
	session.Set("status", user.Status)
	session.Set("group", user.Group)
	// eflow: 记录本次登录时间，没有任何二次验证手段的账号用它判断"刚登录过"。
	session.Set(LoginAtSessionKey, time.Now().Unix())
	recordLoginMetadata(session, c, loginMethod)
	// Step-up state of a previous account in this browser must not carry over
	// to the account signing in now.
	middleware.ResetStepUpSession(session)
	err := session.Save()
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgUserSessionSaveFailed)
		return
	}
	// eflow: 同时签发 e-flowcode-auth JWT cookie，给同域子站做"我是谁"凭证；
	// 未配置 EFLOW_AUTH_SECRET 时该函数返回空串，跳过 SetCookie，无副作用。
	issueEflowAuthCookie(c, user.Id, common.CurrentSessionID(session))
	recordLoginAudit(c, user, loginMethod)
	c.JSON(http.StatusOK, gin.H{
		"message": "",
		"success": true,
		"data": map[string]any{
			"id":           user.Id,
			"username":     user.Username,
			"display_name": user.DisplayName,
			"role":         user.Role,
			"status":       user.Status,
			"group":        user.Group,
		},
	})
}

func Logout(c *gin.Context) {
	session := sessions.Default(c)
	session.Clear()
	err := session.Save()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"message": err.Error(),
			"success": false,
		})
		return
	}
	// eflow: 同步清掉 e-flowcode-auth cookie，避免子站继续认旧身份。
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(common.EflowAuthCookieName, "", -1, "/", "", isHTTPSRequest(c), true)
	c.JSON(http.StatusOK, gin.H{
		"message": "",
		"success": true,
	})
}

// revokeUserSessions eflow: 作废该用户的服务端登录会话，用于改密、重置密码、封禁、注销等场景。
// keepCurrent 时保留发起本次请求的会话。失败只记日志，不影响主操作结果。
func revokeUserSessions(c *gin.Context, userID int, keepCurrent bool) {
	keep := ""
	if keepCurrent {
		if _, ok := c.Get(sessions.DefaultKey); ok {
			keep = common.CurrentSessionID(sessions.Default(c))
		}
	}
	if err := common.RevokeUserSessions(userID, keep); err != nil {
		common.SysLog(fmt.Sprintf("failed to revoke sessions for user %d: %s", userID, err.Error()))
	}
}

func Register(c *gin.Context) {
	authSettings := common.GetRequestAuthSettings(c)
	userQuotaSettings := common.GetUserQuotaSettings()
	autoGroupPolicy := setting.GetAutoGroupPolicy()
	if !authSettings.RegistrationEnabled {
		common.ApiErrorI18n(c, i18n.MsgUserRegisterDisabled)
		return
	}
	if !authSettings.PasswordRegisterEnabled {
		common.ApiErrorI18n(c, i18n.MsgUserPasswordRegisterDisabled)
		return
	}
	var user model.User
	err := json.NewDecoder(c.Request.Body).Decode(&user)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	user.Username = strings.TrimSpace(user.Username)
	user.Email = model.NormalizeEmail(user.Email)
	if user.Username == "" {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if err := common.Validate.Struct(&user); err != nil {
		common.ApiErrorI18n(c, i18n.MsgUserInputInvalid, map[string]any{"Error": err.Error()})
		return
	}
	if authSettings.EmailVerificationEnabled {
		if user.Email == "" || user.VerificationCode == "" {
			common.ApiErrorI18n(c, i18n.MsgUserEmailVerificationRequired)
			return
		}
		valid, err := common.VerifyCodeWithKey(user.Email, user.VerificationCode, common.EmailVerificationPurpose)
		if err != nil {
			verificationStoreError(c)
			return
		}
		if !valid {
			common.ApiErrorI18n(c, i18n.MsgUserVerificationCodeError)
			return
		}
		if err := model.EnsureEmailAvailable(user.Email, 0); err != nil {
			if errors.Is(err, model.ErrEmailAlreadyTaken) {
				common.ApiErrorI18n(c, i18n.MsgUserEmailAlreadyTaken)
				return
			}
			common.ApiErrorI18n(c, i18n.MsgDatabaseError)
			return
		}
	}
	emailForExistCheck := ""
	if authSettings.EmailVerificationEnabled {
		emailForExistCheck = user.Email
	}
	exist, err := model.CheckUserExistOrDeleted(user.Username, emailForExistCheck)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgDatabaseError)
		common.SysLog(fmt.Sprintf("CheckUserExistOrDeleted error: %v", err))
		return
	}
	if exist {
		common.ApiErrorI18n(c, i18n.MsgUserExists)
		return
	}
	invitationCodeValue := strings.TrimSpace(user.InvitationCode)
	affCode := user.AffCode // this code is the inviter's code, not the user's own code
	inviterId, _ := model.GetUserIdByAffCode(affCode)
	cleanUser := model.User{
		Username:    user.Username,
		Password:    user.Password,
		DisplayName: user.Username,
		InviterId:   inviterId,
		Role:        common.RoleCommonUser, // 明确设置角色为普通用户
	}
	if authSettings.EmailVerificationEnabled {
		cleanUser.Email = user.Email
	}
	var invitationCode *model.InvitationCode
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		if authSettings.InvitationCodeEnabled || invitationCodeValue != "" {
			invitationCode, err = model.GetUsableInvitationCodeWithTx(tx, invitationCodeValue)
			if err != nil {
				return err
			}
			if invitationCode.OwnerUserId != 0 {
				inviterId = invitationCode.OwnerUserId
			}
		}
		cleanUser.InviterId = inviterId
		if err := cleanUser.InsertWithTx(tx, inviterId, userQuotaSettings); err != nil {
			return err
		}
		if invitationCode != nil {
			if err := model.ConsumeInvitationCodeWithTx(tx, invitationCode, cleanUser.Id, cleanUser.Username); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, model.ErrEmailAlreadyTaken) {
			common.ApiErrorI18n(c, i18n.MsgUserEmailAlreadyTaken)
			return
		}
		if handleInvitationCodeError(c, err) {
			return
		}
		common.ApiError(c, err)
		return
	}
	if authSettings.EmailVerificationEnabled {
		// 注册成功后作废验证码，避免同一验证码被再次用于注册。
		_, _ = common.ConsumeVerificationCodeWithKey(user.Email, user.VerificationCode, common.EmailVerificationPurpose)
	}
	cleanUser.FinalizeOAuthUserCreation(inviterId, userQuotaSettings)
	// 生成默认令牌
	if constant.GenerateDefaultToken {
		key, err := common.GenerateKey()
		if err != nil {
			common.ApiErrorI18n(c, i18n.MsgUserDefaultTokenFailed)
			common.SysLog("failed to generate token key: " + err.Error())
			return
		}
		// 生成默认令牌
		token := model.Token{
			UserId:             cleanUser.Id,
			Name:               cleanUser.Username + "的初始令牌",
			Key:                key,
			CreatedTime:        common.GetTimestamp(),
			AccessedTime:       common.GetTimestamp(),
			ExpiredTime:        -1,     // 永不过期
			RemainQuota:        500000, // 示例额度
			UnlimitedQuota:     true,
			ModelLimitsEnabled: false,
		}
		if autoGroupPolicy.DefaultUseAutoGroup {
			token.Group = "auto"
		}
		if err := token.Insert(); err != nil {
			common.ApiErrorI18n(c, i18n.MsgCreateDefaultTokenErr)
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

// eflow: 解析查询串里的 role 参数为 []int。前端 columnFilter 多选时以
//
//	?role=1&role=5&role=10  方式发出（QueryArray），单选退化为单元素切片。
//
// 非整数静默丢弃,避免脏值导致 500;全空返回 nil,model 层 len()==0 不挂 WHERE。
func parseRolesQuery(c *gin.Context) []int {
	raw := c.QueryArray("role")
	if len(raw) == 0 {
		return nil
	}
	out := make([]int, 0, len(raw))
	for _, s := range raw {
		v, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil {
			continue
		}
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// eflow: 用户状态筛选——同 parseRolesQuery：?status=1&status=3 取并集 IN。
// 列表页本来就有 status columnFilter，但之前是 client-side filterFn 跨页失效，
// 跟 role 同型 bug。本次接到后端按 IN 过滤，跨页正确。
func parseStatusesQuery(c *gin.Context) []int {
	raw := c.QueryArray("status")
	if len(raw) == 0 {
		return nil
	}
	out := make([]int, 0, len(raw))
	for _, s := range raw {
		v, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil {
			continue
		}
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func GetAllUsers(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	sortOpts := model.NewUserSortOptions(c.Query("sort_by"), c.Query("sort_order"))
	roles := parseRolesQuery(c)
	statuses := parseStatusesQuery(c)
	group := strings.TrimSpace(c.Query("group"))
	users, total, err := model.GetAllUsers(pageInfo, sortOpts, roles, statuses, group)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(users)

	common.ApiSuccess(c, pageInfo)
	return
}

func SearchUsers(c *gin.Context) {
	keyword := c.Query("keyword")
	group := c.Query("group")
	// eflow: 用户列表三个独立搜索框——各自只过滤自己的字段（AND 组合），不跨字段 OR
	searchId := c.Query("search_id")
	searchUsername := c.Query("search_username")
	searchEmail := c.Query("search_email")
	roles := parseRolesQuery(c)
	statuses := parseStatusesQuery(c)
	pageInfo := common.GetPageQuery(c)
	sortOpts := model.NewUserSortOptions(c.Query("sort_by"), c.Query("sort_order"))
	users, total, err := model.SearchUsers(keyword, group, roles, statuses, pageInfo.GetStartIdx(), pageInfo.GetPageSize(), sortOpts, searchId, searchUsername, searchEmail)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(users)
	common.ApiSuccess(c, pageInfo)
	return
}

// ListUserDistinctGroups 给"用户列表-分组筛选"下拉提供选项。
// 区别于 GET /api/group/（那个走 ratio_setting.GetGroupRatioCopy()，是 admin 在
// 「系统设置→分组倍率」配置过的"应该有"的分组），这里直读 users.group 字段的
// 实际取值——历史上有 worker 这种没在倍率配置里登记但 users 表用着的分组，
// 必须直查列才能选到。
func ListUserDistinctGroups(c *gin.Context) {
	groups, err := model.GetDistinctUserGroups()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if groups == nil {
		groups = []string{}
	}
	common.ApiSuccess(c, groups)
}

// ListUserGroupOptions returns customer tiers only. Token routing groups are
// served by /api/group/ and must not be used as user.group options.
func ListUserGroupOptions(c *gin.Context) {
	groups := common.GetTopupGroupNames()
	if !common.StringsContains(groups, "default") {
		groups = append(groups, "default")
	}
	sort.Strings(groups)
	common.ApiSuccess(c, groups)
}

func canManageTargetRole(myRole int, targetRole int) bool {
	return myRole == common.RoleRootUser || myRole > targetRole
}

func GetUser(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	user, err := model.GetUserById(id, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	myRole := c.GetInt("role")
	if !canManageTargetRole(myRole, user.Role) {
		common.ApiErrorI18n(c, i18n.MsgUserNoPermissionSameLevel)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    user,
	})
	return
}

// GetUserProfileSummary 通用用户画像（staff: admin + 客服）。返回字段跟
// GetTicketUserProfile 的 TicketUserProfileResponse 一致，但不依赖 ticket id ——
// 前端在使用日志、用户列表等地方点用户名时调这条接口拉 sheet 数据。
//
// 跟 GetUser 不同——这条只返回只读 summary（无 password / token 等敏感字段），
// 因此不做"同级或更高 role 不可看"的限制（跟 GetTicketUserProfile 行为对齐），
// 客服查看其他客服 / 管理员也允许。
func GetUserProfileSummary(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	user, err := model.GetUserById(id, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	resp := TicketUserProfileResponse{
		UserId:       user.Id,
		Username:     user.Username,
		DisplayName:  user.DisplayName,
		Email:        user.Email,
		Role:         user.Role,
		Status:       user.Status,
		Group:        user.Group,
		CreatedTime:  user.CreatedTime,
		Quota:        user.Quota,
		UsedQuota:    user.UsedQuota,
		RequestCount: user.RequestCount,
	}

	if pending, pErr := model.SumUserPendingRefundQuota(user.Id); pErr != nil {
		common.SysLog(fmt.Sprintf("user profile: failed to sum pending refund quota for user %d: %s", user.Id, pErr.Error()))
	} else {
		resp.PendingRefundQuota = pending
	}

	recentLogs, _, logErr := model.GetUserLogs(
		user.Id, model.LogTypeConsume, 0, 0, "", "",
		0, ticketUserRecentLogLimit, "", "", "",
		model.LogSortOptions{},
	)
	if logErr != nil {
		common.SysLog(fmt.Sprintf("user profile: failed to fetch recent logs for user %d: %s", user.Id, logErr.Error()))
		recentLogs = nil
	}
	resp.RecentLogs = recentLogs

	since := common.GetTimestamp() - int64(ticketUserModelUsageDays)*24*3600
	usage, usageErr := model.GetUserModelUsageTopN(user.Id, since, ticketUserModelUsageLimit)
	if usageErr != nil {
		common.SysLog(fmt.Sprintf("user profile: failed to aggregate model usage for user %d: %s", user.Id, usageErr.Error()))
	}
	resp.ModelUsage = usage

	common.ApiSuccess(c, resp)
}

type TransferAffQuotaRequest struct {
	Quota int `json:"quota" binding:"required"`
}

func TransferAffQuota(c *gin.Context) {
	if !requirePaymentCompliance(c) {
		return
	}

	id := c.GetInt("id")
	user, err := model.GetUserById(id, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	tran := TransferAffQuotaRequest{}
	if err := c.ShouldBindJSON(&tran); err != nil {
		common.ApiError(c, err)
		return
	}
	err = user.TransferAffQuotaToQuota(tran.Quota)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgUserTransferFailed, map[string]any{"Error": err.Error()})
		return
	}
	common.ApiSuccessI18n(c, i18n.MsgUserTransferSuccess, nil)
}

func GetAffCode(c *gin.Context) {
	id := c.GetInt("id")
	user, err := model.GetUserById(id, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if user.AffCode == "" {
		user.AffCode = common.GetRandomString(4)
		if err := user.Update(false); err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    user.AffCode,
	})
	return
}

func GetSelf(c *gin.Context) {
	id := c.GetInt("id")
	userRole := c.GetInt("role")
	user, err := model.GetUserById(id, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	// eflow: lazy issue —— 用户带着旧 session 第一次进 web 时这里会被调到，
	// 顺便补签 e-flowcode-auth cookie，避免要求所有人重新登录。
	// cookie 缺失 / 过期 / 签名异常都会重新签发。
	if common.EflowAuthEnabled() {
		// eflow: 缺失、过期或属于其他会话（换号登录、旧会话签发）的子站 cookie 一律按当前会话重签。
		sid := currentSessionID(c)
		existing, _ := c.Cookie(common.EflowAuthCookieName)
		if !common.EflowAuthMatchesSession(existing, id, eflowSessionPublicID(sid)) {
			issueEflowAuthCookie(c, id, sid)
		}
	}
	// Hide admin remarks: set to empty to trigger omitempty tag, ensuring the remark field is not included in JSON returned to regular users
	user.Remark = ""

	// 计算用户权限信息
	permissions := calculateUserPermissions(userRole)

	// 获取用户设置并提取sidebar_modules
	userSetting := user.GetSetting()

	// 构建响应数据，包含用户信息和权限
	responseData := map[string]any{
		"id":                user.Id,
		"username":          user.Username,
		"display_name":      user.DisplayName,
		"role":              user.Role,
		"status":            user.Status,
		"email":             user.Email,
		"github_id":         user.GitHubId,
		"discord_id":        user.DiscordId,
		"oidc_id":           user.OidcId,
		"wechat_id":         user.WeChatId,
		"telegram_id":       user.TelegramId,
		"group":             user.Group,
		"quota":             user.Quota,
		"used_quota":        user.UsedQuota,
		"request_count":     user.RequestCount,
		"aff_code":          user.AffCode,
		"aff_count":         user.AffCount,
		"aff_quota":         user.AffQuota,
		"aff_history_quota": user.AffHistoryQuota,
		"inviter_id":        user.InviterId,
		"linux_do_id":       user.LinuxDOId,
		"setting":           user.Setting,
		"stripe_customer":   user.StripeCustomer,
		"sidebar_modules":   userSetting.SidebarModules, // 正确提取sidebar_modules字段
		"permissions":       permissions,                // 新增权限字段
		// risk_warning_pending: vague-on-purpose flag for the login warning
		// modal. We never expose the timestamp or the underlying rule.
		"risk_warning_pending": user.RiskWarningPendingAt > 0,
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    responseData,
	})
	return
}

// AcknowledgeRiskWarning clears the user's pending risk-warning flag. The
// underlying block/observe decision is unchanged; this only dismisses the
// next-login modal.
func AcknowledgeRiskWarning(c *gin.Context) {
	id := c.GetInt("id")
	if id <= 0 {
		common.ApiErrorMsg(c, "未登录")
		return
	}
	if err := model.AckUserRiskWarningPending(id); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"acknowledged": true})
}

// 计算用户权限的辅助函数
func calculateUserPermissions(userRole int) map[string]any {
	permissions := map[string]any{}

	// 根据用户角色计算权限
	if userRole == common.RoleRootUser {
		// 超级管理员不需要边栏设置功能
		permissions["sidebar_settings"] = false
		permissions["sidebar_modules"] = map[string]any{}
	} else if userRole == common.RoleAdminUser {
		// 管理员可以设置边栏，但不包含系统设置功能
		permissions["sidebar_settings"] = true
		permissions["sidebar_modules"] = map[string]any{
			"admin": map[string]any{
				"setting": false, // 管理员不能访问系统设置
			},
		}
	} else if userRole == common.RoleCustomerServiceUser || userRole == common.RoleFinanceUser {
		// 客服 / 财务：仅工单管理与用户管理只读视图。其它管理员功能一律锁闭。
		permissions["sidebar_settings"] = true
		permissions["sidebar_modules"] = map[string]any{
			"admin": map[string]any{
				"channel":         false,
				"models":          false,
				"redemption":      false,
				"invitation_code": false,
				"setting":         false,
				"user":            false,
				// ticket_admin 保持开启，由默认配置提供
			},
		}
	} else {
		// 普通用户只能设置个人功能，不包含管理员区域
		permissions["sidebar_settings"] = true
		permissions["sidebar_modules"] = map[string]any{
			"admin": false, // 普通用户不能访问管理员区域
		}
	}

	return permissions
}

// 根据用户角色生成默认的边栏配置
func generateDefaultSidebarConfig(userRole int) string {
	defaultConfig := map[string]any{}

	// 聊天区域 - 所有用户都可以访问
	defaultConfig["chat"] = map[string]any{
		"enabled":    true,
		"playground": true,
		"chat":       true,
	}

	// 控制台区域 - 所有用户都可以访问
	defaultConfig["console"] = map[string]any{
		"enabled":    true,
		"detail":     true,
		"token":      true,
		"log":        true,
		"midjourney": true,
		"task":       true,
		"ticket":     true,
	}

	// 个人中心区域 - 所有用户都可以访问
	defaultConfig["personal"] = map[string]any{
		"enabled":  true,
		"topup":    true,
		"personal": true,
	}

	// 管理员区域 - 根据角色决定
	if userRole == common.RoleAdminUser {
		// 管理员可以访问管理员区域，但不能访问系统设置
		defaultConfig["admin"] = map[string]any{
			"enabled":         true,
			"channel":         true,
			"models":          true,
			"redemption":      true,
			"invitation_code": true,
			"user":            true,
			"ticket_admin":    true,
			"setting":         false, // 管理员不能访问系统设置
		}
	} else if userRole == common.RoleRootUser {
		// 超级管理员可以访问所有功能
		defaultConfig["admin"] = map[string]any{
			"enabled":         true,
			"channel":         true,
			"models":          true,
			"redemption":      true,
			"invitation_code": true,
			"user":            true,
			"ticket_admin":    true,
			"setting":         true,
		}
	} else if userRole == common.RoleCustomerServiceUser {
		// 客服：仅开放工单后台入口；其它管理员功能一律关闭。
		defaultConfig["admin"] = map[string]any{
			"enabled":         true,
			"channel":         false,
			"models":          false,
			"redemption":      false,
			"invitation_code": false,
			"user":            false,
			"ticket_admin":    true,
			"setting":         false,
		}
	}
	// 普通用户不包含admin区域

	// 转换为JSON字符串
	configBytes, err := json.Marshal(defaultConfig)
	if err != nil {
		common.SysLog("生成默认边栏配置失败: " + err.Error())
		return ""
	}

	return string(configBytes)
}

func GetUserModels(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		id = c.GetInt("id")
	}
	user, err := model.GetUserCache(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	groups := service.GetUserEffectiveGroups(user.Group, user.AdditionalCustomerTiers.Values())
	var models []string
	for group := range groups {
		for _, g := range model.GetGroupEnabledModels(group) {
			if !common.StringsContains(models, g) {
				models = append(models, g)
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    models,
	})
	return
}

func UpdateUser(c *gin.Context) {
	// 指针字段区分“未提交”与“显式清空”：未提交的邮箱、客户等级保留原值，
	// 避免脚本局部更新时静默重置主客户等级或清空附加客户等级。
	var request struct {
		model.User
		Email                   *string          `json:"email"`
		Group                   *string          `json:"group"`
		AdditionalCustomerTiers *model.GroupList `json:"additional_customer_tiers"`
	}
	err := common.DecodeJson(c.Request.Body, &request)
	updatedUser := request.User
	if request.Email != nil {
		updatedUser.Email = model.NormalizeEmail(*request.Email)
	}
	if err != nil || updatedUser.Id == 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if updatedUser.Password == "" {
		updatedUser.Password = "$I_LOVE_U" // make Validator happy :)
	}
	if err := common.Validate.Struct(&updatedUser); err != nil {
		common.ApiErrorI18n(c, i18n.MsgUserInputInvalid, map[string]any{"Error": err.Error()})
		return
	}
	originUser, err := model.GetUserById(updatedUser.Id, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if request.Email == nil {
		updatedUser.Email = originUser.Email
	}
	updatedUser.Group = originUser.Group
	updatedUser.AdditionalCustomerTiers = originUser.AdditionalCustomerTiers
	myRole := c.GetInt("role")
	if !canManageTargetRole(myRole, originUser.Role) {
		common.ApiErrorI18n(c, i18n.MsgUserNoPermissionHigherLevel)
		return
	}
	if !canManageTargetRole(myRole, updatedUser.Role) {
		common.ApiErrorI18n(c, i18n.MsgUserCannotCreateHigherLevel)
		return
	}
	// 只校验本次提交的等级；保留的原值即使对应等级已被删除，也不阻断其他字段的修改。
	if request.Group != nil {
		updatedUser.Group = *request.Group
		if updatedUser.Group == "" {
			updatedUser.Group = "default"
		}
		if err := validateCustomerTier(updatedUser.Group); err != nil {
			common.ApiErrorMsg(c, err.Error())
			return
		}
	}
	if request.AdditionalCustomerTiers != nil {
		updatedUser.AdditionalCustomerTiers = *request.AdditionalCustomerTiers
		if err := validateAdditionalCustomerTiers(updatedUser.AdditionalCustomerTiers); err != nil {
			common.ApiErrorMsg(c, err.Error())
			return
		}
	}
	updatedUser.AdditionalCustomerTiers = withoutPrimaryTier(updatedUser.AdditionalCustomerTiers, updatedUser.Group)
	if updatedUser.Password == "$I_LOVE_U" {
		updatedUser.Password = "" // rollback to what it should be
	}
	if updatedUser.Email != "" {
		if err := model.EnsureEmailAvailable(updatedUser.Email, updatedUser.Id); err != nil {
			if !errors.Is(err, model.ErrEmailAlreadyTaken) {
				common.ApiError(c, err)
				return
			}
			common.ApiErrorMsg(c, "邮箱已被其他用户使用")
			return
		}
	}
	updatePassword := updatedUser.Password != ""
	// eflow: 重置密码、改邮箱（找回密码通道）需要绑定被管理用户的一次性验证凭证；
	// 只改分组、备注、显示名等不需要（admin-api 整对象回写只改分组时不会触发）。
	if updatePassword || updatedUser.Email != originUser.Email {
		if requireAdminUserProof(c, service.VerificationScopeAdminUserUpdate, service.AdminUserContext{UserID: updatedUser.Id}) == nil {
			return
		}
	}
	if err := updatedUser.Edit(updatePassword); err != nil {
		common.ApiError(c, err)
		return
	}
	if updatePassword {
		revokeUserSessions(c, updatedUser.Id, false)
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

func AdminClearUserBinding(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}

	bindingType := strings.ToLower(strings.TrimSpace(c.Param("binding_type")))
	if bindingType == "" {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}

	user, err := model.GetUserById(id, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	myRole := c.GetInt("role")
	if !canManageTargetRole(myRole, user.Role) {
		common.ApiErrorI18n(c, i18n.MsgUserNoPermissionSameLevel)
		return
	}
	if requireAdminUserProof(c, service.VerificationScopeAdminUserBindingClear, service.AdminUserBindingContext{UserID: user.Id, BindingType: bindingType}) == nil {
		return
	}

	if err := user.ClearBinding(bindingType); err != nil {
		common.ApiError(c, err)
		return
	}

	model.RecordLog(user.Id, model.LogTypeManage, fmt.Sprintf("admin cleared %s binding for user %s", bindingType, user.Username))

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "success",
	})
}

func UpdateSelf(c *gin.Context) {
	var requestData map[string]any
	if err := common.DecodeJson(c.Request.Body, &requestData); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}

	// 检查是否是用户设置更新请求 (sidebar_modules 或 language)
	if sidebarModules, sidebarExists := requestData["sidebar_modules"]; sidebarExists {
		userId := c.GetInt("id")
		user, err := model.GetUserById(userId, false)
		if err != nil {
			common.ApiError(c, err)
			return
		}

		// 获取当前用户设置
		currentSetting := user.GetSetting()

		// 更新sidebar_modules字段
		if sidebarModulesStr, ok := sidebarModules.(string); ok {
			currentSetting.SidebarModules = sidebarModulesStr
		}

		if err := model.UpdateUserSetting(user.Id, currentSetting); err != nil {
			common.ApiErrorI18n(c, i18n.MsgUpdateFailed)
			return
		}

		common.ApiSuccessI18n(c, i18n.MsgUpdateSuccess, nil)
		return
	}

	// 检查是否是语言偏好更新请求
	if language, langExists := requestData["language"]; langExists {
		userId := c.GetInt("id")
		user, err := model.GetUserById(userId, false)
		if err != nil {
			common.ApiError(c, err)
			return
		}

		// 获取当前用户设置
		currentSetting := user.GetSetting()

		// 更新language字段
		if langStr, ok := language.(string); ok {
			currentSetting.Language = langStr
		}

		if err := model.UpdateUserSetting(user.Id, currentSetting); err != nil {
			common.ApiErrorI18n(c, i18n.MsgUpdateFailed)
			return
		}

		common.ApiSuccessI18n(c, i18n.MsgUpdateSuccess, nil)
		return
	}

	// 原有的用户信息更新逻辑
	var user model.User
	requestDataBytes, err := common.Marshal(requestData)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if err = common.Unmarshal(requestDataBytes, &user); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}

	if user.Password == "" {
		user.Password = "$I_LOVE_U" // make Validator happy :)
	}
	if err := common.Validate.Struct(&user); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidInput)
		return
	}

	cleanUser := model.User{
		Id:          c.GetInt("id"),
		Username:    user.Username,
		Password:    user.Password,
		DisplayName: user.DisplayName,
	}
	if user.Password == "$I_LOVE_U" {
		user.Password = "" // rollback to what it should be
		cleanUser.Password = ""
	}
	updatePassword, err := checkUpdatePassword(user.OriginalPassword, user.Password, cleanUser.Id)
	if err == nil && updatePassword {
		// eflow: 改密在核对原密码之外还需要 account.password.change 一次性验证凭证。
		if middleware.RequireSecurityProof(c, service.VerificationOperation{Scope: service.VerificationScopePasswordChange}) == nil {
			return
		}
	}
	if err != nil {
		if errors.Is(err, errUserPasswordUnset) {
			common.ApiErrorI18n(c, i18n.MsgUserPasswordUnset)
			return
		}
		if errors.Is(err, errPasswordUnchanged) {
			common.ApiErrorMsg(c, errPasswordUnchanged.Error())
			return
		}
		if errors.Is(err, errOriginalPasswordFail) {
			common.ApiErrorI18n(c, i18n.MsgUserOriginalPasswordError)
			return
		}
		common.ApiError(c, err)
		return
	}
	if err := cleanUser.Update(updatePassword); err != nil {
		common.ApiError(c, err)
		return
	}
	if updatePassword {
		// eflow: 改密后踢掉其他设备，只保留当前会话。
		revokeUserSessions(c, cleanUser.Id, true)
		recordUserSecurityAudit(c, cleanUser.Id, "user.password_change", nil)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

func checkUpdatePassword(originalPassword string, newPassword string, userId int) (updatePassword bool, err error) {
	if newPassword == "" {
		return
	}
	var currentUser *model.User
	currentUser, err = model.GetUserById(userId, true)
	if err != nil {
		return
	}

	// 密码不为空,需要验证原密码
	if currentUser.Password == "" {
		err = errUserPasswordUnset
		return
	}
	if !common.ValidatePasswordAndHash(originalPassword, currentUser.Password) {
		err = errOriginalPasswordFail
		return
	}
	if newPassword == originalPassword {
		err = errPasswordUnchanged
		return
	}
	updatePassword = true
	return
}

func DeleteUser(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	originUser, err := model.GetUserById(id, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	myRole := c.GetInt("role")
	if myRole <= originUser.Role {
		common.ApiErrorI18n(c, i18n.MsgUserNoPermissionHigherLevel)
		return
	}
	if requireAdminUserProof(c, service.VerificationScopeAdminUserDelete, service.AdminUserContext{UserID: id}) == nil {
		return
	}
	err = model.HardDeleteUserById(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	revokeUserSessions(c, id, false)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

// DeleteSelf 注销当前账号。设置了密码的账号必须提交正确的当前密码；
// 仅通过第三方登录、从未设置密码的账号需要近期身份验证。
// DeleteSelf deletes the caller's account after a single-use account.delete
// proof, then signs out every session and clears the sub-site credential.
func DeleteSelf(c *gin.Context) {
	id := c.GetInt("id")
	user, err := model.GetUserById(id, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if user.Role == common.RoleRootUser {
		common.ApiErrorI18n(c, i18n.MsgUserCannotDeleteRootUser)
		return
	}
	authorization := middleware.RequireSecurityProof(c, service.VerificationOperation{Scope: service.VerificationScopeAccountDelete})
	if authorization == nil {
		return
	}

	if err := model.DeleteUserById(id); err != nil {
		common.ApiError(c, err)
		return
	}
	revokeUserSessions(c, id, false)
	c.SetCookie(common.EflowAuthCookieName, "", -1, "/", "", isHTTPSRequest(c), true)
	recordUserSecurityAudit(c, id, "user.account_delete", map[string]any{"verification_method": authorization.Method})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

func CreateUser(c *gin.Context) {
	var user model.User
	err := json.NewDecoder(c.Request.Body).Decode(&user)
	user.Username = strings.TrimSpace(user.Username)
	if err != nil || user.Username == "" || user.Password == "" {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if err := common.Validate.Struct(&user); err != nil {
		common.ApiErrorI18n(c, i18n.MsgUserInputInvalid, map[string]any{"Error": err.Error()})
		return
	}
	if user.DisplayName == "" {
		user.DisplayName = user.Username
	}
	myRole := c.GetInt("role")
	if !common.IsValidateRole(user.Role) {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if user.Role >= myRole {
		common.ApiErrorI18n(c, i18n.MsgUserCannotCreateHigherLevel)
		return
	}
	if user.Group == "" {
		user.Group = "default"
	}

	if err := validateCustomerTier(user.Group); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	if err := validateAdditionalCustomerTiers(user.AdditionalCustomerTiers); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	// eflow: 创建管理员需要绑定目标角色的一次性验证凭证；客服、财务、普通用户不需要。
	if user.Role >= common.RoleAdminUser {
		if requireAdminUserProof(c, service.VerificationScopeAdminUserCreate, service.AdminUserCreateContext{Role: user.Role}) == nil {
			return
		}
	}
	// Even for admin users, we cannot fully trust them!
	cleanUser := model.User{
		Username:                user.Username,
		Password:                user.Password,
		DisplayName:             user.DisplayName,
		Role:                    user.Role, // 保持管理员设置的角色
		Group:                   user.Group,
		AdditionalCustomerTiers: withoutPrimaryTier(user.AdditionalCustomerTiers, user.Group),
	}
	if err := cleanUser.Insert(0); err != nil {
		common.ApiError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

func validateCustomerTier(group string) error {
	if group == "default" {
		return nil
	}
	if slices.Contains(common.GetTopupGroupNames(), group) {
		return nil
	}
	return fmt.Errorf("客户等级 %q 未配置", group)
}

// withoutPrimaryTier 去掉与主客户等级重复的附加等级；有效等级集合始终包含主等级，重复项只会造成展示冗余。
func withoutPrimaryTier(tiers model.GroupList, primary string) model.GroupList {
	kept := make([]string, 0)
	for _, tier := range tiers.Values() {
		if tier != primary {
			kept = append(kept, tier)
		}
	}
	return model.NewGroupList(kept)
}

func validateAdditionalCustomerTiers(tiers model.GroupList) error {
	for _, tier := range tiers.Values() {
		if err := validateCustomerTier(tier); err != nil {
			return err
		}
	}
	return nil
}

type ManageRequest struct {
	Id     int    `json:"id"`
	Action string `json:"action"`
	Value  int    `json:"value"`
	Mode   string `json:"mode"`
}

// ManageUser Only admin user can do this
func ManageUser(c *gin.Context) {
	var req ManageRequest
	err := json.NewDecoder(c.Request.Body).Decode(&req)

	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	user := model.User{
		Id: req.Id,
	}
	// Fill attributes
	model.DB.Unscoped().Where(&user).First(&user)
	if user.Id == 0 {
		common.ApiErrorI18n(c, i18n.MsgUserNotExists)
		return
	}
	myRole := c.GetInt("role")
	if !canManageTargetRole(myRole, user.Role) {
		common.ApiErrorI18n(c, i18n.MsgUserNoPermissionHigherLevel)
		return
	}
	// requireManageProof consumes the proof bound to this user and action once
	// the action's own checks have passed. Quota adjustments are not gated.
	requireManageProof := func(role *int) bool {
		if adminUserStatusExempt(c, req.Action) {
			return true
		}
		return requireAdminUserProof(c, service.VerificationScopeAdminUserManage, service.AdminUserManageContext{UserID: user.Id, Action: req.Action, Role: role}) != nil
	}
	switch req.Action {
	case "disable":
		user.Status = common.UserStatusDisabled
		if user.Role == common.RoleRootUser {
			common.ApiErrorI18n(c, i18n.MsgUserCannotDisableRootUser)
			return
		}
		if !requireManageProof(nil) {
			return
		}
	case "enable":
		user.Status = common.UserStatusEnabled
		if !requireManageProof(nil) {
			return
		}
	case "delete":
		if user.Role == common.RoleRootUser {
			common.ApiErrorI18n(c, i18n.MsgUserCannotDeleteRootUser)
			return
		}
		if requireAdminUserProof(c, service.VerificationScopeAdminUserDelete, service.AdminUserContext{UserID: user.Id}) == nil {
			return
		}
		if err := user.Delete(); err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
		// 删除用户后，强制清理 Redis 中所有该用户令牌的缓存，
		// 避免已缓存的令牌在 TTL 过期前仍能通过 TokenAuth 校验。
		if err := model.InvalidateUserTokensCache(user.Id); err != nil {
			common.SysLog(fmt.Sprintf("failed to invalidate tokens cache for user %d: %s", user.Id, err.Error()))
		}
		// eflow: 软删除后不能再走下面的 user.Update（记录已不可见，会误报 record not found）。
		revokeUserSessions(c, user.Id, false)
		c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
		return
	case "promote":
		if myRole != common.RoleRootUser {
			common.ApiErrorI18n(c, i18n.MsgUserAdminCannotPromote)
			return
		}
		if user.Role >= common.RoleAdminUser {
			common.ApiErrorI18n(c, i18n.MsgUserAlreadyAdmin)
			return
		}
		if !requireManageProof(nil) {
			return
		}
		user.Role = common.RoleAdminUser
	case "demote":
		if user.Role == common.RoleRootUser {
			common.ApiErrorI18n(c, i18n.MsgUserCannotDemoteRootUser)
			return
		}
		if user.Role == common.RoleCommonUser {
			common.ApiErrorI18n(c, i18n.MsgUserAlreadyCommon)
			return
		}
		if !requireManageProof(nil) {
			return
		}
		user.Role = common.RoleCommonUser
	case "set_role":
		// set_role 允许管理员直接选择目标角色，是 promote/demote 的通用替代。
		// 规则：
		//   - 只能在"自己角色以下"的范围内操作（RoleRootUser 例外可以任意设置）
		//   - 不能把 Root 用户降级（只能手动在 DB 中改动）
		//   - 目标角色必须是合法值
		if user.Role == common.RoleRootUser {
			common.ApiErrorI18n(c, i18n.MsgUserCannotDemoteRootUser)
			return
		}
		target := req.Value
		if !common.IsValidateRole(target) || target == common.RoleGuestUser {
			common.ApiErrorI18n(c, i18n.MsgInvalidParams)
			return
		}
		// 权限：提升到 >= 自身角色只允许 Root
		if target >= myRole && myRole != common.RoleRootUser {
			common.ApiErrorI18n(c, i18n.MsgUserAdminCannotPromote)
			return
		}
		if user.Role == target {
			// 幂等：无需再次写库
			c.JSON(http.StatusOK, gin.H{
				"success": true,
				"message": "",
				"data":    model.User{Role: user.Role, Status: user.Status},
			})
			return
		}
		if !requireManageProof(&target) {
			return
		}
		user.Role = target
	case "add_quota":
		adminName := c.GetString("username")
		adminId := c.GetInt("id")
		adminInfo := map[string]any{
			"admin_id":       adminId,
			"admin_username": adminName,
		}
		switch req.Mode {
		case "add":
			if req.Value <= 0 {
				common.ApiErrorI18n(c, i18n.MsgUserQuotaChangeZero)
				return
			}
			if err := model.IncreaseUserQuota(user.Id, req.Value, true); err != nil {
				common.ApiError(c, err)
				return
			}
			model.RecordLogWithAdminInfo(user.Id, model.LogTypeManage,
				fmt.Sprintf("管理员增加用户额度 %s", logger.LogQuota(req.Value)), adminInfo)
		case "subtract":
			if req.Value <= 0 {
				common.ApiErrorI18n(c, i18n.MsgUserQuotaChangeZero)
				return
			}
			if err := model.DecreaseUserQuota(user.Id, req.Value, true); err != nil {
				common.ApiError(c, err)
				return
			}
			model.RecordLogWithAdminInfo(user.Id, model.LogTypeManage,
				fmt.Sprintf("管理员减少用户额度 %s", logger.LogQuota(req.Value)), adminInfo)
		case "override":
			oldQuota := user.Quota
			if err := model.DB.Model(&model.User{}).Where("id = ?", user.Id).Update("quota", req.Value).Error; err != nil {
				common.ApiError(c, err)
				return
			}
			model.RecordLogWithAdminInfo(user.Id, model.LogTypeManage,
				fmt.Sprintf("管理员覆盖用户额度从 %s 为 %s", logger.LogQuota(oldQuota), logger.LogQuota(req.Value)), adminInfo)
		default:
			common.ApiErrorI18n(c, i18n.MsgInvalidParams)
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "",
		})
		return
	}

	if err := user.Update(false); err != nil {
		common.ApiError(c, err)
		return
	}
	// 禁用 / 角色调整后，强制失效用户缓存与其全部令牌缓存，
	// 避免在 Redis TTL 过期前仍使用旧状态（尤其是禁用后仍可发起请求的问题）。
	// InvalidateUserCache 会让下一次 GetUserCache 从数据库重新加载，
	// InvalidateUserTokensCache 则确保令牌侧的缓存也同步刷新。
	if req.Action == "disable" || req.Action == "delete" {
		revokeUserSessions(c, user.Id, false)
	}
	if req.Action == "disable" || req.Action == "promote" || req.Action == "demote" || req.Action == "set_role" {
		if err := model.InvalidateUserCache(user.Id); err != nil {
			common.SysLog(fmt.Sprintf("failed to invalidate user cache for user %d: %s", user.Id, err.Error()))
		}
		if err := model.InvalidateUserTokensCache(user.Id); err != nil {
			common.SysLog(fmt.Sprintf("failed to invalidate tokens cache for user %d: %s", user.Id, err.Error()))
		}
	}
	clearUser := model.User{
		Role:   user.Role,
		Status: user.Status,
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    clearUser,
	})
	return
}

type emailBindRequest struct {
	Email   string `json:"email"`
	Code    string `json:"code"`
	OldCode string `json:"old_code"`
}

// emailChangeNeedsOldAddressCode reports whether replacing the current email
// must also be confirmed from that address: accounts without 2FA or a Passkey
// verify with the password or a recent sign-in, which a hijacked session could
// satisfy (upstream 0973dc2b8 intent).
func emailChangeNeedsOldAddressCode(user *model.User) (bool, error) {
	if user.Email == "" {
		return false, nil
	}
	state, err := model.GetUserVerificationState(user.Id)
	if err != nil {
		return false, err
	}
	return !state.HasTwoFA && !state.HasPasskey, nil
}

// SendOldEmailVerification sends a confirmation code to the caller's current
// email before it is replaced.
func SendOldEmailVerification(c *gin.Context) {
	user, err := model.GetUserById(c.GetInt("id"), false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if user.Email == "" {
		common.ApiErrorMsg(c, "No email address is bound.")
		return
	}
	code, err := common.GetOrCreateVerificationCodeWithKey(user.Email, common.EmailBindOldAddressPurpose(user.Id), 6)
	if err != nil {
		verificationStoreError(c)
		return
	}
	systemName := common.GetBrandingSettings().SystemName
	subject := fmt.Sprintf("%s邮箱换绑确认", systemName)
	content := fmt.Sprintf("<p>您好，你正在把%s账号的邮箱换绑为新的地址。</p>"+
		"<p>请在换绑页面填写当前邮箱的确认码: <strong>%s</strong></p>"+
		"<p>确认码 %d 分钟内有效。如果不是本人操作，请立即修改密码并开启两步验证。</p>", systemName, code, common.VerificationValidMinutes)
	if err := common.SendEmail(subject, user.Email, content); err != nil {
		common.ApiError(c, err)
		return
	}
	recordUserSecurityAudit(c, user.Id, "user.email_binding_resend", map[string]any{"address": "current"})
	common.ApiSuccess(c, nil)
}

func EmailBind(c *gin.Context) {
	var req emailBindRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiError(c, errors.New("invalid request body"))
		return
	}
	email := req.Email
	email = model.NormalizeEmail(email)
	code := req.Code
	// eflow: 验证码按发码用户隔离，先校验再在写库前原子消费，防止并发重复使用。
	purpose := common.EmailBindVerificationPurpose(c.GetInt("id"))
	valid, err := common.VerifyCodeWithKey(email, code, purpose)
	if err != nil {
		verificationStoreError(c)
		return
	}
	if !valid {
		common.ApiErrorI18n(c, i18n.MsgUserVerificationCodeError)
		return
	}
	user := model.User{
		Id: c.GetInt("id"),
	}
	err = user.FillUserById()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	// eflow: 已有邮箱时换绑等于转移找回密码的通道：没有 2FA/Passkey 的账号先核对发到当前邮箱的
	// 确认码（在消费验证凭证之前，避免填错码浪费凭证），再要绑定新邮箱的一次性验证凭证。
	needsOldCode, err := emailChangeNeedsOldAddressCode(&user)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	oldEmail := user.Email
	oldPurpose := common.EmailBindOldAddressPurpose(user.Id)
	if needsOldCode {
		valid, err := common.VerifyCodeWithKey(oldEmail, req.OldCode, oldPurpose)
		if err != nil {
			verificationStoreError(c)
			return
		}
		if !valid {
			writeSecurityStatusError(c, http.StatusOK, "EMAIL_OLD_CODE_INVALID", "Enter the confirmation code sent to your current email address.")
			return
		}
	}
	if user.Email != "" {
		proofContext, err := common.Marshal(service.AccountBindingContext{Provider: "email", Email: email})
		if err != nil {
			common.ApiError(c, err)
			return
		}
		if middleware.RequireSecurityProof(c, service.VerificationOperation{Scope: service.VerificationScopeAccountBind, Context: proofContext}) == nil {
			return
		}
	}
	valid, err = common.ConsumeVerificationCodeWithKey(email, code, purpose)
	if err != nil {
		verificationStoreError(c)
		return
	}
	if !valid {
		common.ApiErrorI18n(c, i18n.MsgUserVerificationCodeError)
		return
	}
	if needsOldCode {
		if valid, err := common.ConsumeVerificationCodeWithKey(oldEmail, req.OldCode, oldPurpose); err != nil || !valid {
			writeSecurityStatusError(c, http.StatusOK, "EMAIL_OLD_CODE_INVALID", "Enter the confirmation code sent to your current email address.")
			return
		}
	}
	if err := model.BindEmailToUser(&user, email); err != nil {
		if errors.Is(err, model.ErrEmailAlreadyTaken) {
			common.ApiErrorI18n(c, i18n.MsgUserEmailAlreadyTaken)
			return
		}
		common.ApiError(c, err)
		return
	}
	recordUserSecurityAudit(c, user.Id, "user.binding_bind", map[string]any{"provider": "email"})
	if oldEmail != "" && oldEmail != email {
		notifyEmailChanged(oldEmail)
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

type topUpRequest struct {
	Key string `json:"key"`
}

// ponytail: 充值兑换是低频写操作，全局锁换取无泄漏、可直接审计的并发语义。
var topUpLock sync.Mutex

func TopUp(c *gin.Context) {
	if !operation_setting.IsPaymentComplianceConfirmed() {
		common.ApiErrorI18n(c, i18n.MsgPaymentComplianceRequired)
		return
	}

	id := c.GetInt("id")
	if !topUpLock.TryLock() {
		common.ApiErrorI18n(c, i18n.MsgUserTopUpProcessing)
		return
	}
	defer topUpLock.Unlock()
	req := topUpRequest{}
	err := c.ShouldBindJSON(&req)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	quota, err := model.Redeem(req.Key, id)
	if err != nil {
		if errors.Is(err, model.ErrRedeemFailed) {
			common.ApiErrorI18n(c, i18n.MsgRedeemFailed)
			return
		}
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    quota,
	})
}

type UpdateUserSettingRequest struct {
	QuotaWarningType                 string  `json:"notify_type"`
	QuotaWarningThreshold            float64 `json:"quota_warning_threshold"`
	WebhookUrl                       string  `json:"webhook_url,omitempty"`
	WebhookSecret                    string  `json:"webhook_secret,omitempty"`
	NotificationEmail                string  `json:"notification_email,omitempty"`
	BarkUrl                          string  `json:"bark_url,omitempty"`
	GotifyUrl                        string  `json:"gotify_url,omitempty"`
	GotifyToken                      string  `json:"gotify_token,omitempty"`
	GotifyPriority                   int     `json:"gotify_priority,omitempty"`
	UpstreamModelUpdateNotifyEnabled *bool   `json:"upstream_model_update_notify_enabled,omitempty"`
	AcceptUnsetModelRatioModel       bool    `json:"accept_unset_model_ratio_model"`
	RecordIpLog                      bool    `json:"record_ip_log"`
}

func UpdateUserSetting(c *gin.Context) {
	var req UpdateUserSettingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}

	// 验证预警类型
	if req.QuotaWarningType != dto.NotifyTypeEmail && req.QuotaWarningType != dto.NotifyTypeWebhook && req.QuotaWarningType != dto.NotifyTypeBark && req.QuotaWarningType != dto.NotifyTypeGotify {
		common.ApiErrorI18n(c, i18n.MsgSettingInvalidType)
		return
	}

	// 验证预警阈值
	if req.QuotaWarningThreshold <= 0 {
		common.ApiErrorI18n(c, i18n.MsgQuotaThresholdGtZero)
		return
	}

	// 如果是webhook类型,验证webhook地址
	if req.QuotaWarningType == dto.NotifyTypeWebhook {
		if req.WebhookUrl == "" {
			common.ApiErrorI18n(c, i18n.MsgSettingWebhookEmpty)
			return
		}
		// 验证URL格式
		if _, err := url.ParseRequestURI(req.WebhookUrl); err != nil {
			common.ApiErrorI18n(c, i18n.MsgSettingWebhookInvalid)
			return
		}
	}

	// 如果是邮件类型，验证邮箱地址
	if req.QuotaWarningType == dto.NotifyTypeEmail && req.NotificationEmail != "" {
		// 只接受单个裸地址：SMTP 发送按 ; 拆收件人，放行 ; , 会变成群发任意第三方。
		if !isSingleNotificationEmail(req.NotificationEmail) {
			common.ApiErrorI18n(c, i18n.MsgSettingEmailInvalid)
			return
		}
	}

	// 如果是Bark类型，验证Bark URL
	if req.QuotaWarningType == dto.NotifyTypeBark {
		if req.BarkUrl == "" {
			common.ApiErrorI18n(c, i18n.MsgSettingBarkUrlEmpty)
			return
		}
		// 验证URL格式
		if _, err := url.ParseRequestURI(req.BarkUrl); err != nil {
			common.ApiErrorI18n(c, i18n.MsgSettingBarkUrlInvalid)
			return
		}
		// 检查是否是HTTP或HTTPS
		if !strings.HasPrefix(req.BarkUrl, "https://") && !strings.HasPrefix(req.BarkUrl, "http://") {
			common.ApiErrorI18n(c, i18n.MsgSettingUrlMustHttp)
			return
		}
	}

	// 如果是Gotify类型，验证Gotify URL和Token
	if req.QuotaWarningType == dto.NotifyTypeGotify {
		if req.GotifyUrl == "" {
			common.ApiErrorI18n(c, i18n.MsgSettingGotifyUrlEmpty)
			return
		}
		if req.GotifyToken == "" {
			common.ApiErrorI18n(c, i18n.MsgSettingGotifyTokenEmpty)
			return
		}
		// 验证URL格式
		if _, err := url.ParseRequestURI(req.GotifyUrl); err != nil {
			common.ApiErrorI18n(c, i18n.MsgSettingGotifyUrlInvalid)
			return
		}
		// 检查是否是HTTP或HTTPS
		if !strings.HasPrefix(req.GotifyUrl, "https://") && !strings.HasPrefix(req.GotifyUrl, "http://") {
			common.ApiErrorI18n(c, i18n.MsgSettingUrlMustHttp)
			return
		}
	}

	userId := c.GetInt("id")
	user, err := model.GetUserById(userId, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	existingSettings := user.GetSetting()
	upstreamModelUpdateNotifyEnabled := existingSettings.UpstreamModelUpdateNotifyEnabled
	if user.Role >= common.RoleAdminUser && req.UpstreamModelUpdateNotifyEnabled != nil {
		upstreamModelUpdateNotifyEnabled = *req.UpstreamModelUpdateNotifyEnabled
	}

	// 以现有设置为底，只改写通知相关字段，保留侧栏模块、语言等其他设置。
	settings := existingSettings
	settings.NotifyType = req.QuotaWarningType
	settings.QuotaWarningThreshold = req.QuotaWarningThreshold
	settings.UpstreamModelUpdateNotifyEnabled = upstreamModelUpdateNotifyEnabled
	settings.AcceptUnsetRatioModel = req.AcceptUnsetModelRatioModel
	settings.RecordIpLog = req.RecordIpLog
	settings.WebhookUrl = ""
	settings.WebhookSecret = ""
	settings.NotificationEmail = ""
	settings.BarkUrl = ""
	settings.GotifyUrl = ""
	settings.GotifyToken = ""
	settings.GotifyPriority = 0

	// 如果是webhook类型,添加webhook相关设置；密钥留空表示沿用原密钥
	if req.QuotaWarningType == dto.NotifyTypeWebhook {
		settings.WebhookUrl = req.WebhookUrl
		settings.WebhookSecret = req.WebhookSecret
		if req.WebhookSecret == "" {
			settings.WebhookSecret = existingSettings.WebhookSecret
		}
	}

	// 如果提供了通知邮箱，添加到设置中
	if req.QuotaWarningType == dto.NotifyTypeEmail && req.NotificationEmail != "" {
		settings.NotificationEmail = req.NotificationEmail
	}

	// 如果是Bark类型，添加Bark URL到设置中
	if req.QuotaWarningType == dto.NotifyTypeBark {
		settings.BarkUrl = req.BarkUrl
	}

	// 如果是Gotify类型，添加Gotify配置到设置中
	if req.QuotaWarningType == dto.NotifyTypeGotify {
		settings.GotifyUrl = req.GotifyUrl
		settings.GotifyToken = req.GotifyToken
		// Gotify优先级范围0-10，超出范围则使用默认值5
		if req.GotifyPriority < 0 || req.GotifyPriority > 10 {
			settings.GotifyPriority = 5
		} else {
			settings.GotifyPriority = req.GotifyPriority
		}
	}

	// 更新用户设置
	if err := model.UpdateUserSetting(user.Id, settings); err != nil {
		common.ApiErrorI18n(c, i18n.MsgUpdateFailed)
		return
	}

	common.ApiSuccessI18n(c, i18n.MsgSettingSaved, nil)
}

// isSingleNotificationEmail 判断通知邮箱是单个不带显示名的地址，且不含分隔符与控制字符。
func isSingleNotificationEmail(value string) bool {
	if strings.ContainsAny(value, ";,") || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return false
	}
	addr, err := mail.ParseAddress(value)
	return err == nil && addr.Name == "" && addr.Address == value
}

// isHTTPSRequest 判断浏览器侧是否经 HTTPS 访问（直连 TLS 或反代声明的 X-Forwarded-Proto），
// 用于给 e-flowcode-auth cookie 打 Secure；本地 HTTP 开发环境保持可用。
func isHTTPSRequest(c *gin.Context) bool {
	if c.Request.TLS != nil {
		return true
	}
	proto, _, _ := strings.Cut(c.GetHeader("X-Forwarded-Proto"), ",")
	return strings.EqualFold(strings.TrimSpace(proto), "https")
}

// notifyEmailChanged tells the previous address that the account email changed.
func notifyEmailChanged(previous string) {
	systemName := common.GetBrandingSettings().SystemName
	subject := fmt.Sprintf("%s账号邮箱已更换", systemName)
	content := fmt.Sprintf("<p>您好，你的%s账号已换绑到新的邮箱地址。</p>"+
		"<p>如果不是本人操作，请立即联系管理员。</p>", systemName)
	go func() {
		if err := common.SendEmail(subject, previous, content); err != nil {
			common.SysError("notify previous email address: " + err.Error())
		}
	}()
}

func eflowSessionPublicID(sid string) string {
	if sid == "" {
		return ""
	}
	return common.SessionPublicID(sid)
}

// issueEflowAuthCookie sets the sub-site identity cookie bound to the session sid.
func issueEflowAuthCookie(c *gin.Context, userID int, sid string) {
	token, err := common.IssueEflowAuthForSession(userID, eflowSessionPublicID(sid))
	if err != nil || token == "" {
		return
	}
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(common.EflowAuthCookieName, token, int(common.EflowAuthTTLSeconds), "/", "", isHTTPSRequest(c), true)
}
