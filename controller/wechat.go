package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/middleware"
	"eflow.local/new-api/model"
	"eflow.local/new-api/service"
	"eflow.local/new-api/setting/system_setting"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type wechatLoginResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    string `json:"data"`
}

func getWeChatIdByCode(code string, settings system_setting.LegacyOAuthSettings) (string, error) {
	if code == "" {
		return "", errors.New("无效的参数")
	}
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/api/wechat/user?code=%s", settings.WeChatServerAddress, url.QueryEscape(code)), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", settings.WeChatServerToken)
	client := http.Client{
		Timeout: 5 * time.Second,
	}
	httpResponse, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer httpResponse.Body.Close()
	var res wechatLoginResponse
	err = json.NewDecoder(httpResponse.Body).Decode(&res)
	if err != nil {
		return "", err
	}
	if !res.Success {
		return "", errors.New(res.Message)
	}
	if res.Data == "" {
		return "", errors.New("验证码错误或已过期")
	}
	return res.Data, nil
}

func WeChatAuth(c *gin.Context) {
	settings := system_setting.GetLegacyOAuthSettings()
	authSettings := common.GetAuthSettings()
	userQuotaSettings := common.GetUserQuotaSettings()
	if !settings.WeChatAuthEnabled {
		c.JSON(http.StatusOK, gin.H{
			"message": "管理员未开启通过微信登录以及注册",
			"success": false,
		})
		return
	}
	code := c.Query("code")
	invitationCodeValue := strings.TrimSpace(c.Query("invitation_code"))
	wechatId, err := getWeChatIdByCode(code, settings)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"message": err.Error(),
			"success": false,
		})
		return
	}
	user := model.User{
		WeChatId: wechatId,
	}
	if model.IsWeChatIdAlreadyTaken(wechatId) {
		err := user.FillUserByWeChatId()
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
		if user.Id == 0 {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "用户已注销",
			})
			return
		}
	} else {
		if authSettings.RegistrationEnabled {
			user.Username = "wechat_" + strconv.Itoa(model.GetMaxUserId()+1)
			user.DisplayName = "WeChat User"
			user.Role = common.RoleCommonUser
			user.Status = common.UserStatusEnabled
			inviterId := 0
			err := model.DB.Transaction(func(tx *gorm.DB) error {
				var invitationCode *model.InvitationCode
				if shouldValidateOAuthInvitationCode(invitationCodeValue, authSettings) {
					invitationCodeResult, err := model.GetUsableInvitationCodeWithTx(tx, invitationCodeValue)
					if err != nil {
						return err
					}
					invitationCode = invitationCodeResult
					if invitationCode.OwnerUserId != 0 {
						inviterId = invitationCode.OwnerUserId
					}
				}
				user.InviterId = inviterId
				if err := user.InsertWithTx(tx, inviterId, userQuotaSettings); err != nil {
					return err
				}
				if invitationCode != nil {
					if err := model.ConsumeInvitationCodeWithTx(tx, invitationCode, user.Id, user.Username); err != nil {
						return err
					}
				}
				return nil
			})
			if err != nil {
				if handleInvitationCodeError(c, err) {
					return
				}
				c.JSON(http.StatusOK, gin.H{
					"success": false,
					"message": err.Error(),
				})
				return
			}
			user.FinalizeOAuthUserCreation(inviterId, userQuotaSettings)
		} else {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "管理员关闭了新用户注册",
			})
			return
		}
	}

	if user.Status != common.UserStatusEnabled {
		c.JSON(http.StatusOK, gin.H{
			"message": "用户已被封禁",
			"success": false,
		})
		return
	}
	completeLogin(&user, c, loginMethodWeChat)
}

type wechatBindRequest struct {
	Code string `json:"code"`
}

func WeChatBind(c *gin.Context) {
	settings := system_setting.GetLegacyOAuthSettings()
	if !settings.WeChatAuthEnabled {
		c.JSON(http.StatusOK, gin.H{
			"message": "管理员未开启通过微信登录以及注册",
			"success": false,
		})
		return
	}
	var req wechatBindRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无效的请求",
		})
		return
	}
	code := req.Code
	proofContext, err := common.Marshal(service.AccountBindingContext{Provider: "wechat", Code: code})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if middleware.RequireSecurityProof(c, service.VerificationOperation{Scope: service.VerificationScopeAccountBind, Context: proofContext}) == nil {
		return
	}
	wechatId, err := getWeChatIdByCode(code, settings)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"message": err.Error(),
			"success": false,
		})
		return
	}
	if model.IsWeChatIdAlreadyTaken(wechatId) {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "该微信账号已被绑定",
		})
		return
	}
	session := sessions.Default(c)
	id := session.Get("id")
	userId, ok := id.(int)
	if !ok || userId == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "未登录"})
		return
	}
	// 只更新绑定列，避免完整用户快照覆盖并发的状态变更。
	if err := model.UpdateUserBindColumn(userId, "wechat_id", wechatId); err != nil {
		common.ApiError(c, err)
		return
	}
	recordUserSecurityAudit(c, userId, "user.binding_bind", map[string]any{"provider": "wechat"})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}
