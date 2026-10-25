package model

import (
	"errors"
	"strconv"
	"strings"

	"eflow.local/new-api/common"
	"gorm.io/gorm"
)

const (
	TicketStatusOpen       = 1 // 待处理
	TicketStatusProcessing = 2 // 处理中
	TicketStatusResolved   = 3 // 已解决
	TicketStatusClosed     = 4 // 已关闭
)

const (
	TicketTypeGeneral = "general"
	TicketTypeRefund  = "refund"
	TicketTypeInvoice = "invoice"
)

var (
	ErrTicketNotFound              = errors.New("ticket not found")
	ErrTicketForbidden             = errors.New("ticket forbidden")
	ErrTicketClosed                = errors.New("ticket closed")
	ErrTicketSubjectEmpty          = errors.New("ticket subject empty")
	ErrTicketContentEmpty          = errors.New("ticket content empty")
	ErrTicketInvalidStatus         = errors.New("ticket invalid status")
	ErrTicketInvalidType           = errors.New("ticket invalid type")
	ErrTicketAssigneeInvalid       = errors.New("ticket assignee invalid")
	ErrTicketInvoiceNotFound       = errors.New("ticket invoice not found")
	ErrTicketInvoiceStatusInvalid  = errors.New("ticket invoice status invalid")
	ErrTicketInvoiceOrderEmpty     = errors.New("ticket invoice order empty")
	ErrTicketInvoiceOrderInvalid   = errors.New("ticket invoice order invalid")
	ErrTicketInvoiceOrderDuplicate = errors.New("ticket invoice order duplicate")
	ErrTicketInvoiceCompanyEmpty   = errors.New("ticket invoice company empty")
	ErrTicketInvoiceTaxNumberEmpty = errors.New("ticket invoice tax number empty")
	ErrTicketInvoiceEmailEmpty     = errors.New("ticket invoice email empty")
	// eflow: 开票(置为已开具)前必须已上传发票文件，否则用户收不到发票
	ErrTicketInvoiceAttachmentRequired = errors.New("ticket invoice attachment required")
	// eflow: 所选充值订单合计金额未达到最低开票金额
	ErrTicketInvoiceAmountTooLow = errors.New("ticket invoice amount too low")
)

type Ticket struct {
	Id       int    `json:"id"`
	UserId   int    `json:"user_id" gorm:"index;not null"`
	Username string `json:"username" gorm:"type:varchar(64)"`
	Subject  string `json:"subject" gorm:"type:varchar(255);not null"`
	Type     string `json:"type" gorm:"type:varchar(32);index;default:'general'"`
	Status   int    `json:"status" gorm:"type:int;index;default:1"`
	Priority int    `json:"priority" gorm:"type:int;default:2"`
	AdminId  int    `json:"admin_id" gorm:"type:int;default:0"`
	// AssigneeId 当前分配到的客服/管理员用户 ID。0 表示尚未分配（待认领）。
	// 与 AdminId 分开：AdminId 会被"最后一次回复的管理员"覆盖，而 AssigneeId 是明确的负责人。
	AssigneeId  int            `json:"assignee_id" gorm:"type:int;index;default:0"`
	CreatedTime int64          `json:"created_time" gorm:"bigint"`
	UpdatedTime int64          `json:"updated_time" gorm:"bigint;index"`
	ClosedTime  int64          `json:"closed_time" gorm:"bigint;default:0"`
	DeletedAt   gorm.DeletedAt `gorm:"index"`
}

type TicketQueryOptions struct {
	UserId      int
	Status      int
	Type        string
	Keyword     string
	CompanyName string // 仅用于发票工单：按抬头（公司名称）模糊搜索；非发票工单会被忽略
	// AssigneeId 用于客服视角：只返回分配给指定用户的工单；值为 -1 表示"未分配"。
	// 0 表示不按分配过滤。
	AssigneeId int
	// TypeIn 限定工单类型必须在给定列表中；与 Type/抬头搜索取交集，不能被搜索绕过。
	// 用于"客服只看自己负责类型的待认领池"。空列表表示不启用。
	TypeIn []string
	// OrderBy 排序列，白名单：id / created_time / updated_time / status / priority。
	// 空字符串或非法值回退到默认 updated_time，保持与历史行为一致。
	OrderBy string
	// OrderDesc 排序方向，true=降序（默认），false=升序。
	OrderDesc bool
}

