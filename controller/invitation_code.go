package controller

import (
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func invitationCodeErrorKey(err error) string {
	switch {
	case errors.Is(err, model.ErrInvitationCodeRequired):
		return i18n.MsgInvitationCodeRequired
	case errors.Is(err, model.ErrInvitationCodeInvalid):
		return i18n.MsgInvitationCodeInvalid
	case errors.Is(err, model.ErrInvitationCodeExhausted):
		return i18n.MsgInvitationCodeExhausted
	case errors.Is(err, model.ErrInvitationCodeExpired):
		return i18n.MsgInvitationCodeExpired
	case errors.Is(err, model.ErrInvitationCodeDisabled):
		return i18n.MsgInvitationCodeDisabled
	case errors.Is(err, model.ErrInvitationCodeQuotaExceeded):
		return i18n.MsgInvitationCodeQuotaExceeded
	case errors.Is(err, model.ErrInvitationCodeMonthlyQuotaExceeded):
		return i18n.MsgInvitationCodeMonthlyQuotaExceeded
	case errors.Is(err, model.ErrInvitationCodeAccountTooYoung):
		return i18n.MsgInvitationCodeAccountTooYoung
	case errors.Is(err, model.ErrInvitationCodeTopupNotEnough):
		return i18n.MsgInvitationCodeTopupNotEnough
	case errors.Is(err, model.ErrInvitationCodeUserGenerateDisabled):
		return i18n.MsgInvitationCodeUserGenerateDisabled
	default:
		return ""
	}
}

func handleInvitationCodeError(c *gin.Context, err error) bool {
	if key := invitationCodeErrorKey(err); key != "" {
		common.ApiErrorI18n(c, key)
		return true
	}
	return false
}

type invitationCodeQuotaResponse struct {
	Limit                int    `json:"limit"`
	Used                 int64  `json:"used"`
	Remaining            int    `json:"remaining"`
	AccountAgeDays       int    `json:"account_age_days"`
	MinAccountAgeDays    int    `json:"min_account_age_days"`
	DefaultCodeMaxUses   int    `json:"default_code_max_uses"`
	DefaultCodeValidDays int    `json:"default_code_valid_days"`
	// 月度滑动窗口配额：0 = 不限制，>0 = 上限。
	// MonthlyNextResetAt = 窗口里最早一条邀请码出窗时刻（秒级 Unix 时间戳，0=未触发）。
	MonthlyLimit       int     `json:"monthly_limit"`
	MonthlyUsed        int64   `json:"monthly_used"`
	MonthlyRemaining   int     `json:"monthly_remaining"`
	MonthlyNextResetAt int64   `json:"monthly_next_reset_at"`
	// 最低累计充值门槛与用户当前累计成功充值额；min_topup_money=0 表示策略关闭。
	MinTopupMoney  float64 `json:"min_topup_money"`
	UserTopupMoney float64 `json:"user_topup_money"`
	CanGenerate    bool    `json:"can_generate"`
	Reason         string  `json:"reason,omitempty"`
}

func buildInvitationCodeQuotaResponse(c *gin.Context, user *model.User) (*invitationCodeQuotaResponse, error) {
	quotaInfo, err := model.GetUserInvitationCodeQuotaInfo(user)
	if err != nil {
		return nil, err
	}
	canGenerateErr := model.CanUserGenerateInvitationCode(user, quotaInfo)
	resp := &invitationCodeQuotaResponse{
		Limit:                quotaInfo.Limit,
		Used:                 quotaInfo.Used,
		Remaining:            quotaInfo.Remaining,
		AccountAgeDays:       quotaInfo.AccountAgeDays,
		MinAccountAgeDays:    quotaInfo.MinAccountAgeDays,
		DefaultCodeMaxUses:   quotaInfo.DefaultCodeMaxUses,
		DefaultCodeValidDays: quotaInfo.DefaultCodeValidDays,
		MonthlyLimit:         quotaInfo.MonthlyLimit,
		MonthlyUsed:          quotaInfo.MonthlyUsed,
		MonthlyRemaining:     quotaInfo.MonthlyRemaining,
		MonthlyNextResetAt:   quotaInfo.MonthlyNextResetAt,
		MinTopupMoney:        quotaInfo.MinTopupMoney,
		UserTopupMoney:       quotaInfo.UserTopupMoney,
		CanGenerate:          canGenerateErr == nil,
	}
	// eflow: 客服 (role>=5) 在前端"我的邀请码"页一律显示「无限制」并解锁批量参数。
	// 后端用 -1 同 limit 既有的"无限"约定（前端 renderUnlimitedOr 已识别）。
	if user != nil && user.Role >= common.RoleCustomerServiceUser {
		resp.Limit = -1
		resp.Remaining = -1
		resp.MonthlyLimit = 0
		resp.MonthlyRemaining = -1
		resp.MonthlyNextResetAt = 0
		resp.MinTopupMoney = 0
	}
	if canGenerateErr != nil {
		if key := invitationCodeErrorKey(canGenerateErr); key != "" {
			resp.Reason = i18n.T(c, key)
		}
	}
	return resp, nil
}

func validateInvitationCodeExpiredTime(c *gin.Context, expired int64) (bool, string) {
	if expired != 0 && expired < common.GetTimestamp() {
		return false, i18n.T(c, i18n.MsgInvitationCodeExpired)
	}
	return true, ""
}

func GetAllInvitationCodes(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	invitationCodes, total, err := model.GetAllInvitationCodes(pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(invitationCodes)
	common.ApiSuccess(c, pageInfo)
}

func SearchInvitationCodes(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	keyword := c.Query("keyword")
	invitationCodes, total, err := model.SearchInvitationCodes(keyword, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(invitationCodes)
	common.ApiSuccess(c, pageInfo)
}

func GetInvitationCode(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidId)
		return
	}
	invitationCode, err := model.GetInvitationCodeById(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, invitationCode)
}

func GetInvitationCodeUsages(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidId)
		return
	}
	usages, err := model.GetInvitationCodeUsages(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, usages)
}

