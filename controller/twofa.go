package controller

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/middleware"
	"eflow.local/new-api/model"
	"eflow.local/new-api/service"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

// eflow: 登录待 2FA 状态 5 分钟过期，过期需重新登录。连续输错由 TwoFA 自身的失败计数锁定。
const (
	pending2FAUsernameSessionKey  = "pending_username"
	pending2FAUserIDSessionKey    = "pending_user_id"
	pending2FAStartedAtSessionKey = "pending_2fa_at"
	// pending2FALoginMethodSessionKey 记住进入待验证前的登录方式，完成后写入登录审计。
	pending2FALoginMethodSessionKey = "pending_login_method"
	pending2FAMaxAge                = 5 * time.Minute
)

func clearPending2FA(session sessions.Session) {
	session.Delete(pending2FAUsernameSessionKey)
	session.Delete(pending2FAUserIDSessionKey)
	session.Delete(pending2FAStartedAtSessionKey)
	session.Delete(pending2FALoginMethodSessionKey)
	session.Delete(pendingLoginFlowTokenSessionKey)
	session.Delete(loginPasskeyFlowKey)
}

// Setup2FARequest 设置2FA请求结构
type Setup2FARequest struct {
	Code string `json:"code" binding:"required"`
}

// Verify2FARequest 验证2FA请求结构
type Verify2FARequest struct {
	Code string `json:"code" binding:"required"`
}

// Setup2FAResponse 设置2FA响应结构
type Setup2FAResponse struct {
	Secret      string   `json:"secret"`
	QRCodeData  string   `json:"qr_code_data"`
	BackupCodes []string `json:"backup_codes"`
}

// twoFASetupAuthorizationKey keeps the consumed 2fa.setup proof between
// setup and enable, so enabling belongs to the same verified flow.
const twoFASetupAuthorizationKey = "twofa_setup_authorization"

// Setup2FA 初始化2FA设置。需要 2fa.setup 一次性验证凭证。
func Setup2FA(c *gin.Context) {
	userId := c.GetInt("id")
	authorization := middleware.RequireSecurityProof(c, service.VerificationOperation{Scope: service.VerificationScopeTwoFASetup})
	if authorization == nil {
		return
	}

	// 检查用户是否已经启用2FA
	existing, err := model.GetTwoFAByUserId(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if existing != nil && existing.IsEnabled {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "用户已启用2FA，请先禁用后重新设置",
		})
		return
	}

	// 如果存在已禁用的2FA记录，先删除它
	if existing != nil && !existing.IsEnabled {
		if err := existing.Delete(); err != nil {
			common.ApiError(c, err)
			return
		}
		existing = nil // 重置为nil，后续将创建新记录
	}

	// 获取用户信息
	user, err := model.GetUserById(userId, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// 生成TOTP密钥
	key, err := common.GenerateTOTPSecret(user.Username)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "生成2FA密钥失败",
		})
		common.SysLog("生成TOTP密钥失败: " + err.Error())
		return
	}

	// 生成备用码
	backupCodes, err := common.GenerateBackupCodes()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "生成备用码失败",
		})
		common.SysLog("生成备用码失败: " + err.Error())
		return
	}

	// 生成二维码数据
	qrCodeData := common.GenerateQRCodeData(key.Secret(), user.Username)

	// 创建或更新2FA记录（暂未启用）
	twoFA := &model.TwoFA{
		UserId:    userId,
		Secret:    key.Secret(),
		IsEnabled: false,
	}

	if existing != nil {
		// 更新现有记录
		twoFA.Id = existing.Id
		err = twoFA.Update()
	} else {
		// 创建新记录
		err = twoFA.Create()
	}

	if err != nil {
		common.ApiError(c, err)
		return
	}

	// 创建备用码记录
	if err := model.CreateBackupCodes(userId, backupCodes); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "保存备用码失败",
		})
		common.SysLog("保存备用码失败: " + err.Error())
		return
	}

	encodedAuthorization, err := common.Marshal(authorization)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	session := sessions.Default(c)
	session.Set(twoFASetupAuthorizationKey, string(encodedAuthorization))
	if err := session.Save(); err != nil {
		common.ApiError(c, err)
		return
	}

	// 记录操作日志
	model.RecordLog(userId, model.LogTypeSystem, "开始设置两步验证")
	recordUserSecurityAudit(c, userId, "user.2fa_setup", nil)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "2FA设置初始化成功，请使用认证器扫描二维码并输入验证码完成设置",
		"data": Setup2FAResponse{
			Secret:      key.Secret(),
			QRCodeData:  qrCodeData,
			BackupCodes: backupCodes,
		},
	})
}