// resolveTicketOrder 把外部传入的排序列名经白名单解析为安全的 ORDER BY 子句。
// 只允许 tickets 表上的物理列，列名为字面量常量，杜绝 SQL 注入；
// 列名均为三种数据库通用的普通标识符（无保留字），符合 CLAUDE.md 规则 2。
// 除按 id 排序外都追加 `tickets.id desc` 作为次级排序，保证分页稳定。
func resolveTicketOrder(orderBy string, desc bool) string {
	allowed := map[string]string{
		"id":           "tickets.id",
		"created_time": "tickets.created_time",
		"updated_time": "tickets.updated_time",
		"status":       "tickets.status",
		"priority":     "tickets.priority",
	}
	col, ok := allowed[strings.TrimSpace(orderBy)]
	if !ok {
		// 未显式指定合法排序列：保持历史默认，不受方向位影响，
		// 这样所有未传排序参数的旧调用方行为完全不变。
		return "tickets.updated_time desc, tickets.id desc"
	}
	dir := "desc"
	if !desc {
		dir = "asc"
	}
	if col == "tickets.id" {
		return col + " " + dir
	}
	return col + " " + dir + ", tickets.id desc"
}

type CreateTicketParams struct {
	UserId        int
	Username      string
	Subject       string
	Type          string
	Priority      int
	Content       string
	Role          int
	AttachmentIds []int
}

func (ticket *Ticket) BeforeCreate(tx *gorm.DB) error {
	now := common.GetTimestamp()
	if ticket.CreatedTime == 0 {
		ticket.CreatedTime = now
	}
	if ticket.UpdatedTime == 0 {
		ticket.UpdatedTime = now
	}
	return nil
}

func (ticket *Ticket) BeforeUpdate(tx *gorm.DB) error {
	ticket.UpdatedTime = common.GetTimestamp()
	return nil
}

func NormalizeTicketType(ticketType string) string {
	return strings.ToLower(strings.TrimSpace(ticketType))
}

func NormalizeTicketPriority(priority int) int {
	switch priority {
	case 1, 2, 3:
		return priority
	default:
		return 2
	}
}

func IsValidTicketType(ticketType string) bool {
	switch NormalizeTicketType(ticketType) {
	case TicketTypeGeneral, TicketTypeRefund, TicketTypeInvoice:
		return true
	default:
		return false
	}
}

func IsValidTicketStatus(status int) bool {
	switch status {
	case TicketStatusOpen, TicketStatusProcessing, TicketStatusResolved, TicketStatusClosed:
		return true
	default:
		return false
	}
}

func CreateTicketWithMessage(params CreateTicketParams) (*Ticket, *TicketMessage, error) {
	subject := strings.TrimSpace(params.Subject)
	if subject == "" {
		return nil, nil, ErrTicketSubjectEmpty
	}
	content := strings.TrimSpace(params.Content)
	if content == "" {
		return nil, nil, ErrTicketContentEmpty
	}

	ticketType := NormalizeTicketType(params.Type)
	if ticketType == "" {
		ticketType = TicketTypeGeneral
	}
	if !IsValidTicketType(ticketType) {
		return nil, nil, ErrTicketInvalidType
	}

	now := common.GetTimestamp()
	ticket := &Ticket{
		UserId:      params.UserId,
		Username:    strings.TrimSpace(params.Username),
		Subject:     subject,
		Type:        ticketType,
		Status:      TicketStatusOpen,
		Priority:    NormalizeTicketPriority(params.Priority),
		CreatedTime: now,
		UpdatedTime: now,
	}
	message := &TicketMessage{
		UserId:      params.UserId,
		Username:    strings.TrimSpace(params.Username),
		Role:        params.Role,
		Content:     content,
		CreatedTime: now,
	}

	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(ticket).Error; err != nil {
			return err
		}
		message.TicketId = ticket.Id
		if err := tx.Create(message).Error; err != nil {
			return err
		}
		if err := BindAttachmentsToMessage(tx, params.AttachmentIds, ticket.Id, message.Id, params.UserId); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return ticket, message, nil
}

