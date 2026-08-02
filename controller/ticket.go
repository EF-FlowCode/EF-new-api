package controller

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
)

// validateAttachmentRequest 验证附件 ID 列表是否合法：数量不超过上限、无重复、为正整数。
// 具体归属/绑定状态在 model 层的事务里一并校验。
func validateAttachmentRequest(ids []int) error {
	if len(ids) == 0 {
		return nil
	}
	if !setting.TicketAttachmentEnabled {
		return errors.New("ticket attachment is disabled")
	}
	if len(ids) > setting.TicketAttachmentMaxCount {
		return fmt.Errorf("too many attachments: max %d", setting.TicketAttachmentMaxCount)
	}
	seen := make(map[int]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return errors.New("invalid attachment id")
		}
		if _, ok := seen[id]; ok {
			return errors.New("duplicate attachment id")
		}
		seen[id] = struct{}{}
	}
	return nil
}

type CreateTicketRequest struct {
	Subject       string `json:"subject"`
	Type          string `json:"type"`
	Priority      int    `json:"priority"`
	Content       string `json:"content"`
	AttachmentIds []int  `json:"attachment_ids,omitempty"`
}

type CreateTicketMessageRequest struct {
	Content       string `json:"content"`
	AttachmentIds []int  `json:"attachment_ids,omitempty"`
}

type UpdateTicketStatusRequest struct {
	Status   *int `json:"status,omitempty"`
	Priority *int `json:"priority,omitempty"`
}

// eflow: 工单管理页支持批量勾选后统一改状态（最常见：批量标记已关闭）。
type BatchUpdateTicketStatusRequest struct {
	Ids    []int `json:"ids"`
	Status int   `json:"status"`
}

type CreateInvoiceTicketRequest struct {
	Subject        string `json:"subject"`
	Priority       int    `json:"priority"`
	Content        string `json:"content"`
	CompanyName    string `json:"company_name"`
	TaxNumber      string `json:"tax_number"`
	BankName       string `json:"bank_name"`
	BankAccount    string `json:"bank_account"`
	CompanyAddress string `json:"company_address"`
	CompanyPhone   string `json:"company_phone"`
	Email          string `json:"email"`
	TopUpOrderIds  []int  `json:"topup_order_ids"`
}

type UpdateInvoiceStatusRequest struct {
	InvoiceStatus int `json:"invoice_status"`
}

type CreateRefundTicketRequest struct {
	Subject      string `json:"subject"`
	Priority     int    `json:"priority"`
	RefundQuota  int    `json:"refund_quota"`
	PayeeType    string `json:"payee_type"`
	PayeeName    string `json:"payee_name"`
	PayeeAccount string `json:"payee_account"`
	PayeeBank    string `json:"payee_bank"`
	Contact      string `json:"contact"`
	Reason       string `json:"reason"`
}

type UpdateRefundStatusRequest struct {
	RefundStatus      int    `json:"refund_status"`
	QuotaMode         string `json:"quota_mode"`
	ActualRefundQuota *int   `json:"actual_refund_quota,omitempty"`
}

// eflow: 中国大陆手机号 1[3-9] + 9 位数字。退款只走支付宝，且必须用绑定手机号收款，
// 否则大额转账没法通过实名校验。
var alipayPhoneRegex = regexp.MustCompile(`^1[3-9]\d{9}$`)

func getTicketCurrentUser(c *gin.Context) (*model.User, error) {
	return model.GetUserById(c.GetInt("id"), false)
}

// ensureTicketAccessible 针对"工单处理人"（客服 / 管理员）侧的接口做二次鉴权：
//   - 管理员及以上：全部放行
//   - 客服：只能访问分配给自己的工单，或工单所在组的未分配工单
//
// 不适用于工单提交用户访问自己工单的接口（那一侧走 model.GetUserTicketById）。
// ticketAccessibleByActor 是 ensureTicketAccessible 的纯判定版（不写响应），
// 供批量接口逐工单鉴权时复用——批量里某条没权限只跳过该条，不能中断整批。
func ticketAccessibleByActor(c *gin.Context, ticket *model.Ticket) bool {
	role := c.GetInt("role")
	if role >= common.RoleAdminUser {
		return true
	}
	currentUserId := c.GetInt("id")
	// 自己是负责人 —— 最常见路径
	if ticket.AssigneeId == currentUserId {
		return true
	}
	// 同组未分配池 —— 允许客服抢单/回复
	if ticket.AssigneeId == 0 {
		pool := service.ResolveAssigneeIdsForUser(currentUserId, role)
		for _, uid := range pool {
			if uid == currentUserId {
				return true
			}
		}
	}
	return false
}