// Enable2FA 启用2FA
func Enable2FA(c *gin.Context) {
	var req Setup2FARequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "参数错误",
		})
		return
	}

	userId := c.GetInt("id")

	// 获取2FA记录
	twoFA, err := model.GetTwoFAByUserId(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if twoFA == nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "请先完成2FA初始化设置",
		})
		return
	}
	if twoFA.IsEnabled {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "2FA已经启用",
		})
		return
	}
	if !validateTwoFASetupAuthorization(c) {
		return
	}

	// 验证TOTP验证码
	cleanCode, err := common.ValidateNumericCode(req.Code)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	if !common.ValidateTOTPCode(twoFA.Secret, cleanCode) {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "验证码或备用码错误，请重试",
		})
		return
	}

	// 启用2FA
	if err := twoFA.Enable(); err != nil {
		common.ApiError(c, err)
		return
	}

	clearTwoFASetupAuthorization(c)
	// 记录操作日志
	model.RecordLog(userId, model.LogTypeSystem, "成功启用两步验证")
	// A new sign-in factor signs out the account's other browser sessions.
	revokeUserSessions(c, userId, true)
	recordUserSecurityAudit(c, userId, "user.2fa_enable", nil)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "两步验证启用成功",
	})
}

// Disable2FA 禁用2FA。需要 2fa.disable 一次性验证凭证。
func Disable2FA(c *gin.Context) {
	userId := c.GetInt("id")

	// 获取2FA记录
	twoFA, err := model.GetTwoFAByUserId(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if twoFA == nil || !twoFA.IsEnabled {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "用户未启用2FA",
		})
		return
	}
	if middleware.RequireSecurityProof(c, service.VerificationOperation{Scope: service.VerificationScopeTwoFADisable}) == nil {
		return
	}

	// 禁用2FA
	if err := model.DisableTwoFA(userId); err != nil {
		common.ApiError(c, err)
		return
	}

	// 记录操作日志
	model.RecordLog(userId, model.LogTypeSystem, "禁用两步验证")
	revokeUserSessions(c, userId, true)
	recordUserSecurityAudit(c, userId, "user.2fa_disable_self", nil)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "两步验证已禁用",
	})
}

// Get2FAStatus 获取用户2FA状态
func Get2FAStatus(c *gin.Context) {
	userId := c.GetInt("id")

	twoFA, err := model.GetTwoFAByUserId(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	status := map[string]any{
		"enabled": false,
		"locked":  false,
	}

	if twoFA != nil {
		status["enabled"] = twoFA.IsEnabled
		status["locked"] = twoFA.IsLocked()
		if twoFA.IsEnabled {
			// 获取剩余备用码数量
			backupCount, err := model.GetUnusedBackupCodeCount(userId)
			if err != nil {
				common.SysLog("获取备用码数量失败: " + err.Error())
			} else {
				status["backup_codes_remaining"] = backupCount
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    status,
	})
}

// RegenerateBackupCodes 重新生成备用码。需要 2fa.backup_codes.regenerate 一次性验证凭证
// （两步验证码方式只接受 TOTP，不接受即将作废的备用码）。
func RegenerateBackupCodes(c *gin.Context) {
	userId := c.GetInt("id")

	// 获取2FA记录
	twoFA, err := model.GetTwoFAByUserId(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if twoFA == nil || !twoFA.IsEnabled {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "用户未启用2FA",
		})
		return
	}
	if middleware.RequireSecurityProof(c, service.VerificationOperation{Scope: service.VerificationScopeTwoFABackupCodes}) == nil {
		return
	}

	// 生成新的备用码
	backupCodes, err := common.GenerateBackupCodes()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "生成备用码失败",
		})
		common.SysLog("生成备用码失败: " + err.Error())
		return
	}

	// 保存新的备用码
	if err := model.CreateBackupCodes(userId, backupCodes); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "保存备用码失败",
		})
		common.SysLog("保存备用码失败: " + err.Error())
		return
	}

	// 记录操作日志
	model.RecordLog(userId, model.LogTypeSystem, "重新生成两步验证备用码")
	recordUserSecurityAudit(c, userId, "user.2fa_backup_codes", nil)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "备用码重新生成成功",
		"data": map[string]any{
			"backup_codes": backupCodes,
		},
	})
}