func applyTicketFilters(query *gorm.DB, options TicketQueryOptions) *gorm.DB {
	ticketType := NormalizeTicketType(options.Type)
	companyName := strings.TrimSpace(options.CompanyName)

	// 发票抬头搜索只对发票工单生效：如果用户传了 CompanyName 又没指定类型，
	// 自动加上 type=invoice。EXISTS 不会把发票表的同名字段覆盖到工单结果。
	needInvoiceFilter := companyName != "" && (ticketType == "" || ticketType == TicketTypeInvoice)
	if needInvoiceFilter {
		ticketType = TicketTypeInvoice
	}

	if options.UserId > 0 {
		query = query.Where("tickets.user_id = ?", options.UserId)
	}
	if options.Status > 0 {
		query = query.Where("tickets.status = ?", options.Status)
	}
	if ticketType != "" {
		query = query.Where("tickets.type = ?", ticketType)
	}
	if len(options.TypeIn) > 0 {
		query = query.Where("tickets.type IN ?", options.TypeIn)
	}
	// 分配过滤：-1 = 仅未分配，>0 = 仅某人。
	switch {
	case options.AssigneeId == -1:
		query = query.Where("tickets.assignee_id = 0")
	case options.AssigneeId > 0:
		query = query.Where("tickets.assignee_id = ?", options.AssigneeId)
	}
	keyword := strings.TrimSpace(options.Keyword)
	if keyword != "" {
		like := "%" + keyword + "%"
		if ticketId, err := strconv.Atoi(keyword); err == nil {
			query = query.Where("(tickets.id = ? OR tickets.subject LIKE ? OR tickets.username LIKE ?)", ticketId, like, like)
		} else {
			query = query.Where("(tickets.subject LIKE ? OR tickets.username LIKE ?)", like, like)
		}
	}
	if needInvoiceFilter {
		like := "%" + strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(companyName) + "%"
		query = query.Where("EXISTS (SELECT 1 FROM ticket_invoices WHERE ticket_invoices.ticket_id = tickets.id AND ticket_invoices.company_name LIKE ? ESCAPE '!')", like)
	}
	return query
}

func ListTickets(options TicketQueryOptions, pageInfo *common.PageInfo) (tickets []*Ticket, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	query := applyTicketFilters(tx.Model(&Ticket{}), options)
	if err = query.Count(&total).Error; err != nil {
		tx.Rollback()
		return nil, 0, err
	}
	if err = query.Order(resolveTicketOrder(options.OrderBy, options.OrderDesc)).
		Limit(pageInfo.GetPageSize()).
		Offset(pageInfo.GetStartIdx()).
		Find(&tickets).Error; err != nil {
		tx.Rollback()
		return nil, 0, err
	}
	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}
	return tickets, total, nil
}

func GetTicketById(ticketId int) (*Ticket, error) {
	var ticket Ticket
	if err := DB.First(&ticket, "id = ?", ticketId).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTicketNotFound
		}
		return nil, err
	}
	return &ticket, nil
}

func GetUserTicketById(ticketId int, userId int) (*Ticket, error) {
	ticket, err := GetTicketById(ticketId)
	if err != nil {
		return nil, err
	}
	if ticket.UserId != userId {
		return nil, ErrTicketForbidden
	}
	return ticket, nil
}

func GetTicketMessages(ticketId int) (messages []*TicketMessage, err error) {
	err = DB.Where("ticket_id = ?", ticketId).Order("id asc").Find(&messages).Error
	return messages, err
}

// TicketMessageWithAttachments 是工单详情页的渲染结构：消息 + 关联附件。
type TicketMessageWithAttachments struct {
	*TicketMessage
	Attachments []*TicketAttachment `json:"attachments"`
}