func ensureTicketAccessible(c *gin.Context, ticket *model.Ticket) bool {
	if ticketAccessibleByActor(c, ticket) {
		return true
	}
	common.ApiErrorI18n(c, i18n.MsgForbidden)
	return false
}

func parseTicketID(c *gin.Context) (int, bool) {
	ticketId, err := strconv.Atoi(c.Param("id"))
	if err != nil || ticketId <= 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidId)
		return 0, false
	}
	return ticketId, true
}

func normalizeTicketTypeOrError(c *gin.Context, rawType string) (string, bool) {
	ticketType := model.NormalizeTicketType(rawType)
	if ticketType == "" {
		return "", true
	}
	if !model.IsValidTicketType(ticketType) {
		common.ApiErrorI18n(c, i18n.MsgTicketInvalidType)
		return "", false
	}
	return ticketType, true
}

func handleTicketError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, model.ErrTicketSubjectEmpty):
		common.ApiErrorI18n(c, i18n.MsgTicketSubjectEmpty)
	case errors.Is(err, model.ErrTicketContentEmpty):
		common.ApiErrorI18n(c, i18n.MsgTicketContentEmpty)
	case errors.Is(err, model.ErrTicketNotFound):
		common.ApiErrorI18n(c, i18n.MsgTicketNotFound)
	case errors.Is(err, model.ErrTicketForbidden):
		common.ApiErrorI18n(c, i18n.MsgForbidden)
	case errors.Is(err, model.ErrTicketClosed):
		common.ApiErrorI18n(c, i18n.MsgTicketClosed)
	case errors.Is(err, model.ErrTicketInvalidStatus):
		common.ApiErrorI18n(c, i18n.MsgTicketInvalidStatus)
	case errors.Is(err, model.ErrTicketInvalidType):
		common.ApiErrorI18n(c, i18n.MsgTicketInvalidType)
	case errors.Is(err, model.ErrTicketInvoiceNotFound):
		common.ApiErrorI18n(c, i18n.MsgTicketInvoiceNotFound)
	case errors.Is(err, model.ErrTicketInvoiceStatusInvalid):
		common.ApiErrorI18n(c, i18n.MsgTicketInvoiceStatusInvalid)
	case errors.Is(err, model.ErrTicketInvoiceOrderEmpty):
		common.ApiErrorI18n(c, i18n.MsgTicketInvoiceOrderEmpty)
	case errors.Is(err, model.ErrTicketInvoiceOrderInvalid):
		common.ApiErrorI18n(c, i18n.MsgTicketInvoiceOrderInvalid)
	case errors.Is(err, model.ErrTicketInvoiceOrderDuplicate):
		common.ApiErrorI18n(c, i18n.MsgTicketInvoiceOrderDuplicate)
	case errors.Is(err, model.ErrTicketInvoiceCompanyEmpty):
		common.ApiErrorI18n(c, i18n.MsgTicketInvoiceCompanyEmpty)
	case errors.Is(err, model.ErrTicketInvoiceTaxNumberEmpty):
		common.ApiErrorI18n(c, i18n.MsgTicketInvoiceTaxNumberEmpty)
	case errors.Is(err, model.ErrTicketInvoiceEmailEmpty):
		common.ApiErrorI18n(c, i18n.MsgTicketInvoiceEmailEmpty)
	case errors.Is(err, model.ErrTicketInvoiceAttachmentRequired):
		common.ApiErrorI18n(c, i18n.MsgTicketInvoiceAttachmentRequired)
	case errors.Is(err, model.ErrTicketRefundNotFound):
		common.ApiErrorI18n(c, i18n.MsgTicketRefundNotFound)
	case errors.Is(err, model.ErrTicketRefundStatusInvalid):
		common.ApiErrorI18n(c, i18n.MsgTicketRefundStatusInvalid)
	case errors.Is(err, model.ErrTicketRefundQuotaInvalid):
		common.ApiErrorI18n(c, i18n.MsgTicketRefundQuotaInvalid)
	case errors.Is(err, model.ErrTicketRefundQuotaExceed):
		common.ApiErrorI18n(c, i18n.MsgTicketRefundQuotaExceed)
	case errors.Is(err, model.ErrTicketRefundPayeeTypeEmpty):
		common.ApiErrorI18n(c, i18n.MsgTicketRefundPayeeTypeEmpty)
	case errors.Is(err, model.ErrTicketRefundPayeeNameEmpty):
		common.ApiErrorI18n(c, i18n.MsgTicketRefundPayeeNameEmpty)
	case errors.Is(err, model.ErrTicketRefundPayeeAccountEmpty):
		common.ApiErrorI18n(c, i18n.MsgTicketRefundPayeeAccountEmpty)
	case errors.Is(err, model.ErrTicketRefundPayeeBankEmpty):
		common.ApiErrorI18n(c, i18n.MsgTicketRefundPayeeBankEmpty)
	case errors.Is(err, model.ErrTicketRefundContactEmpty):
		common.ApiErrorI18n(c, i18n.MsgTicketRefundContactEmpty)
	case errors.Is(err, model.ErrTicketRefundNotPending):
		common.ApiErrorI18n(c, i18n.MsgTicketRefundNotPending)
	case errors.Is(err, model.ErrTicketRefundQuotaModeInvalid):
		common.ApiErrorI18n(c, i18n.MsgTicketRefundQuotaModeInvalid)
	case errors.Is(err, model.ErrTicketAssigneeInvalid):
		common.ApiErrorI18n(c, i18n.MsgTicketAssigneeInvalid)
	case errors.Is(err, model.ErrAttachmentNotFound):
		common.ApiErrorMsg(c, "attachment not found")
	case errors.Is(err, model.ErrAttachmentForbidden):
		common.ApiErrorMsg(c, "attachment belongs to another user")
	case errors.Is(err, model.ErrAttachmentBound):
		common.ApiErrorMsg(c, "attachment already bound")
	case errors.Is(err, model.ErrAttachmentBindTicket):
		common.ApiErrorMsg(c, "attachment belongs to another ticket")
	default:
		common.ApiError(c, err)
	}
}

