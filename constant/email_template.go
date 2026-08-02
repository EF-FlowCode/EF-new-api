package constant

// EmailTemplateKey 是每种通知邮件的唯一标识。保存到 Option 时统一带前缀：
//
//	EmailTemplate.<key>.subject
//	EmailTemplate.<key>.body
const (
	EmailTemplateKeyTicketCreatedAdmin  = "ticket_created_admin"
	EmailTemplateKeyTicketReplyUser     = "ticket_reply_user"
	EmailTemplateKeyTicketReplyAdmin    = "ticket_reply_admin"
	EmailTemplateKeyTicketStatusUser    = "ticket_status_user"
	EmailTemplateKeyTicketAssigned      = "ticket_assigned"
	EmailTemplateKeyPaymentSuccessUser  = "payment_success_user"
	EmailTemplateKeyPaymentSuccessAdmin = "payment_success_admin"
)

// EmailTemplateOptionPrefix 是存入 OptionMap / DB 时的 Key 前缀
const EmailTemplateOptionPrefix = "EmailTemplate."

// EmailTemplateVariable 描述一个模板变量
type EmailTemplateVariable struct {
	Name        string `json:"name"`        // 变量名，不含 {{}}
	Description string `json:"description"` // 人类可读说明
	Sample      string `json:"sample"`      // 预览时填充的示例值（HTML 安全）
}

// EmailTemplateSpec 每种邮件模板的元信息 + 默认值
type EmailTemplateSpec struct {
	Key             string                  `json:"key"`
	Name            string                  `json:"name"`
	Description     string                  `json:"description"`
	Variables       []EmailTemplateVariable `json:"variables"`
	DefaultSubject  string                  `json:"default_subject"`
	DefaultBody     string                  `json:"default_body"`
}

// 共用变量
var (
	varSystemName = EmailTemplateVariable{Name: "system_name", Description: "系统名称", Sample: "New API"}
	varServerAddr = EmailTemplateVariable{Name: "server_address", Description: "网站地址（ServerAddress）", Sample: "https://example.com"}
	varHeading    = EmailTemplateVariable{Name: "heading", Description: "邮件大标题", Sample: "通知"}
	varInfoTable  = EmailTemplateVariable{Name: "info_table", Description: "由系统拼装的信息表格 HTML（工单字段/订单字段）", Sample: `<table style="width:100%;border-collapse:collapse;font-size:14px;"><tr><td style="padding:10px 0;color:#8e8e93;">示例字段</td><td style="padding:10px 0;color:#1d1d1f;text-align:right;">示例值</td></tr></table>`}
	varPreview    = EmailTemplateVariable{Name: "content_preview", Description: "内容预览区块（工单邮件专用；已转义为安全 HTML，可能为空）", Sample: `<div style="padding:16px;background-color:#f5f5f7;border-radius:10px;color:#1d1d1f;">这里是工单内容预览…</div>`}
	varActionURL  = EmailTemplateVariable{Name: "action_url", Description: "跳转链接", Sample: "https://example.com/ticket"}
	varActionLabel = EmailTemplateVariable{Name: "action_label", Description: "跳转按钮文字", Sample: "前往查看"}
)

// defaultBody 是基础外壳模板，风格参考 Apple 邮件：大量留白、细边、系统字体。
// 管理员可在后台完全覆盖此模板。
const defaultBody = `<div style="background-color:#fafafa;padding:40px 16px;font-family:-apple-system,BlinkMacSystemFont,'SF Pro Text','Helvetica Neue',Arial,'PingFang SC','Microsoft YaHei',sans-serif;color:#1d1d1f;line-height:1.55;">
  <div style="max-width:560px;margin:0 auto;background-color:#ffffff;border:1px solid #ebebeb;border-radius:14px;padding:40px 40px 32px;">
    <h1 style="margin:0 0 8px;font-size:24px;font-weight:600;letter-spacing:-0.01em;color:#1d1d1f;">{{heading}}</h1>
    <p style="margin:0 0 28px;font-size:14px;color:#8e8e93;">{{intro}}</p>
    {{info_table}}
    {{content_preview_block}}
    <div style="margin:32px 0 8px;">
      <a href="{{action_url}}" style="display:inline-block;padding:11px 22px;background-color:#1d1d1f;color:#ffffff;text-decoration:none;border-radius:10px;font-size:14px;font-weight:500;letter-spacing:0.01em;">{{action_label}}</a>
    </div>
  </div>
  <p style="max-width:560px;margin:20px auto 0;text-align:center;color:#a1a1a6;font-size:12px;letter-spacing:0.02em;">— {{system_name}} —</p>
</div>`