// GetTicketMessagesWithAttachments 一次拉取工单所有消息及其附件，避免前端 N+1。
func GetTicketMessagesWithAttachments(ticketId int) ([]*TicketMessageWithAttachments, error) {
	messages, err := GetTicketMessages(ticketId)
	if err != nil {
		return nil, err
	}
	if len(messages) == 0 {
		return []*TicketMessageWithAttachments{}, nil
	}
	ids := make([]int, 0, len(messages))
	for _, m := range messages {
		ids = append(ids, m.Id)
	}
	attachments, err := GetAttachmentsByMessageIds(ids)
	if err != nil {
		return nil, err
	}
	grouped := make(map[int][]*TicketAttachment, len(messages))
	for _, a := range attachments {
		grouped[a.MessageId] = append(grouped[a.MessageId], a)
	}
	out := make([]*TicketMessageWithAttachments, 0, len(messages))
	for _, m := range messages {
		list := grouped[m.Id]
		if list == nil {
			list = []*TicketAttachment{}
		}
		out = append(out, &TicketMessageWithAttachments{
			TicketMessage: m,
			Attachments:   list,
		})
	}
	return out, nil
}

// AddTicketMessage 追加一条工单回复并按角色自动推进状态。
// 第三个返回值是追加前的工单主状态，供调用方判定是否需要对外发送状态变更通知。
// attachmentIds 在同一事务内绑定到新建的消息上，失败整体回滚。允许 content 为空但 attachmentIds 非空
// （纯图片/文件回复），反之至少要有一者。
func AddTicketMessage(ticketId int, userId int, username string, role int, content string, attachmentIds []int) (*TicketMessage, *Ticket, int, error) {
	content = strings.TrimSpace(content)
	if content == "" && len(attachmentIds) == 0 {
		return nil, nil, 0, ErrTicketContentEmpty
	}

	var (
		ticket     Ticket
		prevStatus int
	)
	now := common.GetTimestamp()
	message := &TicketMessage{
		TicketId:    ticketId,
		UserId:      userId,
		Username:    strings.TrimSpace(username),
		Role:        role,
		Content:     content,
		CreatedTime: now,
	}

	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&ticket, "id = ?", ticketId).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrTicketNotFound
			}
			return err
		}
		if ticket.Status == TicketStatusClosed {
			return ErrTicketClosed
		}
		prevStatus = ticket.Status
		if err := tx.Create(message).Error; err != nil {
			return err
		}
		if err := BindAttachmentsToMessage(tx, attachmentIds, ticket.Id, message.Id, userId); err != nil {
			return err
		}

		// 工单工作人员（客服或管理员）首次接手后自动进入处理中；
		// 若工单尚未分配（assignee_id=0），同一事务里"谁先回复谁认领"。
		// 普通用户在已解决状态追问时也会把工单回退到处理中。
		updates := map[string]any{
			"updated_time": now,
		}
		if role >= common.RoleCustomerServiceUser {
			updates["admin_id"] = userId
			ticket.AdminId = userId
			if ticket.AssigneeId == 0 {
				updates["assignee_id"] = userId
				ticket.AssigneeId = userId
			}
			if ticket.Status == TicketStatusOpen {
				updates["status"] = TicketStatusProcessing
				ticket.Status = TicketStatusProcessing
			}
		} else if ticket.Status == TicketStatusResolved {
			updates["status"] = TicketStatusProcessing
			ticket.Status = TicketStatusProcessing
		}
		ticket.UpdatedTime = now

		if err := tx.Model(&Ticket{}).Where("id = ?", ticket.Id).Updates(updates).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, nil, 0, err
	}
	return message, &ticket, prevStatus, nil
}

func CloseUserTicket(ticketId int, userId int) (*Ticket, error) {
	var ticket Ticket
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&ticket, "id = ?", ticketId).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrTicketNotFound
			}
			return err
		}
		if ticket.UserId != userId {
			return ErrTicketForbidden
		}
		if ticket.Status == TicketStatusClosed {
			return nil
		}

		now := common.GetTimestamp()
		if err := tx.Model(&Ticket{}).Where("id = ?", ticket.Id).Updates(map[string]any{
			"status":       TicketStatusClosed,
			"closed_time":  now,
			"updated_time": now,
		}).Error; err != nil {
			return err
		}
		ticket.Status = TicketStatusClosed
		ticket.ClosedTime = now
		ticket.UpdatedTime = now
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &ticket, nil
}