func buildTicketDetailResponse(ticket *model.Ticket) (gin.H, error) {
	messages, err := model.GetTicketMessagesWithAttachments(ticket.Id)
	if err != nil {
		return nil, err
	}

	resp := gin.H{
		"ticket":         ticket,
		"messages":       messages,
		"invoice":        nil,
		"invoice_orders": []*model.TopUp{},
		"refund":         nil,
	}

	if ticket.Type == model.TicketTypeInvoice {
		invoice, orders, err := model.GetTicketInvoiceDetail(ticket.Id)
		if err != nil && !errors.Is(err, model.ErrTicketInvoiceNotFound) {
			return nil, err
		}
		if err == nil {
			resp["invoice"] = invoice
			resp["invoice_orders"] = orders
		}
	}
	if ticket.Type == model.TicketTypeRefund {
		refund, err := model.GetTicketRefundByTicketId(ticket.Id)
		if err != nil && !errors.Is(err, model.ErrTicketRefundNotFound) {
			return nil, err
		}
		if err == nil {
			resp["refund"] = refund
		}
	}
	return resp, nil
}

func CreateTicket(c *gin.Context) {
	var req CreateTicketRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if err := validateAttachmentRequest(req.AttachmentIds); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}

	currentUser, err := getTicketCurrentUser(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	ticket, message, err := model.CreateTicketWithMessage(model.CreateTicketParams{
		UserId:        currentUser.Id,
		Username:      currentUser.Username,
		Subject:       req.Subject,
		Type:          req.Type,
		Priority:      req.Priority,
		Content:       req.Content,
		Role:          currentUser.Role,
		AttachmentIds: req.AttachmentIds,
	})
	if err != nil {
		handleTicketError(c, err)
		return
	}
	// 自动分配 -> 先分配再发"已创建"通知，邮件里能带上"已分配给 xxx"一行。
	service.AutoAssignTicketOnCreate(ticket.Id, ticket.Type, service.NotifyTicketAssigned)
	if refreshed, refreshErr := model.GetTicketById(ticket.Id); refreshErr == nil {
		ticket = refreshed
	}
	service.NotifyTicketCreatedToAdmin(ticket, message)
	common.ApiSuccess(c, gin.H{
		"ticket":  ticket,
		"message": message,
	})
}

func GetUserTickets(c *gin.Context) {
	ticketType, ok := normalizeTicketTypeOrError(c, c.Query("type"))
	if !ok {
		return
	}
	status, _ := strconv.Atoi(c.Query("status"))
	pageInfo := common.GetPageQuery(c)

	tickets, total, err := model.ListTickets(model.TicketQueryOptions{
		UserId:    c.GetInt("id"),
		Status:    status,
		Type:      ticketType,
		OrderBy:   c.Query("order_by"),
		OrderDesc: !strings.EqualFold(c.Query("order"), "asc"),
	}, pageInfo)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(tickets)
	common.ApiSuccess(c, pageInfo)
}

func GetUserTicket(c *gin.Context) {
	ticketId, ok := parseTicketID(c)
	if !ok {
		return
	}
	ticket, err := model.GetUserTicketById(ticketId, c.GetInt("id"))
	if err != nil {
		handleTicketError(c, err)
		return
	}
	resp, err := buildTicketDetailResponse(ticket)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, resp)
}