func AddInvitationCode(c *gin.Context) {
	invitationCode := model.InvitationCode{}
	if err := common.DecodeJson(c.Request.Body, &invitationCode); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	invitationCode.Name = strings.TrimSpace(invitationCode.Name)
	if utf8.RuneCountInString(invitationCode.Name) == 0 || utf8.RuneCountInString(invitationCode.Name) > 64 {
		common.ApiErrorI18n(c, i18n.MsgInvitationCodeNameLength)
		return
	}
	if invitationCode.Count <= 0 {
		common.ApiErrorI18n(c, i18n.MsgRedemptionCountPositive)
		return
	}
	if invitationCode.Count > 100 {
		common.ApiErrorI18n(c, i18n.MsgInvitationCodeCountMax)
		return
	}
	if invitationCode.MaxUses < 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if valid, msg := validateInvitationCodeExpiredTime(c, invitationCode.ExpiredTime); !valid {
		common.ApiErrorMsg(c, msg)
		return
	}
	if invitationCode.OwnerUserId != 0 {
		if _, err := model.GetUserById(invitationCode.OwnerUserId, false); err != nil {
			common.ApiErrorI18n(c, i18n.MsgUserNotExists)
			return
		}
	}
	keys := make([]string, 0, invitationCode.Count)
	for i := 0; i < invitationCode.Count; i++ {
		cleanInvitationCode := model.InvitationCode{
			Name:        invitationCode.Name,
			Code:        strings.ToUpper(common.GetRandomString(12)),
			Status:      model.InvitationCodeStatusEnabled,
			MaxUses:     invitationCode.MaxUses,
			CreatedBy:   c.GetInt("id"),
			OwnerUserId: invitationCode.OwnerUserId,
			CreatedTime: common.GetTimestamp(),
			ExpiredTime: invitationCode.ExpiredTime,
			IsAdmin:     true,
		}
		if err := cleanInvitationCode.Insert(); err != nil {
			common.ApiErrorI18n(c, i18n.MsgInvitationCodeCreateFailed)
			return
		}
		keys = append(keys, cleanInvitationCode.Code)
	}
	common.ApiSuccess(c, keys)
}