// UpdateTicketStatus 管理员调整工单状态/优先级。
// 第二个返回值是修改前的主状态，便于调用方仅在状态真正变化时触发通知。
func UpdateTicketStatus(ticketId int, adminId int, status *int, priority *int) (*Ticket, int, error) {
	var (
		ticket     Ticket
		prevStatus int
	)
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&ticket, "id = ?", ticketId).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrTicketNotFound
			}
			return err
		}
		prevStatus = ticket.Status

		now := common.GetTimestamp()
		updates := map[string]any{
			"updated_time": now,
			"admin_id":     adminId,
		}
		ticket.AdminId = adminId
		ticket.UpdatedTime = now

		if status != nil {
			if !IsValidTicketStatus(*status) {
				return ErrTicketInvalidStatus
			}
			updates["status"] = *status
			ticket.Status = *status
			if *status == TicketStatusClosed {
				updates["closed_time"] = now
				ticket.ClosedTime = now
			} else {
				updates["closed_time"] = 0
				ticket.ClosedTime = 0
			}
		}
		if priority != nil {
			nextPriority := NormalizeTicketPriority(*priority)
			updates["priority"] = nextPriority
			ticket.Priority = nextPriority
		}

		if err := tx.Model(&Ticket{}).Where("id = ?", ticket.Id).Updates(updates).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	return &ticket, prevStatus, nil
}

// AssignTicket 将工单分配到指定用户。assigneeId 为 0 表示"取消分配，回到待认领池"。
// 并发安全：使用 assignee_id 作为乐观锁条件，避免两个管理员在同一时刻指定为不同的人。
// 返回的第三个值是本次调用前的 AssigneeId，调用方可据此判断是否发生了变化并决定是否通知。
func AssignTicket(ticketId int, assigneeId int, expectedAssigneeId *int) (*Ticket, int, error) {
	var (
		ticket     Ticket
		prevAssign int
	)
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&ticket, "id = ?", ticketId).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrTicketNotFound
			}
			return err
		}
		prevAssign = ticket.AssigneeId
		if expectedAssigneeId != nil && ticket.AssigneeId != *expectedAssigneeId {
			// 乐观锁失败：说明另一个管理员/客服已经抢先分配过
			return ErrTicketAssigneeInvalid
		}
		if ticket.AssigneeId == assigneeId {
			// 已经是目标负责人，幂等返回
			return nil
		}
		now := common.GetTimestamp()
		result := tx.Model(&Ticket{}).Where("id = ? AND assignee_id = ?", ticket.Id, prevAssign).
			Updates(map[string]any{
				"assignee_id":  assigneeId,
				"updated_time": now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrTicketAssigneeInvalid
		}
		ticket.AssigneeId = assigneeId
		ticket.UpdatedTime = now
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	return &ticket, prevAssign, nil
}

// CountAssigneeLoad 统计一批候选负责人当前分别承担多少活跃工单（非已关闭），
// 用于"最少负载"分配策略。返回 map[userId]count；未出现的候选表示计数为 0。
func CountAssigneeLoad(candidateIds []int) (map[int]int, error) {
	result := make(map[int]int, len(candidateIds))
	if len(candidateIds) == 0 {
		return result, nil
	}
	type row struct {
		AssigneeId int `gorm:"column:assignee_id"`
		Cnt        int `gorm:"column:cnt"`
	}
	var rows []row
	err := DB.Model(&Ticket{}).
		Select("assignee_id, COUNT(*) AS cnt").
		Where("assignee_id IN ?", candidateIds).
		Where("status <> ?", TicketStatusClosed).
		Group("assignee_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		result[r.AssigneeId] = r.Cnt
	}
	return result, nil
}