func CreateUserTicketMessage(c *gin.Context) {
	ticketId, ok := parseTicketID(c)
	if !ok {
		return
	}

	var req CreateTicketMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if err := validateAttachmentRequest(req.AttachmentIds); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}

	currentUser, err := getTicketCurrentUser(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if _, err = model.GetUserTicketById(ticketId, currentUser.Id); err != nil {
		handleTicketError(c, err)
		return
	}

	message, ticket, _, err := model.AddTicketMessage(
		ticketId,
		currentUser.Id,
		currentUser.Username,
		currentUser.Role,
		req.Content,
		req.AttachmentIds,
	)
	if err != nil {
		handleTicketError(c, err)
		return
	}
	// 用户追加回复：仅提醒管理员有新消息。
	// 用户自己触发的"已解决 → 处理中"是用户自己刚刚的操作，对用户端不再重复发邮件，避免骚扰。
	service.NotifyTicketReplyToAdmin(ticket, message)
	common.ApiSuccess(c, gin.H{
		"ticket":  ticket,
		"message": message,
	})
}

func CloseUserTicket(c *gin.Context) {
	ticketId, ok := parseTicketID(c)
	if !ok {
		return
	}
	ticket, err := model.CloseUserTicket(ticketId, c.GetInt("id"))
	if err != nil {
		handleTicketError(c, err)
		return
	}
	common.ApiSuccess(c, ticket)
}

// GetAllTickets 管理员/客服视角的工单列表。
// 可见范围：
//   - 管理员及以上：全部工单；支持 scope=mine / scope=unassigned 过滤
//   - 客服：自己被分配的工单 + 同组待认领工单；scope=mine 仅自己的；scope=unassigned 仅本组未分配
func GetAllTickets(c *gin.Context) {
	ticketType, ok := normalizeTicketTypeOrError(c, c.Query("type"))
	if !ok {
		return
	}
	status, _ := strconv.Atoi(c.Query("status"))
	scope := strings.ToLower(strings.TrimSpace(c.Query("scope"))) // "" / mine / unassigned
	pageInfo := common.GetPageQuery(c)

	opts := model.TicketQueryOptions{
		Status:      status,
		Type:        ticketType,
		Keyword:     c.Query("keyword"),
		CompanyName: c.Query("company_name"),
		OrderBy:     c.Query("order_by"),
		// order 缺省或非 asc 一律按降序，配合空/非法 order_by 时仍回退到历史默认排序
		OrderDesc: !strings.EqualFold(c.Query("order"), "asc"),
	}

	role := c.GetInt("role")
	currentUserId := c.GetInt("id")
	if role >= common.RoleAdminUser {
		// 管理员视角：默认看全部；scope=mine 只看自己处理的；scope=unassigned 只看待认领池
		switch scope {
		case "mine":
			opts.AssigneeId = currentUserId
		case "unassigned":
			opts.AssigneeId = -1
		}
	} else {
		// 客服视角（role=5）：
		//   - 默认 / scope=mine：只看分配给自己的工单（和用户侧"我的工单"是对称语义）
		//   - scope=unassigned：只看自己负责的那些"类型"里的待认领工单；
		//     这里按 type 过滤而不是按 assignee 成员列表，因为未分配工单的 assignee_id=0
		//     无法携带分组信息，更自然的归属是"工单类型 → 规则 → 客服"。
		// 不再提供"看到所有工单"的视角，避免越权与视觉噪音。
		switch scope {
		case "unassigned":
			opts.AssigneeId = -1
			myTypes := setting.TicketTypesForUser(currentUserId)
			if opts.Type == "" && len(myTypes) > 0 {
				opts.TypeIn = myTypes
			}
			if len(myTypes) == 0 {
				// 未加入任何分组的客服 —— 没有可认领的工单，强制返回空结果。
				opts.TypeIn = []string{"__none__"}
			}
		default:
			opts.AssigneeId = currentUserId
		}
	}

	tickets, total, err := model.ListTickets(opts, pageInfo)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(tickets)
	common.ApiSuccess(c, pageInfo)
}