// EmailTemplateSpecs 按顺序返回所有可配置邮件模板
func EmailTemplateSpecs() []EmailTemplateSpec {
	return []EmailTemplateSpec{
		{
			Key:         EmailTemplateKeyTicketCreatedAdmin,
			Name:        "工单创建-通知管理员",
			Description: "用户创建新工单时，发送给管理员的邮件。",
			Variables: []EmailTemplateVariable{
				varSystemName, varServerAddr, varHeading,
				{Name: "intro", Description: "正文开头说明", Sample: "来自 alice 的一条新工单，等你看看。"},
				{Name: "ticket_id", Description: "工单编号", Sample: "1024"},
				{Name: "ticket_subject", Description: "工单主题", Sample: "充值没到账"},
				{Name: "ticket_type", Description: "工单类型", Sample: "普通工单"},
				{Name: "ticket_priority", Description: "工单优先级", Sample: "中"},
				{Name: "ticket_status", Description: "工单状态", Sample: "待处理"},
				{Name: "ticket_username", Description: "提交用户名", Sample: "alice"},
				{Name: "ticket_created_at", Description: "创建时间", Sample: "2026-04-17 21:30:00"},
				varInfoTable, varPreview,
				{Name: "content_preview_block", Description: "完整内容预览块（含标题，可能为空）", Sample: `<p style="margin:24px 0 8px;color:#8e8e93;font-size:13px;">内容预览</p><div style="padding:16px;background-color:#f5f5f7;border-radius:10px;color:#1d1d1f;font-size:14px;line-height:1.6;">这里是工单内容…</div>`},
				varActionURL, varActionLabel,
			},
			DefaultSubject: "新工单 #{{ticket_id}}：{{ticket_subject}}",
			DefaultBody:    defaultBody,
		},
		{
			Key:         EmailTemplateKeyTicketReplyUser,
			Name:        "工单回复-通知用户",
			Description: "管理员回复工单后，发送给工单提交用户的邮件。",
			Variables: []EmailTemplateVariable{
				varSystemName, varServerAddr, varHeading,
				{Name: "intro", Description: "正文开头说明", Sample: "我们刚刚更新了你的工单，以下是最新进展。"},
				{Name: "ticket_id", Description: "工单编号", Sample: "1024"},
				{Name: "ticket_subject", Description: "工单主题", Sample: "充值没到账"},
				{Name: "ticket_type", Description: "工单类型", Sample: "普通工单"},
				{Name: "ticket_priority", Description: "工单优先级", Sample: "中"},
				{Name: "ticket_status", Description: "工单状态", Sample: "处理中"},
				{Name: "ticket_username", Description: "提交用户名", Sample: "alice"},
				{Name: "reply_username", Description: "回复人用户名", Sample: "admin"},
				{Name: "reply_time", Description: "回复时间", Sample: "2026-04-17 22:00:00"},
				varInfoTable, varPreview,
				{Name: "content_preview_block", Description: "完整内容预览块", Sample: `<p style="margin:16px 0 4px;color:#333;font-weight:600;">内容预览</p><div style="padding:12px;background-color:#f5f5f5;border-left:3px solid #1890ff;border-radius:4px;">这里是管理员回复…</div>`},
				varActionURL, varActionLabel,
			},
			DefaultSubject: "工单 #{{ticket_id}} 有新回复",
			DefaultBody:    defaultBody,
		},
		{
			Key:         EmailTemplateKeyTicketReplyAdmin,
			Name:        "工单回复-通知管理员",
			Description: "用户回复工单后，发送给管理员的邮件。",
			Variables: []EmailTemplateVariable{
				varSystemName, varServerAddr, varHeading,
				{Name: "intro", Description: "正文开头说明", Sample: "alice 在工单中追问了新问题。"},
				{Name: "ticket_id", Description: "工单编号", Sample: "1024"},
				{Name: "ticket_subject", Description: "工单主题", Sample: "充值没到账"},
				{Name: "ticket_type", Description: "工单类型", Sample: "普通工单"},
				{Name: "ticket_priority", Description: "工单优先级", Sample: "中"},
				{Name: "ticket_status", Description: "工单状态", Sample: "处理中"},
				{Name: "ticket_username", Description: "提交用户名", Sample: "alice"},
				{Name: "reply_username", Description: "回复人用户名", Sample: "alice"},
				{Name: "reply_time", Description: "回复时间", Sample: "2026-04-17 22:00:00"},
				varInfoTable, varPreview,
				{Name: "content_preview_block", Description: "完整内容预览块", Sample: `<p style="margin:16px 0 4px;color:#333;font-weight:600;">内容预览</p><div style="padding:12px;background-color:#f5f5f5;border-radius:4px;">这里是用户回复…</div>`},
				varActionURL, varActionLabel,
			},
			DefaultSubject: "工单 #{{ticket_id}} 用户追加了回复",
			DefaultBody:    defaultBody,
		},
		{
			Key:         EmailTemplateKeyTicketStatusUser,
			Name:        "工单状态变更-通知用户",
			Description: "管理员调整工单状态/优先级，或发票/退款工单进入新的处理阶段时，发送给工单提交用户的邮件。",
			Variables: []EmailTemplateVariable{
				varSystemName, varServerAddr, varHeading,
				{Name: "intro", Description: "正文开头说明", Sample: "你的工单状态已更新。"},
				{Name: "ticket_id", Description: "工单编号", Sample: "1024"},
				{Name: "ticket_subject", Description: "工单主题", Sample: "充值没到账"},
				{Name: "ticket_type", Description: "工单类型", Sample: "普通工单"},
				{Name: "ticket_priority", Description: "工单优先级", Sample: "中"},
				{Name: "ticket_status", Description: "工单状态", Sample: "已解决"},
				{Name: "ticket_username", Description: "提交用户名", Sample: "alice"},
				{Name: "status_change", Description: "状态变更摘要", Sample: "处理中 → 已解决"},
				varInfoTable,
				{Name: "content_preview_block", Description: "（状态变更邮件通常为空）", Sample: ""},
				varActionURL, varActionLabel,
			},
			DefaultSubject: "工单 #{{ticket_id}} 状态已更新为 {{ticket_status}}",
			DefaultBody:    defaultBody,
		},
		{
			Key:         EmailTemplateKeyTicketAssigned,
			Name:        "工单分配-通知客服",
			Description: "工单被自动或手动分配到指定客服/管理员后发送的通知；同时抄送管理员收件邮箱。",
			Variables: []EmailTemplateVariable{
				varSystemName, varServerAddr, varHeading,
				{Name: "intro", Description: "正文开头说明", Sample: "工单 #1024「充值没到账」已分配给你，请及时处理。"},
				{Name: "ticket_id", Description: "工单编号", Sample: "1024"},
				{Name: "ticket_subject", Description: "工单主题", Sample: "充值没到账"},
				{Name: "ticket_type", Description: "工单类型", Sample: "普通工单"},
				{Name: "ticket_priority", Description: "工单优先级", Sample: "中"},
				{Name: "ticket_status", Description: "工单状态", Sample: "待处理"},
				{Name: "ticket_username", Description: "提交用户名", Sample: "alice"},
				{Name: "ticket_created_at", Description: "创建时间", Sample: "2026-04-17 21:30:00"},
				{Name: "assignee_line", Description: "分配情况说明", Sample: "已分配给客服 bob"},
				varInfoTable,
				{Name: "content_preview_block", Description: "（分配通知默认为空）", Sample: ""},
				varActionURL, varActionLabel,
			},
			DefaultSubject: "工单 #{{ticket_id}} 已分配给你",
			DefaultBody:    defaultBody,
		},
		{
			Key:         EmailTemplateKeyPaymentSuccessUser,
			Name:        "支付成功-通知用户",
			Description: "用户在线充值成功时，发送给下单用户的邮件。",
			Variables: []EmailTemplateVariable{
				varSystemName, varServerAddr, varHeading,
				{Name: "intro", Description: "正文开头说明", Sample: "你的充值已经到账，感谢支持。"},
				{Name: "trade_no", Description: "订单编号", Sample: "T20260417223012"},
				{Name: "payment_method", Description: "支付方式", Sample: "易支付"},
				{Name: "money", Description: "支付金额", Sample: "19.90"},
				{Name: "amount", Description: "充值额度", Sample: "500000"},
				{Name: "username", Description: "下单用户名", Sample: "alice"},
				{Name: "completed_at", Description: "完成时间", Sample: "2026-04-17 22:30:12"},
				varInfoTable,
				{Name: "content_preview_block", Description: "（支付邮件默认为空）", Sample: ""},
				varActionURL, varActionLabel,
			},
			DefaultSubject: "充值已到账",
			DefaultBody:    defaultBody,
		},
		{
			Key:         EmailTemplateKeyPaymentSuccessAdmin,
			Name:        "支付成功-通知管理员",
			Description: "用户在线充值成功时，发送给管理员的邮件。",
			Variables: []EmailTemplateVariable{
				varSystemName, varServerAddr, varHeading,
				{Name: "intro", Description: "正文开头说明", Sample: "alice 刚完成了一笔充值。"},
				{Name: "trade_no", Description: "订单编号", Sample: "T20260417223012"},
				{Name: "payment_method", Description: "支付方式", Sample: "易支付"},
				{Name: "money", Description: "支付金额", Sample: "19.90"},
				{Name: "amount", Description: "充值额度", Sample: "500000"},
				{Name: "username", Description: "下单用户名", Sample: "alice"},
				{Name: "user_id", Description: "下单用户 ID", Sample: "42"},
				{Name: "completed_at", Description: "完成时间", Sample: "2026-04-17 22:30:12"},
				varInfoTable,
				{Name: "content_preview_block", Description: "（支付邮件默认为空）", Sample: ""},
				varActionURL, varActionLabel,
			},
			DefaultSubject: "{{username}} 完成了一笔充值",
			DefaultBody:    defaultBody,
		},
	}
}

// FindEmailTemplateSpec 根据 key 返回模板元信息
func FindEmailTemplateSpec(key string) (EmailTemplateSpec, bool) {
	for _, s := range EmailTemplateSpecs() {
		if s.Key == key {
			return s, true
		}
	}
	return EmailTemplateSpec{}, false
}

// EmailTemplateSubjectKey 返回 Option Key
func EmailTemplateSubjectKey(key string) string {
	return EmailTemplateOptionPrefix + key + ".subject"
}

// EmailTemplateBodyKey 返回 Option Key
func EmailTemplateBodyKey(key string) string {
	return EmailTemplateOptionPrefix + key + ".body"
}