func UpdateInvitationCode(c *gin.Context) {
	invitationCode := model.InvitationCode{}
	if err := common.DecodeJson(c.Request.Body, &invitationCode); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	cleanInvitationCode, err := model.GetInvitationCodeById(invitationCode.Id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	invitationCode.Name = strings.TrimSpace(invitationCode.Name)
	if utf8.RuneCountInString(invitationCode.Name) == 0 || utf8.RuneCountInString(invitationCode.Name) > 64 {
		common.ApiErrorI18n(c, i18n.MsgInvitationCodeNameLength)
		return
	}
	if invitationCode.Status != model.InvitationCodeStatusEnabled && invitationCode.Status != model.InvitationCodeStatusDisabled {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if invitationCode.MaxUses < 0 || (invitationCode.MaxUses > 0 && invitationCode.MaxUses < cleanInvitationCode.UsedCount) {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if valid, msg := validateInvitationCodeExpiredTime(c, invitationCode.ExpiredTime); !valid {
		common.ApiErrorMsg(c, msg)
		return
	}
	if invitationCode.OwnerUserId != 0 {
		if _, err := model.GetUserById(invitationCode.OwnerUserId, false); err != nil {
			common.ApiErrorI18n(c, i18n.MsgUserNotExists)
			return
		}
	}
	cleanInvitationCode.Name = invitationCode.Name
	cleanInvitationCode.Status = invitationCode.Status
	cleanInvitationCode.MaxUses = invitationCode.MaxUses
	cleanInvitationCode.ExpiredTime = invitationCode.ExpiredTime
	cleanInvitationCode.OwnerUserId = invitationCode.OwnerUserId
	if err := cleanInvitationCode.Update(); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, cleanInvitationCode)
}

func DeleteInvitationCode(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidId)
		return
	}
	if err := model.DeleteInvitationCodeById(id); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func DeleteInvalidInvitationCodes(c *gin.Context) {
	rows, err := model.DeleteInvalidInvitationCodes()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, rows)
}

func GetMyInvitationCodes(c *gin.Context) {
	codes, err := model.GetOwnedInvitationCodesByUserId(c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, codes)
}

func GetMyInvitationCodeQuota(c *gin.Context) {
	user, err := model.GetUserById(c.GetInt("id"), false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	quotaInfo, err := buildInvitationCodeQuotaResponse(c, user)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, quotaInfo)
}

func GenerateMyInvitationCode(c *gin.Context) {
	user, err := model.GetUserById(c.GetInt("id"), false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	// eflow: 客服 (role>=5) 可在 body 里带 count/max_uses/expired_time/name 自定义生成；
	// 普通用户路径仍可省略 body（保持原行为：单条、按全局策略）。
	// 客服旁路全部用户侧门槛，故批量参数对其无 quota 限制（仍受单批 100 上限保护）。
	type genBody struct {
		Count       int    `json:"count"`
		MaxUses     *int   `json:"max_uses"`
		ExpiredTime *int64 `json:"expired_time"`
		Name        string `json:"name"`
	}
	var body genBody
	// body 可缺省（普通用户路径）；解析失败按零值兜底，与原调用方等价。
	_ = common.DecodeJson(c.Request.Body, &body)
	isStaff := user != nil && user.Role >= common.RoleCustomerServiceUser
	opts := &model.GenerateUserInvitationCodeOptions{}
	if isStaff {
		// 仅 staff 接受自定义参数；普通用户传了也忽略。
		opts.Count = body.Count
		opts.Name = body.Name
		if body.MaxUses != nil {
			opts.MaxUses = *body.MaxUses
			opts.HasMaxUses = true
		}
		if body.ExpiredTime != nil {
			opts.ExpiredTime = *body.ExpiredTime
			opts.HasExpired = true
			if valid, msg := validateInvitationCodeExpiredTime(c, *body.ExpiredTime); !valid {
				common.ApiErrorMsg(c, msg)
				return
			}
		}
	}
	codes, err := model.GenerateUserInvitationCode(user, opts)
	if err != nil {
		if handleInvitationCodeError(c, err) {
			return
		}
		common.ApiError(c, err)
		return
	}
	if len(codes) == 0 {
		common.ApiErrorI18n(c, i18n.MsgInvitationCodeCreateFailed)
		return
	}
	// 兼容前端契约：单个时回单对象（普通用户路径），批量回数组（客服路径）。
	if len(codes) == 1 && !isStaff {
		common.ApiSuccess(c, codes[0])
		return
	}
	common.ApiSuccess(c, codes)
}
