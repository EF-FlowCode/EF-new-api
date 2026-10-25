package service

import (
	"context"
	"fmt"
	"html"
	"io"
	"strings"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/dto"
	gopool "eflow.local/new-api/internal/drainpool"
	"eflow.local/new-api/model"
	"eflow.local/new-api/service/attachment"
	"eflow.local/new-api/setting"
	"eflow.local/new-api/setting/system_setting"
)

const ticketContentPreviewMaxLen = 500

// 工单状态变更邮件的标题文案。集中在此，便于日后调整或走 i18n。
const (
	TicketStatusReasonGeneric         = "工单状态已更新"
	TicketStatusReasonRefundApproved  = "你的退款申请已通过"
	TicketStatusReasonRefundRejected  = "你的退款申请已被驳回"
	TicketStatusReasonInvoiceIssued   = "发票已开具"
	TicketStatusReasonInvoiceRejected = "发票申请未通过"
)

func ticketStatusLabel(status int) string {
	switch status {
	case model.TicketStatusOpen:
		return "待处理"
	case model.TicketStatusProcessing:
		return "处理中"
	case model.TicketStatusResolved:
		return "已解决"
	case model.TicketStatusClosed:
		return "已关闭"
	default:
		return "未知"
	}
}

func ticketTypeLabel(ticketType string) string {
	switch ticketType {
	case model.TicketTypeGeneral:
		return "普通工单"
	case model.TicketTypeRefund:
		return "退款申请"
	case model.TicketTypeInvoice:
		return "发票申请"
	default:
		return ticketType
	}
}

func ticketPriorityLabel(priority int) string {
	switch priority {
	case 1:
		return "低"
	case 3:
		return "高"
	default:
		return "中"
	}
}

// ticketContentPreview 返回截断后、已 HTML 转义的内容预览
func ticketContentPreview(content string) string {
	trimmed := strings.TrimSpace(content)
	if len([]rune(trimmed)) > ticketContentPreviewMaxLen {
		runes := []rune(trimmed)
		trimmed = string(runes[:ticketContentPreviewMaxLen]) + "..."
	}
	return common.EscapeAndBreak(trimmed)
}

func buildTicketIntro(ticket *model.Ticket, message *model.TicketMessage, isAdmin bool) string {
	if isAdmin {
		if message != nil {
			replyBy := strings.TrimSpace(message.Username)
			if replyBy == "" {
				replyBy = strings.TrimSpace(ticket.Username)
			}
			return fmt.Sprintf("%s 在工单中追加了新的回复。", replyBy)
		}
		return fmt.Sprintf("来自 %s 的一条新工单，等你看看。", strings.TrimSpace(ticket.Username))
	}
	if message != nil {
		return "我们刚刚更新了你的工单，以下是最新进展。"
	}
	return "这是你工单的最新进展。"
}

func ticketLink(serverAddress string) string {
	base := strings.TrimRight(serverAddress, "/")
	if base == "" {
		return ""
	}
	return fmt.Sprintf("%s/ticket", base)
}

