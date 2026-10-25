package constant

// eflow: 消费日志的计费异常标记（logs.billing_anomaly）。离散值而非位掩码，
// 便于 (billing_anomaly, created_at) 索引等值筛选。0 为正常。
const (
	BillingAnomalyNone            = 0
	BillingAnomalyInputEstimated  = 1 // 上游未返回输入用量，输入按本地估算计费（可能虚高/丢缓存拆分）
	BillingAnomalyOutputEstimated = 2 // 输入用上游真实值，仅输出按已收文本估算（常见于中途断开）
	BillingAnomalyNoUpstreamUsage = 3 // 上游未交付任何用量，按 0 结算免单（仅审核）
)
