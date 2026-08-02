export const INVITATION_CODE_STATUS = {
  ENABLED: 1,
  DISABLED: 2,
} as const

export type InvitationCodeStatus =
  (typeof INVITATION_CODE_STATUS)[keyof typeof INVITATION_CODE_STATUS]

export type InvitationCode = {
  id: number
  name: string
  code: string
  status: InvitationCodeStatus
  max_uses: number
  used_count: number
  owner_user_id: number
  created_by: number
  is_admin: boolean
  created_time: number
  expired_time: number
}

export type InvitationCodeUsage = {
  id: number
  user_id: number
  username?: string
  used_time: number
}

export type InvitationCodeListResponse = {
  items: InvitationCode[]
  total: number
  page: number
  page_size?: number
}

export type InvitationCodeSavePayload = {
  id?: number
  name: string
  count?: number
  max_uses: number
  owner_user_id: number
  expired_time: number // unix seconds, 0 = never
  status?: InvitationCodeStatus
}

// 普通用户视角的「我的邀请码」配额信息。
// 字段对应后端 controller/invitation_code.go::invitationCodeQuotaResponse。
export type MyInvitationCodeQuota = {
  // 终身配额：limit=-1 表示无限。
  limit: number
  used: number
  remaining: number // limit=-1 时后端给 -1，前端显示 ∞
  account_age_days: number
  min_account_age_days: number
  default_code_max_uses: number
  default_code_valid_days: number
  // 月度滑动窗口配额（近 30 天）：monthly_limit=0 表示不限制。
  monthly_limit: number
  monthly_used: number
  monthly_remaining: number
  // 窗口内最早一条邀请码 +30d 的 Unix 秒。0 = 未触发 / 不限制。
  monthly_next_reset_at: number
  // 最低累计充值门槛与用户当前累计成功充值（元）；min_topup_money=0 表示策略关闭。
  min_topup_money: number
  user_topup_money: number
  can_generate: boolean
  // 不能生成时的原因（i18n 文案）。
  reason?: string
}