// buildTicketVars 准备工单邮件的所有占位变量。所有 value 都已转义为安全 HTML。
func buildTicketVars(ticket *model.Ticket, message *model.TicketMessage, isAdmin bool, heading, serverAddress string) map[string]string {
	createdAt := time.Unix(ticket.CreatedTime, 0).Format("2006-01-02 15:04:05")
	typeLabel := html.EscapeString(ticketTypeLabel(ticket.Type))
	priorityLabel := html.EscapeString(ticketPriorityLabel(ticket.Priority))
	statusLabel := html.EscapeString(ticketStatusLabel(ticket.Status))
	subjectEsc := html.EscapeString(ticket.Subject)
	usernameEsc := html.EscapeString(strings.TrimSpace(ticket.Username))
	createdEsc := html.EscapeString(createdAt)

	rows := []common.EmailTemplateRow{
		{Label: "工单编号", Value: fmt.Sprintf("#%d", ticket.Id)},
		{Label: "主题", Value: subjectEsc},
		{Label: "类型", Value: typeLabel},
		{Label: "优先级", Value: priorityLabel},
		{Label: "当前状态", Value: statusLabel},
		{Label: "提交用户", Value: usernameEsc},
		{Label: "创建时间", Value: createdEsc},
	}

	replyUsername := ""
	replyTime := ""
	previewHTML := ""
	if message != nil {
		replyUsername = strings.TrimSpace(message.Username)
		replyTime = time.Unix(message.CreatedTime, 0).Format("2006-01-02 15:04:05")
		rows = append(rows, common.EmailTemplateRow{
			Label: "最新回复",
			Value: fmt.Sprintf("%s · %s", html.EscapeString(replyUsername), html.EscapeString(replyTime)),
		})
		previewHTML = ticketContentPreview(message.Content)
	}

	actionLabel := "查看工单"
	if isAdmin {
		actionLabel = "前往处理"
	}

	return map[string]string{
		"system_name":           html.EscapeString(common.SystemNameOrDefault()),
		"server_address":        html.EscapeString(strings.TrimRight(serverAddress, "/")),
		"heading":               html.EscapeString(heading),
		"intro":                 html.EscapeString(buildTicketIntro(ticket, message, isAdmin)),
		"ticket_id":             fmt.Sprintf("%d", ticket.Id),
		"ticket_subject":        subjectEsc,
		"ticket_type":           typeLabel,
		"ticket_priority":       priorityLabel,
		"ticket_status":         statusLabel,
		"ticket_username":       usernameEsc,
		"ticket_created_at":     createdEsc,
		"reply_username":        html.EscapeString(replyUsername),
		"reply_time":            html.EscapeString(replyTime),
		"info_table":            common.RenderInfoTableHTML(rows),
		"content_preview":       previewHTML,
		"content_preview_block": common.RenderPreviewBlockHTML("内容预览", previewHTML),
		"action_url":            html.EscapeString(ticketLink(serverAddress)),
		"action_label":          html.EscapeString(actionLabel),
	}
}

// NotifyTicketCreatedToAdmin 异步通知管理员：用户创建了新工单。
// 若工单已完成自动分配，邮件中会附带"已分配给客服 xxx"一行；否则提示"待认领"。
func NotifyTicketCreatedToAdmin(ticket *model.Ticket, message *model.TicketMessage) {
	notificationSettings := common.GetTicketNotificationSettings()
	serverAddress := system_setting.GetServerAddress()
	if ticket == nil {
		return
	}
	if !notificationSettings.Enabled {
		return
	}
	recipients := parseAdminEmails(notificationSettings.AdminEmail)
	if len(recipients) == 0 {
		return
	}
	gopool.Go(func() {
		vars := buildTicketVars(ticket, message, true, "新工单", serverAddress)
		// 叠加分配信息：已分配 -> 显示客服名；未分配 -> "待认领"
		vars["assignee_line"] = html.EscapeString(buildAssigneeLine(ticket))
		subject, body := RenderEmailByKey(constant.EmailTemplateKeyTicketCreatedAdmin, vars)
		if subject == "" || body == "" {
			return
		}
		for _, to := range recipients {
			if err := common.SendEmail(subject, to, body); err != nil {
				common.SysLog(fmt.Sprintf("failed to send ticket-created email to admin %s (ticket=%d): %s", to, ticket.Id, err.Error()))
			}
		}
	})
}

