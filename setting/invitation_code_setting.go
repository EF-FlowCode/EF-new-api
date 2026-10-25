package setting

import (
	"strings"

	"eflow.local/new-api/common"
)

type InvitationCodePolicy struct {
	MinAccountAgeDays    int `json:"min_account_age_days"`
	DefaultGenerateQuota int `json:"default_generate_quota"`
	// MonthlyGenerateQuota 限制单个用户最近 30 天内生成的邀请码数量（滑动窗口）。
	// 0 = 不限制，>0 = 上限，-1 = 显式标记不限制（与 0 等价，仅做语义清晰）。
	// 这条配额跟 DefaultGenerateQuota（终身）叠加生效，先撞哪条都拦下。
	MonthlyGenerateQuota int `json:"monthly_generate_quota"`
	// MinTopupMoney 用户能生成邀请码所需的最低累计成功充值金额（按 topup.money 求和）。
	// 0 = 不限制；>0 = 必须累计充值满该金额才解锁。单位与 topup.money 一致（人民币）。
	// 把"成本"从纯账龄换成"真实付费"，链式裂变成本直线上升。
	MinTopupMoney        float64        `json:"min_topup_money"`
	DefaultCodeMaxUses   int            `json:"default_code_max_uses"`
	DefaultCodeValidDays int            `json:"default_code_valid_days"`
	GroupQuotas          map[string]int `json:"group_quotas"`
	RoleQuotas           map[string]int `json:"role_quotas"`
}

// MonthlyQuotaWindowSeconds 月度滑动窗口长度（30 天，秒级）。
const MonthlyQuotaWindowSeconds int64 = 30 * 24 * 60 * 60

var defaultInvitationCodePolicy = InvitationCodePolicy{
	MinAccountAgeDays:    7,
	DefaultGenerateQuota: 5,
	MonthlyGenerateQuota: 0,
	MinTopupMoney:        0,
	DefaultCodeMaxUses:   1,
	DefaultCodeValidDays: 30,
	GroupQuotas: map[string]int{
		"default": 5,
		"vip":     20,
		"premium": 50,
	},
	RoleQuotas: map[string]int{
		"10":  -1,
		"100": -1,
	},
}

var invitationCodePolicy = sanitizeInvitationCodePolicy(defaultInvitationCodePolicy)

func sanitizeInvitationCodePolicy(policy InvitationCodePolicy) InvitationCodePolicy {
	if policy.MinAccountAgeDays < 0 {
		policy.MinAccountAgeDays = 0
	}
	if policy.DefaultGenerateQuota < -1 {
		policy.DefaultGenerateQuota = 0
	}
	if policy.MonthlyGenerateQuota < -1 {
		policy.MonthlyGenerateQuota = 0
	}
	if policy.MinTopupMoney < 0 {
		policy.MinTopupMoney = 0
	}
	if policy.DefaultCodeMaxUses < 0 {
		policy.DefaultCodeMaxUses = 0
	}
	if policy.DefaultCodeValidDays < 0 {
		policy.DefaultCodeValidDays = 0
	}
	if policy.GroupQuotas == nil {
		policy.GroupQuotas = map[string]int{}
	}
	if policy.RoleQuotas == nil {
		policy.RoleQuotas = map[string]int{}
	}
	return policy
}

func ValidateInvitationCodePolicy(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var policy InvitationCodePolicy
	if err := common.UnmarshalJsonStr(raw, &policy); err != nil {
		return err
	}
	_ = sanitizeInvitationCodePolicy(policy)
	return nil
}

func UpdateInvitationCodePolicy(raw string) error {
	if strings.TrimSpace(raw) == "" {
		invitationCodePolicy = sanitizeInvitationCodePolicy(defaultInvitationCodePolicy)
		return nil
	}
	var policy InvitationCodePolicy
	if err := common.UnmarshalJsonStr(raw, &policy); err != nil {
		return err
	}
	invitationCodePolicy = sanitizeInvitationCodePolicy(policy)
	return nil
}

func GetInvitationCodePolicy() InvitationCodePolicy {
	return invitationCodePolicy
}

func GetDefaultInvitationCodePolicyJSON() string {
	bytes, err := common.Marshal(defaultInvitationCodePolicy)
	if err != nil {
		return "{}"
	}
	return string(bytes)
}

func GetInvitationCodePolicyJSON() string {
	bytes, err := common.Marshal(invitationCodePolicy)
	if err != nil {
		return "{}"
	}
	return string(bytes)
}