// AssignTicket 管理员/客服主动指派或认领一张工单。
// Body: { "assignee_id": 0|<user_id>, "expected_assignee_id": null|<int> }
// assignee_id=0 表示取消分配、放回待认领池（仅管理员可做）。
// expected_assignee_id 用于乐观锁：客户端上一次看到的 assignee，用来避免并发覆盖。
func AssignTicket(c *gin.Context) {
	ticketId, ok := parseTicketID(c)
	if !ok {
		return
	}
	var req struct {
		AssigneeId         int  `json:"assignee_id"`
		ExpectedAssigneeId *int `json:"expected_assignee_id,omitempty"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if req.AssigneeId < 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}

	role := c.GetInt("role")
	currentUserId := c.GetInt("id")

	// 权限校验：
	// - 管理员可以指派任意人 / 取消分配
	// - 客服只能把工单分配给自己（"认领"）或在同组内转派，且不能取消分配
	if role < common.RoleAdminUser {
		if req.AssigneeId == 0 {
			common.ApiErrorI18n(c, i18n.MsgForbidden)
			return
		}
		allowed := req.AssigneeId == currentUserId
		if !allowed {
			pool := service.ResolveAssigneeIdsForUser(currentUserId, role)
			for _, uid := range pool {
				if uid == req.AssigneeId {
					allowed = true
					break
				}
			}
		}
		if !allowed {
			common.ApiErrorI18n(c, i18n.MsgForbidden)
			return
		}
	}

	// 目标用户必须是有效的客服或管理员（assignee_id=0 时跳过校验）
	if req.AssigneeId > 0 {
		target, err := model.GetUserById(req.AssigneeId, false)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		if target == nil || target.Status != common.UserStatusEnabled || target.Role < common.RoleCustomerServiceUser {
			common.ApiErrorMsg(c, "assignee must be an active customer service or admin user")
			return
		}
	}

	ticket, prevAssign, err := model.AssignTicket(ticketId, req.AssigneeId, req.ExpectedAssigneeId)
	if err != nil {
		handleTicketError(c, err)
		return
	}
	// 分配人确实变化时才发通知（减少噪音）。
	if prevAssign != req.AssigneeId && req.AssigneeId > 0 {
		service.NotifyTicketAssigned(ticket, req.AssigneeId)
	}
	common.ApiSuccess(c, gin.H{"ticket": ticket})
}

// ListTicketStaff 返回可作为工单处理人的账号列表（客服 + 管理员 + 超级管理员）。
// 给前端分配下拉选择使用。仅返回非禁用账号。
func ListTicketStaff(c *gin.Context) {
	users, err := model.GetTicketStaffUsers()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, users)
}

func GetTicket(c *gin.Context) {
	ticketId, ok := parseTicketID(c)
	if !ok {
		return
	}
	ticket, err := model.GetTicketById(ticketId)
	if err != nil {
		handleTicketError(c, err)
		return
	}
	if !ensureTicketAccessible(c, ticket) {
		return
	}
	resp, err := buildTicketDetailResponse(ticket)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, resp)
}

func CreateAdminTicketMessage(c *gin.Context) {
	ticketId, ok := parseTicketID(c)
	if !ok {
		return
	}

	var req CreateTicketMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if err := validateAttachmentRequest(req.AttachmentIds); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}

	currentUser, err := getTicketCurrentUser(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// 客服视角：确认有权限处理这张工单（要么是负责人，要么在同组待认领池）。
	existing, err := model.GetTicketById(ticketId)
	if err != nil {
		handleTicketError(c, err)
		return
	}
	if !ensureTicketAccessible(c, existing) {
		return
	}

	message, ticket, _, err := model.AddTicketMessage(
		ticketId,
		currentUser.Id,
		currentUser.Username,
		currentUser.Role,
		req.Content,
		req.AttachmentIds,
	)
	if err != nil {
		handleTicketError(c, err)
		return
	}
	// 管理员回复会自动将 Open -> Processing，此时用户端收到的"回复"邮件已能反映最新状态，
	// 故不再额外发一封"状态变更"邮件，避免同一事件产生两封邮件。
	service.NotifyTicketReplyToUser(ticket, message)
	common.ApiSuccess(c, gin.H{
		"ticket":  ticket,
		"message": message,
	})
}

func UpdateTicketStatus(c *gin.Context) {
	ticketId, ok := parseTicketID(c)
	if !ok {
		return
	}

	var req UpdateTicketStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if req.Status == nil && req.Priority == nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}

	existing, err := model.GetTicketById(ticketId)
	if err != nil {
		handleTicketError(c, err)
		return
	}
	if !ensureTicketAccessible(c, existing) {
		return
	}

	ticket, prevStatus, err := model.UpdateTicketStatus(ticketId, c.GetInt("id"), req.Status, req.Priority)
	if err != nil {
		handleTicketError(c, err)
		return
	}
	// 只改优先级时不触发状态变更通知，避免骚扰。
	if req.Status != nil {
		service.NotifyIfTicketStatusChanged(ticket, prevStatus, service.TicketStatusReasonGeneric)
	}
	common.ApiSuccess(c, ticket)
}

// batchTicketStatusLimit 单次批量改状态的工单数上限，防止误操作/恶意一把刷全表。
const batchTicketStatusLimit = 200

// BatchUpdateTicketStatus 批量把选中的工单改成同一状态（典型场景：批量关闭）。
// 逐条复用 model.UpdateTicketStatus，单条失败/无权限只跳过并计入 failed，不中断整批；
// 状态真正变化的工单照常触发通知，与单条接口行为一致。
func BatchUpdateTicketStatus(c *gin.Context) {
	var req BatchUpdateTicketStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if len(req.Ids) == 0 || !model.IsValidTicketStatus(req.Status) {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}

	// 去重 + 限量，保留请求顺序
	seen := make(map[int]struct{}, len(req.Ids))
	ids := make([]int, 0, len(req.Ids))
	for _, id := range req.Ids {
		if id <= 0 {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if len(ids) > batchTicketStatusLimit {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}

	status := req.Status
	adminId := c.GetInt("id")
	updated := make([]int, 0, len(ids))
	failed := make([]int, 0)

	for _, id := range ids {
		existing, err := model.GetTicketById(id)
		if err != nil {
			failed = append(failed, id)
			continue
		}
		if !ticketAccessibleByActor(c, existing) {
			failed = append(failed, id)
			continue
		}
		ticket, prevStatus, err := model.UpdateTicketStatus(id, adminId, &status, nil)
		if err != nil {
			failed = append(failed, id)
			continue
		}
		service.NotifyIfTicketStatusChanged(ticket, prevStatus, service.TicketStatusReasonGeneric)
		updated = append(updated, id)
	}

	common.ApiSuccess(c, gin.H{
		"updated":       len(updated),
		"updated_ids":   updated,
		"failed":        len(failed),
		"failed_ids":    failed,
		"requested":     len(ids),
	})
}

func GetEligibleInvoiceOrders(c *gin.Context) {
	topUps, err := model.GetEligibleInvoiceOrders(c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, topUps)
}

func CreateInvoiceTicket(c *gin.Context) {
	var req CreateInvoiceTicketRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}

	currentUser, err := getTicketCurrentUser(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	subject := strings.TrimSpace(req.Subject)
	if subject == "" {
		subject = fmt.Sprintf("发票申请（%d 笔订单）", len(req.TopUpOrderIds))
	}

	ticket, invoice, message, orders, err := model.CreateInvoiceTicket(model.CreateInvoiceTicketParams{
		UserId:         currentUser.Id,
		Username:       currentUser.Username,
		Subject:        subject,
		Priority:       req.Priority,
		Content:        req.Content,
		CompanyName:    req.CompanyName,
		TaxNumber:      req.TaxNumber,
		BankName:       req.BankName,
		BankAccount:    req.BankAccount,
		CompanyAddress: req.CompanyAddress,
		CompanyPhone:   req.CompanyPhone,
		Email:          req.Email,
		TopUpOrderIds:  req.TopUpOrderIds,
	})
	if err != nil {
		// eflow: 未达最低开票金额——带阈值文案，让用户知道差多少
		if errors.Is(err, model.ErrTicketInvoiceAmountTooLow) {
			common.ApiErrorI18n(c, i18n.MsgTicketInvoiceAmountTooLow, map[string]any{
				"Min": strconv.FormatFloat(common.TicketInvoiceMinTopupMoney, 'f', -1, 64),
			})
			return
		}
		handleTicketError(c, err)
		return
	}

	service.AutoAssignTicketOnCreate(ticket.Id, ticket.Type, service.NotifyTicketAssigned)
	if refreshed, refreshErr := model.GetTicketById(ticket.Id); refreshErr == nil {
		ticket = refreshed
	}
	service.NotifyTicketCreatedToAdmin(ticket, message)
	common.ApiSuccess(c, gin.H{
		"ticket":         ticket,
		"invoice":        invoice,
		"message":        message,
		"invoice_orders": orders,
	})
}

func GetTicketInvoice(c *gin.Context) {
	ticketId, ok := parseTicketID(c)
	if !ok {
		return
	}
	existing, err := model.GetTicketById(ticketId)
	if err != nil {
		handleTicketError(c, err)
		return
	}
	if !ensureTicketAccessible(c, existing) {
		return
	}
	invoice, orders, err := model.GetTicketInvoiceDetail(ticketId)
	if err != nil {
		handleTicketError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"invoice": invoice,
		"orders":  orders,
	})
}

func CreateRefundTicket(c *gin.Context) {
	var req CreateRefundTicketRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}

	currentUser, err := getTicketCurrentUser(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// eflow: 退款只走支付宝。
	//   - 微信大额转账必须先加好友，沟通成本高、一次失败没法重发；
	//   - 银行卡退款没几个人用，且对账还要追加户名校验；
	//   - 同步前端 create-ticket-dialog 已只露支付宝表单，这里再做一道服务端兜底，
	//     避免直接打 API 绕过 UI。
	// 同时校验 payee_account 必须是 11 位中国大陆手机号——支付宝大额转账没实名 + 手机号转不过去，
	// 让用户在提交工单时就把信息填对，别等管理员人工返工。
	req.PayeeType = "alipay"
	req.PayeeBank = ""
	req.PayeeAccount = strings.ReplaceAll(strings.TrimSpace(req.PayeeAccount), " ", "")
	if !alipayPhoneRegex.MatchString(req.PayeeAccount) {
		common.ApiError(c, fmt.Errorf("payee account must be an 11-digit mainland mobile number bound to Alipay"))
		return
	}

	ticket, refund, message, err := model.CreateRefundTicket(model.CreateRefundTicketParams{
		UserId:       currentUser.Id,
		Username:     currentUser.Username,
		Subject:      req.Subject,
		Priority:     req.Priority,
		RefundQuota:  req.RefundQuota,
		PayeeType:    req.PayeeType,
		PayeeName:    req.PayeeName,
		PayeeAccount: req.PayeeAccount,
		PayeeBank:    req.PayeeBank,
		Contact:      req.Contact,
		Reason:       req.Reason,
	})
	if err != nil {
		handleTicketError(c, err)
		return
	}

	service.AutoAssignTicketOnCreate(ticket.Id, ticket.Type, service.NotifyTicketAssigned)
	if refreshed, refreshErr := model.GetTicketById(ticket.Id); refreshErr == nil {
		ticket = refreshed
	}
	service.NotifyTicketCreatedToAdmin(ticket, message)
	common.ApiSuccess(c, gin.H{
		"ticket":  ticket,
		"refund":  refund,
		"message": message,
	})
}

func GetTicketRefund(c *gin.Context) {
	ticketId, ok := parseTicketID(c)
	if !ok {
		return
	}
	existing, err := model.GetTicketById(ticketId)
	if err != nil {
		handleTicketError(c, err)
		return
	}
	if !ensureTicketAccessible(c, existing) {
		return
	}
	refund, err := model.GetTicketRefundByTicketId(ticketId)
	if err != nil {
		handleTicketError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"refund": refund,
	})
}

func UpdateRefundStatus(c *gin.Context) {
	ticketId, ok := parseTicketID(c)
	if !ok {
		return
	}

	var req UpdateRefundStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}

	existing, err := model.GetTicketById(ticketId)
	if err != nil {
		handleTicketError(c, err)
		return
	}
	if !ensureTicketAccessible(c, existing) {
		return
	}

	// eflow: 客服 (role<Admin) 处理退款时只放行最安全的两种动作——
	//   - 批准 + write_off：核销已扣余额，账户余额不动；
	//   - 驳回（一律退还原金额）。
	// 不允许客服走 subtract（自定义核销金额，可能追加扣减）或 override
	// （直接覆盖用户余额，含写 0 的可能），这两种属于 admin 才能行使的资金调账权。
	// 前端 refund-detail-card 也对客服隐藏对应 RadioGroup 选项；这里是服务端兜底，
	// 防止客服直接 PUT 绕过 UI。
	role := c.GetInt("role")
	if role < common.RoleAdminUser {
		mode := strings.TrimSpace(req.QuotaMode)
		if req.RefundStatus == model.RefundStatusRefunded {
			if mode != "" && mode != model.RefundQuotaModeWriteOff {
				common.ApiError(c, fmt.Errorf(
					"customer service can only approve refunds with write_off; ask an admin for subtract/override"))
				return
			}
			// 兜底：客服批准退款时强制走 write_off，并清空 ActualRefundQuota。
			req.QuotaMode = model.RefundQuotaModeWriteOff
			req.ActualRefundQuota = nil
		}
	}

	params := model.UpdateRefundStatusParams{
		TicketId:     ticketId,
		AdminId:      c.GetInt("id"),
		RefundStatus: req.RefundStatus,
		QuotaMode:    req.QuotaMode,
	}
	if req.ActualRefundQuota != nil {
		params.ActualRefundQuota = *req.ActualRefundQuota
	}

	refund, ticket, prevStatus, err := model.UpdateRefundStatus(params)
	if err != nil {
		handleTicketError(c, err)
		return
	}

	reason := service.TicketStatusReasonGeneric
	switch req.RefundStatus {
	case model.RefundStatusRefunded:
		reason = service.TicketStatusReasonRefundApproved
	case model.RefundStatusRejected:
		reason = service.TicketStatusReasonRefundRejected
	}
	service.NotifyIfTicketStatusChanged(ticket, prevStatus, reason)

	common.ApiSuccess(c, gin.H{
		"refund": refund,
		"ticket": ticket,
	})
}

// TicketUserProfileResponse 是 /ticket/admin/:id/user-profile 的返回结构。
// 运营视角下的必要信息：余额、近期消费日志、模型使用 TopN、是否有待审核的退款申请；敏感字段不暴露。
type TicketUserProfileResponse struct {
	UserId             int                `json:"user_id"`
	Username           string             `json:"username"`
	DisplayName        string             `json:"display_name"`
	Email              string             `json:"email"`
	Role               int                `json:"role"`
	Status             int                `json:"status"`
	Group              string             `json:"group"`
	CreatedTime        int64              `json:"created_time"`
	Quota              int                `json:"quota"`
	UsedQuota          int                `json:"used_quota"`
	PendingRefundQuota int64              `json:"pending_refund_quota"` // 待审核退款额度（已从余额扣除）
	RequestCount       int                `json:"request_count"`
	RecentLogs         []*model.Log       `json:"recent_logs"`
	ModelUsage         []*model.QuotaData `json:"model_usage"`
}

const (
	ticketUserRecentLogLimit  = 15
	ticketUserModelUsageLimit = 8
	ticketUserModelUsageDays  = 30
)

// GetTicketUserProfile 管理员/客服在工单详情页查看用户画像。
// 任一分项加载失败不影响其它字段返回。客服只能看到分配给自己或本组待认领工单的用户画像。
func GetTicketUserProfile(c *gin.Context) {
	ticketId, ok := parseTicketID(c)
	if !ok {
		return
	}
	ticket, err := model.GetTicketById(ticketId)
	if err != nil {
		handleTicketError(c, err)
		return
	}
	if !ensureTicketAccessible(c, ticket) {
		return
	}

	user, err := model.GetUserById(ticket.UserId, false)
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
		common.SysLog(fmt.Sprintf("ticket user profile: failed to sum pending refund quota for user %d: %s", user.Id, pErr.Error()))
	} else {
		resp.PendingRefundQuota = pending
	}

	recentLogs, _, logErr := model.GetUserLogs(
		user.Id, model.LogTypeConsume, 0, 0, "", "",
		0, ticketUserRecentLogLimit, "", "", "",
		model.LogSortOptions{},
	)
	if logErr != nil {
		common.SysLog(fmt.Sprintf("ticket user profile: failed to fetch recent logs for user %d: %s", user.Id, logErr.Error()))
		recentLogs = nil
	}
	resp.RecentLogs = recentLogs

	since := common.GetTimestamp() - int64(ticketUserModelUsageDays)*24*3600
	usage, usageErr := model.GetUserModelUsageTopN(user.Id, since, ticketUserModelUsageLimit)
	if usageErr != nil {
		common.SysLog(fmt.Sprintf("ticket user profile: failed to aggregate model usage for user %d: %s", user.Id, usageErr.Error()))
	}
	resp.ModelUsage = usage

	common.ApiSuccess(c, resp)
}

func UpdateInvoiceStatus(c *gin.Context) {
	ticketId, ok := parseTicketID(c)
	if !ok {
		return
	}

	var req UpdateInvoiceStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}

	existing, err := model.GetTicketById(ticketId)
	if err != nil {
		handleTicketError(c, err)
		return
	}
	if !ensureTicketAccessible(c, existing) {
		return
	}

	invoice, ticket, prevStatus, err := model.UpdateInvoiceStatus(ticketId, c.GetInt("id"), req.InvoiceStatus)
	if err != nil {
		handleTicketError(c, err)
		return
	}

	// eflow: 开票成功走专用通知——把客服上传的发票文件作为附件直发到发票邮箱；
	// 其它状态(驳回等)仍走通用状态变更通知(已按发票工单优先投发票邮箱)。
	switch req.InvoiceStatus {
	case model.InvoiceStatusIssued:
		service.NotifyInvoiceIssuedToUser(ticket, prevStatus)
	default:
		reason := service.TicketStatusReasonGeneric
		if req.InvoiceStatus == model.InvoiceStatusRejected {
			reason = service.TicketStatusReasonInvoiceRejected
		}
		service.NotifyIfTicketStatusChanged(ticket, prevStatus, reason)
	}

	common.ApiSuccess(c, gin.H{
		"invoice": invoice,
		"ticket":  ticket,
	})
}