// NotifyTicketAssigned 异步通知被分配到的客服，以及抄送管理员邮箱。
// 触发时机：
//   - 自动分配成功后
//   - 管理员手动指派后（assigneeId 发生变化）
//
// 抄送管理员是为了让整个工单流转对管理员透明，无需额外配置邮箱（客服用自己的邮箱）。
func NotifyTicketAssigned(ticket *model.Ticket, assigneeId int) {
	notificationSettings := common.GetTicketNotificationSettings()
	serverAddress := system_setting.GetServerAddress()
	if ticket == nil || assigneeId <= 0 {
		return
	}
	if !notificationSettings.Enabled {
		return
	}
	gopool.Go(func() {
		assignee, err := model.GetUserById(assigneeId, false)
		if err != nil {
			common.SysLog(fmt.Sprintf("ticket assigned notify: failed to load user %d: %s", assigneeId, err.Error()))
			return
		}
		assigneeSetting := assignee.GetSetting()
		assigneeEmail := ResolveUserNotificationEmail(assignee, assigneeSetting)

		heading := "你有一条新的工单待处理"
		vars := buildTicketVars(ticket, nil, true, heading, serverAddress)
		vars["assignee_line"] = html.EscapeString(buildAssigneeLine(ticket))
		vars["intro"] = html.EscapeString(fmt.Sprintf(
			"工单 #%d「%s」已分配给你，请及时处理。",
			ticket.Id, strings.TrimSpace(ticket.Subject)))

		subject, body := RenderEmailByKey(constant.EmailTemplateKeyTicketAssigned, vars)
		if subject == "" || body == "" {
			// 未配置模板时使用最简备用文案，不让通知丢失
			subject = fmt.Sprintf("[%s] 你有一条新的工单待处理 #%d", common.SystemNameOrDefault(), ticket.Id)
			body = fmt.Sprintf(
				`<p>%s</p><p>工单主题：%s</p><p>请前往管理后台查看并处理。</p>`,
				html.EscapeString(vars["intro"]),
				html.EscapeString(strings.TrimSpace(ticket.Subject)))
		}

		if assigneeEmail != "" {
			if err := common.SendEmail(subject, assigneeEmail, body); err != nil {
				common.SysLog(fmt.Sprintf("failed to send ticket-assigned email to assignee %d (ticket=%d): %s",
					assigneeId, ticket.Id, err.Error()))
			}
		}

		// 同时抄送管理员列表，正文里会包含"已分配给客服 xxx"这一行
		recipients := parseAdminEmails(notificationSettings.AdminEmail)
		for _, to := range recipients {
			if to == assigneeEmail {
				continue
			}
			if err := common.SendEmail(subject, to, body); err != nil {
				common.SysLog(fmt.Sprintf("failed to send ticket-assigned email to admin %s (ticket=%d): %s",
					to, ticket.Id, err.Error()))
			}
		}
	})
}

// buildAssigneeLine 构造 "已分配给客服 xxx" 或 "待认领" 的可读文字。
// 不做 HTML 转义；调用方在写入 vars 时自行 escape。
func buildAssigneeLine(ticket *model.Ticket) string {
	if ticket.AssigneeId <= 0 {
		return "当前状态：待认领"
	}
	assignee, err := model.GetUserById(ticket.AssigneeId, false)
	if err != nil || assignee == nil {
		return fmt.Sprintf("已分配给用户 ID #%d", ticket.AssigneeId)
	}
	name := strings.TrimSpace(assignee.DisplayName)
	if name == "" {
		name = strings.TrimSpace(assignee.Username)
	}
	if name == "" {
		name = fmt.Sprintf("#%d", assignee.Id)
	}
	roleLabel := common.RoleLabel(assignee.Role)
	return fmt.Sprintf("已分配给%s %s", roleLabel, name)
}

// resolveTicketRecipientEmail 决定一封工单邮件的实际收件地址。
//
// eflow: 发票类型工单优先投递到用户在发票申请表单里填写的"发票接收邮箱"
// (TicketInvoice.Email)——企业开票时这通常是财务邮箱，和登录账号邮箱不是一个；
// 取不到发票记录或该字段为空时，回落到账号通知邮箱(ResolveUserNotificationEmail)。
// 其它类型工单维持原账号通知邮箱逻辑。
//
// 注意：调用方若把这里解析出的发票邮箱再交给 NotifyUser，NotifyUser 仍会用
// userSetting.NotificationEmail 重解析覆盖它，因此发票类型工单必须走 common.SendEmail
// 直发路径(见 deliverInvoiceTicketEmail)才能真正投到发票邮箱。
func resolveTicketRecipientEmail(ticket *model.Ticket, user *model.User, userSetting dto.UserSetting) string {
	fallback := ResolveUserNotificationEmail(user, userSetting)
	if ticket == nil || ticket.Type != model.TicketTypeInvoice {
		return fallback
	}
	invoice, err := model.GetTicketInvoiceByTicketId(ticket.Id)
	if err != nil || invoice == nil {
		return fallback
	}
	if e := strings.TrimSpace(invoice.Email); e != "" {
		return e
	}
	return fallback
}

