# 额度与推荐奖励设置

管理员从“财务设置 → 额度设置”维护新用户额度、预消费额度、推荐奖励和外部入口。

`AffiliateEnabled` 是推荐计划总开关：

- 开启后，用户端显示推荐入口；只有邀请人存在且已确认支付合规（`IsPaymentComplianceConfirmed`）时，才按 `QuotaForInviter`、`QuotaForInvitee` 发放新奖励。
- 关闭后，用户端隐藏推荐入口，同时停止发放后续邀请奖励。
- 关闭不会删除历史奖励，也不会回收已经到账的额度。

配置继续保存在现有 `options` 表中，不需要数据库迁移。