// Verify2FALogin 登录时验证2FA。eflow: 保留为 /api/user/login/verify 的 TOTP 别名，
// 只校验本会话中的待验证登录（不需要 flow_token），供旧前端标签页使用。
func Verify2FALogin(c *gin.Context) {
	var req Verify2FARequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "参数错误",
		})
		return
	}
	verifyLoginTwoFactor(c, "", req.Code)
}

// Admin2FAStats 管理员获取2FA统计信息
func Admin2FAStats(c *gin.Context) {
	stats, err := model.GetTwoFAStats()
	if err != nil {
		common.ApiError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    stats,
	})
}

// AdminDisable2FA 管理员强制禁用用户2FA
func AdminDisable2FA(c *gin.Context) {
	userIdStr := c.Param("id")
	userId, err := strconv.Atoi(userIdStr)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "用户ID格式错误",
		})
		return
	}

	// 检查目标用户权限
	targetUser, err := model.GetUserById(userId, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	myRole := c.GetInt("role")
	if !canManageTargetRole(myRole, targetUser.Role) {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无权操作同级或更高级用户的2FA设置",
		})
		return
	}

	if requireAdminUserProof(c, service.VerificationScopeAdminUserTwoFADisable, service.AdminUserContext{UserID: userId}) == nil {
		return
	}

	// 禁用2FA
	if err := model.DisableTwoFA(userId); err != nil {
		if errors.Is(err, model.ErrTwoFANotEnabled) {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "用户未启用2FA",
			})
			return
		}
		common.ApiError(c, err)
		return
	}

	// 记录操作日志：管理员身份通过 admin_info 传递，避免在非管理员可见的日志内容中泄露。
	adminId := c.GetInt("id")
	adminName := c.GetString("username")
	adminInfo := map[string]any{
		"admin_id":       adminId,
		"admin_username": adminName,
	}
	model.RecordLogWithAdminInfo(userId, model.LogTypeManage,
		"管理员强制禁用了用户的两步验证", adminInfo)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "用户2FA已被强制禁用",
	})
}

// validateTwoFASetupAuthorization checks that enabling 2FA continues the setup
// flow whose 2fa.setup proof was consumed by Setup2FA in this session.
func validateTwoFASetupAuthorization(c *gin.Context) bool {
	identity, ok := middleware.GetStepUpIdentity(c)
	raw, _ := sessions.Default(c).Get(twoFASetupAuthorizationKey).(string)
	var authorization service.StepUpAuthorization
	if !ok || raw == "" || common.Unmarshal([]byte(raw), &authorization) != nil {
		writeSecurityStatusError(c, http.StatusOK, "TWOFA_SETUP_INVALID", "Two-factor setup has expired. Start the setup again.")
		return false
	}
	if err := service.ValidateFlowAuthorization(identity, middleware.GetStepUpContext(c), service.VerificationOperation{Scope: service.VerificationScopeTwoFASetup}, &authorization); err != nil {
		writeSecurityOperationError(c, err)
		return false
	}
	return true
}

func clearTwoFASetupAuthorization(c *gin.Context) {
	session := sessions.Default(c)
	session.Delete(twoFASetupAuthorizationKey)
	_ = session.Save()
}