// deliverInvoiceTicketEmail 把一封发票类型工单邮件直发到指定收件地址，绕过 NotifyUser。
// 绕过原因与既有"带附件回复"路径一致：(1) NotifyUser 会用账号 NotificationEmail 覆盖
// 我们特意解析出的发票邮箱；(2) 工单业务邮件频次低，不需要面向系统骚扰邮件的限流；
// (3) 发票文件必须确定送达。atts 非空则随邮件发出，否则纯文本。
func deliverInvoiceTicketEmail(recipient, subject, body string, atts []common.EmailAttachment) error {
	if len(atts) > 0 {
		return common.SendEmailWithAttachments(subject, recipient, body, atts)
	}
	return common.SendEmail(subject, recipient, body)
}

// NotifyTicketReplyToUser 异步通知用户：管理员回复了工单
//
// eflow: 当 TicketReplyAttachmentEnabled 开启、且本条回复带有附件时，会**直接**走
// SendEmailWithAttachments 把附件随邮件一起发出（典型用途：发票工单管理员回复时把发票
// 图片直接发到用户邮箱，省得用户回站点下载）。这条路径绕过 NotifyUser 的统一限流/去重，
// 因为：(1) 工单回复频次本来就低；(2) 限流逻辑面向"系统级骚扰邮件"，不适用于人工触发的
// 业务邮件；(3) 附件加载有 IO 成本，被限流压住反而让用户拿不到发票。
//
// 附件总大小 > TicketReplyAttachmentMaxBytes 时自动回退到不带附件的标准路径，避免邮件被
// SMTP 服务端拒收。
func NotifyTicketReplyToUser(ticket *model.Ticket, message *model.TicketMessage) {
	notificationSettings := common.GetTicketNotificationSettings()
	attachmentSettings := setting.GetTicketAttachmentSettings()
	serverAddress := system_setting.GetServerAddress()
	if ticket == nil || message == nil {
		return
	}
	if !notificationSettings.Enabled {
		return
	}
	gopool.Go(func() {
		user, err := model.GetUserById(ticket.UserId, false)
		if err != nil {
			common.SysLog(fmt.Sprintf("failed to load user %d for ticket reply email (ticket=%d): %s", ticket.UserId, ticket.Id, err.Error()))
			return
		}
		userSetting := user.GetSetting()
		// eflow: 发票工单优先投发票申请填写的邮箱
		userEmail := resolveTicketRecipientEmail(ticket, user, userSetting)
		if userEmail == "" {
			return
		}
		isInvoice := ticket.Type == model.TicketTypeInvoice
		vars := buildTicketVars(ticket, message, false, "你的工单有新回复", serverAddress)
		subject, body := RenderEmailByKey(constant.EmailTemplateKeyTicketReplyUser, vars)
		if subject == "" || body == "" {
			return
		}

		// 尝试加载附件 → 直接发邮件路径。任何一步失败都回退到标准路径。
		if notificationSettings.ReplyAttachmentEnabled {
			if atts := loadReplyEmailAttachments(message.Id, ticket.Id, notificationSettings, attachmentSettings); len(atts) > 0 {
				if err := common.SendEmailWithAttachments(subject, userEmail, body, atts); err != nil {
					common.SysLog(fmt.Sprintf("failed to send ticket reply email with attachments to user %d (ticket=%d): %s", user.Id, ticket.Id, err.Error()))
					// 失败时仍走标准通知路径，让用户至少收到通知
				} else {
					return
				}
			}
		}

		// eflow: 发票工单走直发，保证投到发票邮箱（NotifyUser 会按账号邮箱重解析覆盖）
		if isInvoice {
			if err := deliverInvoiceTicketEmail(userEmail, subject, body, nil); err != nil {
				common.SysLog(fmt.Sprintf("failed to send invoice ticket reply email to %s (ticket=%d): %s", userEmail, ticket.Id, err.Error()))
			}
			return
		}

		notify := dto.NewNotify(dto.NotifyTypeTicketReply, subject, body, nil)
		// 走统一的限流 + 发送通道
		if err := NotifyUser(user.Id, userEmail, userSetting, notify); err != nil {
			common.SysLog(fmt.Sprintf("failed to send ticket reply notification to user %d (ticket=%d): %s", user.Id, ticket.Id, err.Error()))
		}
	})
}

// drainAttachmentsForEmail 把一组附件记录读成内存字节流，准备塞进邮件 multipart。
// 返回 nil 表示"无可发附件 / 存储不可用 / 总体积超 TicketReplyAttachmentMaxBytes"，
// 调用方应回退到不带附件的发送路径。logTag 仅用于日志区分调用场景。
func drainAttachmentsForEmail(records []*model.TicketAttachment, ticketId int, logTag string, notificationSettings common.TicketNotificationSettings, attachmentSettings setting.TicketAttachmentSettings) []common.EmailAttachment {
	if len(records) == 0 {
		return nil
	}
	storage, err := attachment.CurrentWithSettings(attachmentSettings)
	if err != nil {
		common.SysLog(fmt.Sprintf("%s: storage backend unavailable (ticket=%d): %s", logTag, ticketId, err.Error()))
		return nil
	}
	ctx := context.Background()
	out := make([]common.EmailAttachment, 0, len(records))
	var totalBytes int64
	for _, rec := range records {
		reader, err := storage.Get(ctx, rec.StorageKey)
		if err != nil {
			common.SysLog(fmt.Sprintf("%s: read attachment %d failed (ticket=%d): %s", logTag, rec.Id, ticketId, err.Error()))
			continue
		}
		data, err := io.ReadAll(reader)
		_ = reader.Close()
		if err != nil {
			common.SysLog(fmt.Sprintf("%s: drain attachment %d failed (ticket=%d): %s", logTag, rec.Id, ticketId, err.Error()))
			continue
		}
		totalBytes += int64(len(data))
		if notificationSettings.ReplyAttachmentMaxBytes > 0 && totalBytes > notificationSettings.ReplyAttachmentMaxBytes {
			common.SysLog(fmt.Sprintf("%s: total attachment size %d exceeds limit %d, falling back to text-only (ticket=%d)", logTag, totalBytes, notificationSettings.ReplyAttachmentMaxBytes, ticketId))
			return nil
		}
		out = append(out, common.EmailAttachment{
			FileName:    rec.FileName,
			ContentType: rec.MimeType,
			Data:        data,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// loadReplyEmailAttachments 加载某条工单回复消息上的附件。
func loadReplyEmailAttachments(messageId, ticketId int, notificationSettings common.TicketNotificationSettings, attachmentSettings setting.TicketAttachmentSettings) []common.EmailAttachment {
	records, err := model.GetAttachmentsByMessageIds([]int{messageId})
	if err != nil {
		common.SysLog(fmt.Sprintf("ticket reply email: failed to list attachments (message=%d ticket=%d): %s", messageId, ticketId, err.Error()))
		return nil
	}
	return drainAttachmentsForEmail(records, ticketId, "ticket reply email", notificationSettings, attachmentSettings)
}

// loadInvoiceEmailAttachments 加载该工单上所有"客服上传"的附件（即发票文件），
// 供开票通知邮件随附。ownerUserId 为工单提交者，用来排除用户自己上传的文件。
func loadInvoiceEmailAttachments(ticketId, ownerUserId int, notificationSettings common.TicketNotificationSettings, attachmentSettings setting.TicketAttachmentSettings) []common.EmailAttachment {
	records, err := model.GetStaffAttachmentsByTicket(ticketId, ownerUserId)
	if err != nil {
		common.SysLog(fmt.Sprintf("invoice issued email: failed to list staff attachments (ticket=%d): %s", ticketId, err.Error()))
		return nil
	}
	return drainAttachmentsForEmail(records, ticketId, "invoice issued email", notificationSettings, attachmentSettings)
}

// NotifyInvoiceIssuedToUser 异步通知用户：发票已开具。
//
// eflow: 这是"开票"这条业务动作的专用通知，区别于通用状态变更通知：
//   - 收件地址优先用发票申请填写的发票接收邮箱(见 resolveTicketRecipientEmail)；
//   - 把客服上传的发票文件作为附件随邮件一并发出（后端 UpdateInvoiceStatus 已强制
//     开票前必须有发票文件，正常情况下这里一定能取到附件）；
//   - 走 common.SendEmail 直发，绕过 NotifyUser（同 NotifyTicketReplyToUser 的理由：
//     NotifyUser 会按账号邮箱重解析覆盖发票邮箱，且业务邮件不该被反骚扰限流压住）。
//
// 附件加载失败/超限时回退为纯文本通知，确保"已开具"这一事实至少触达用户。
func NotifyInvoiceIssuedToUser(ticket *model.Ticket, prevStatus int) {
	notificationSettings := common.GetTicketNotificationSettings()
	attachmentSettings := setting.GetTicketAttachmentSettings()
	serverAddress := system_setting.GetServerAddress()
	if ticket == nil {
		return
	}
	if !notificationSettings.Enabled {
		return
	}
	gopool.Go(func() {
		user, err := model.GetUserById(ticket.UserId, false)
		if err != nil {
			common.SysLog(fmt.Sprintf("failed to load user %d for invoice issued email (ticket=%d): %s", ticket.UserId, ticket.Id, err.Error()))
			return
		}
		userSetting := user.GetSetting()
		recipient := resolveTicketRecipientEmail(ticket, user, userSetting)
		if recipient == "" {
			return
		}

		vars := buildTicketVars(ticket, nil, false, TicketStatusReasonInvoiceIssued, serverAddress)
		atts := loadInvoiceEmailAttachments(ticket.Id, ticket.UserId, notificationSettings, attachmentSettings)
		if len(atts) > 0 {
			vars["intro"] = html.EscapeString(fmt.Sprintf(
				"你申请的发票已开具，发票文件见本邮件附件。工单「%s」已标记为已解决。",
				strings.TrimSpace(ticket.Subject)))
		} else {
			// 兜底：附件加载失败时明确告知用户发票将另行送达，避免"以为没附件"
			vars["intro"] = html.EscapeString(fmt.Sprintf(
				"你申请的发票已开具，发票文件将另行通过邮件发送给你，请注意查收。工单「%s」已标记为已解决。",
				strings.TrimSpace(ticket.Subject)))
		}
		vars["status_change"] = html.EscapeString(buildStatusChangeDesc(prevStatus, ticket.Status))

		subject, body := RenderEmailByKey(constant.EmailTemplateKeyTicketStatusUser, vars)
		if subject == "" || body == "" {
			return
		}
		if err := deliverInvoiceTicketEmail(recipient, subject, body, atts); err != nil {
			common.SysLog(fmt.Sprintf("failed to send invoice issued email to %s (ticket=%d): %s", recipient, ticket.Id, err.Error()))
			// 带附件失败时再尝试一次纯文本，至少让用户知道已开具
			if len(atts) > 0 {
				if err2 := deliverInvoiceTicketEmail(recipient, subject, body, nil); err2 != nil {
					common.SysLog(fmt.Sprintf("invoice issued email text-only fallback also failed to %s (ticket=%d): %s", recipient, ticket.Id, err2.Error()))
				}
			}
		}
	})
}

// NotifyTicketReplyToAdmin 异步通知管理员：用户在已有工单中追加了回复
func NotifyTicketReplyToAdmin(ticket *model.Ticket, message *model.TicketMessage) {
	notificationSettings := common.GetTicketNotificationSettings()
	serverAddress := system_setting.GetServerAddress()
	if ticket == nil || message == nil {
		return
	}
	if !notificationSettings.Enabled {
		return
	}
	recipients := parseAdminEmails(notificationSettings.AdminEmail)
	if len(recipients) == 0 {
		return
	}
	gopool.Go(func() {
		vars := buildTicketVars(ticket, message, true, "工单有新回复", serverAddress)
		subject, body := RenderEmailByKey(constant.EmailTemplateKeyTicketReplyAdmin, vars)
		if subject == "" || body == "" {
			return
		}
		for _, to := range recipients {
			if err := common.SendEmail(subject, to, body); err != nil {
				common.SysLog(fmt.Sprintf("failed to send ticket-reply email to admin %s (ticket=%d): %s", to, ticket.Id, err.Error()))
			}
		}
	})
}

// buildStatusChangeDesc 构造 "旧状态 → 新状态" 的描述；若 prevStatus <= 0 或与当前相同，则只显示当前状态。
func buildStatusChangeDesc(prevStatus, curStatus int) string {
	cur := ticketStatusLabel(curStatus)
	if prevStatus <= 0 || prevStatus == curStatus {
		return cur
	}
	return fmt.Sprintf("%s → %s", ticketStatusLabel(prevStatus), cur)
}

// NotifyIfTicketStatusChanged 仅在 prevStatus != ticket.Status 时发出状态变更邮件；否则静默返回。
// 用于控制器在每种状态推进（手动调整 / 发票处理 / 退款处理）后统一触达用户，避免重复模板。
func NotifyIfTicketStatusChanged(ticket *model.Ticket, prevStatus int, reason string) {
	if ticket == nil || prevStatus == ticket.Status {
		return
	}
	NotifyTicketStatusChangedToUser(ticket, prevStatus, reason)
}

// NotifyTicketStatusChangedToUser 异步通知用户：工单状态发生变更（手动调整 / 发票处理 / 退款处理）。
//
// prevStatus 为状态变更前的工单主状态（Ticket.Status）；若传入 0 或与当前相同，则邮件中不显示 "→"。
// reason 用于区分触发场景，会拼接到邮件抬头，便于用户快速识别是"人工调整"、"发票已开具"还是"退款已到账"等。
func NotifyTicketStatusChangedToUser(ticket *model.Ticket, prevStatus int, reason string) {
	notificationSettings := common.GetTicketNotificationSettings()
	serverAddress := system_setting.GetServerAddress()
	if ticket == nil {
		return
	}
	if !notificationSettings.Enabled {
		return
	}
	// 状态未发生实际变更时，不再发送邮件，避免骚扰。
	if prevStatus > 0 && prevStatus == ticket.Status {
		return
	}
	gopool.Go(func() {
		user, err := model.GetUserById(ticket.UserId, false)
		if err != nil {
			common.SysLog(fmt.Sprintf("failed to load user %d for ticket status email (ticket=%d): %s", ticket.UserId, ticket.Id, err.Error()))
			return
		}
		userSetting := user.GetSetting()
		// eflow: 发票工单优先投发票申请填写的邮箱
		userEmail := resolveTicketRecipientEmail(ticket, user, userSetting)
		if userEmail == "" {
			return
		}

		heading := "工单状态已更新"
		if r := strings.TrimSpace(reason); r != "" {
			heading = r
		}
		vars := buildTicketVars(ticket, nil, false, heading, serverAddress)
		vars["intro"] = html.EscapeString(fmt.Sprintf("你的工单「%s」状态已更新为「%s」。",
			strings.TrimSpace(ticket.Subject), ticketStatusLabel(ticket.Status)))
		vars["status_change"] = html.EscapeString(buildStatusChangeDesc(prevStatus, ticket.Status))

		subject, body := RenderEmailByKey(constant.EmailTemplateKeyTicketStatusUser, vars)
		if subject == "" || body == "" {
			return
		}
		// eflow: 发票工单走直发，保证投到发票邮箱（NotifyUser 会按账号邮箱重解析覆盖）
		if ticket.Type == model.TicketTypeInvoice {
			if err := deliverInvoiceTicketEmail(userEmail, subject, body, nil); err != nil {
				common.SysLog(fmt.Sprintf("failed to send invoice ticket status email to %s (ticket=%d): %s", userEmail, ticket.Id, err.Error()))
			}
			return
		}
		notify := dto.NewNotify(dto.NotifyTypeTicketStatus, subject, body, nil)
		if err := NotifyUser(user.Id, userEmail, userSetting, notify); err != nil {
			common.SysLog(fmt.Sprintf("failed to send ticket status notification to user %d (ticket=%d): %s", user.Id, ticket.Id, err.Error()))
		}
	})
}

func parseAdminEmails(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	// 支持 ; , 空白 换行作为分隔
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ';' || r == ',' || r == ' ' || r == '\n' || r == '\r' || r == '\t'
	})
	emails := make([]string, 0, len(fields))
	seen := make(map[string]struct{}, len(fields))
	for _, f := range fields {
		e := strings.TrimSpace(f)
		if e == "" {
			continue
		}
		if _, ok := seen[e]; ok {
			continue
		}
		seen[e] = struct{}{}
		emails = append(emails, e)
	}
	return emails
}
