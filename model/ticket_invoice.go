package model

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	InvoiceStatusPending  = 1
	InvoiceStatusIssued   = 2
	InvoiceStatusRejected = 3
)

type TicketInvoice struct {
	Id             int     `json:"id"`
	TicketId       int     `json:"ticket_id" gorm:"uniqueIndex;not null"`
	UserId         int     `json:"user_id" gorm:"index;not null"`
	CompanyName    string  `json:"company_name" gorm:"type:varchar(255);not null"`
	TaxNumber      string  `json:"tax_number" gorm:"type:varchar(64);not null"`
	BankName       string  `json:"bank_name" gorm:"type:varchar(255)"`
	BankAccount    string  `json:"bank_account" gorm:"type:varchar(128)"`
	CompanyAddress string  `json:"company_address" gorm:"type:varchar(512)"`
	CompanyPhone   string  `json:"company_phone" gorm:"type:varchar(32)"`
	Email          string  `json:"email" gorm:"type:varchar(128);not null"`
	TopUpOrderIds  string  `json:"topup_order_ids" gorm:"type:text;not null"`
	TotalMoney     float64 `json:"total_money"`
	InvoiceStatus  int     `json:"invoice_status" gorm:"type:int;default:1"`
	IssuedTime     int64   `json:"issued_time" gorm:"bigint;default:0"`
	CreatedTime    int64   `json:"created_time" gorm:"bigint"`
}

type CreateInvoiceTicketParams struct {
	UserId         int
	Username       string
	Subject        string
	Priority       int
	Content        string
	CompanyName    string
	TaxNumber      string
	BankName       string
	BankAccount    string
	CompanyAddress string
	CompanyPhone   string
	Email          string
	TopUpOrderIds  []int
}

func (invoice *TicketInvoice) BeforeCreate(tx *gorm.DB) error {
	if invoice.CreatedTime == 0 {
		invoice.CreatedTime = common.GetTimestamp()
	}
	return nil
}

func IsValidInvoiceStatus(status int) bool {
	switch status {
	case InvoiceStatusPending, InvoiceStatusIssued, InvoiceStatusRejected:
		return true
	default:
		return false
	}
}

func normalizeTopUpOrderIDs(orderIds []int) ([]int, error) {
	seen := make(map[int]struct{}, len(orderIds))
	result := make([]int, 0, len(orderIds))
	for _, orderId := range orderIds {
		if orderId <= 0 {
			return nil, ErrTicketInvoiceOrderInvalid
		}
		if _, ok := seen[orderId]; ok {
			continue
		}
		seen[orderId] = struct{}{}
		result = append(result, orderId)
	}
	if len(result) == 0 {
		return nil, ErrTicketInvoiceOrderEmpty
	}
	return result, nil
}

func (invoice *TicketInvoice) GetTopUpOrderIDs() ([]int, error) {
	if strings.TrimSpace(invoice.TopUpOrderIds) == "" {
		return []int{}, nil
	}
	var orderIds []int
	if err := common.UnmarshalJsonStr(invoice.TopUpOrderIds, &orderIds); err != nil {
		return nil, err
	}
	return normalizeTopUpOrderIDs(orderIds)
}

func (invoice *TicketInvoice) SetTopUpOrderIDs(orderIds []int) error {
	normalizedOrderIds, err := normalizeTopUpOrderIDs(orderIds)
	if err != nil {
		return err
	}
	raw, err := common.Marshal(normalizedOrderIds)
	if err != nil {
		return err
	}
	invoice.TopUpOrderIds = string(raw)
	return nil
}

func GetTicketInvoiceByTicketId(ticketId int) (*TicketInvoice, error) {
	var invoice TicketInvoice
	if err := DB.Where("ticket_id = ?", ticketId).First(&invoice).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTicketInvoiceNotFound
		}
		return nil, err
	}
	return &invoice, nil
}

func getProtectedInvoiceOrderSet(tx *gorm.DB, userId int) (map[int]struct{}, error) {
	var invoices []*TicketInvoice
	if err := tx.Where("user_id = ? AND invoice_status <> ?", userId, InvoiceStatusRejected).Find(&invoices).Error; err != nil {
		return nil, err
	}

	used := make(map[int]struct{})
	for _, invoice := range invoices {
		orderIds, err := invoice.GetTopUpOrderIDs()
		if err != nil {
			return nil, err
		}
		for _, orderId := range orderIds {
			used[orderId] = struct{}{}
		}
	}
	return used, nil
}

func fetchSuccessTopUpsByIds(tx *gorm.DB, userId int, orderIds []int) ([]*TopUp, error) {
	var topUps []*TopUp
	if err := tx.Where("user_id = ? AND status = ?", userId, common.TopUpStatusSuccess).
		Where("id IN ?", orderIds).
		Find(&topUps).Error; err != nil {
		return nil, err
	}
	return topUps, nil
}

func orderTopUps(orderIds []int, topUps []*TopUp) []*TopUp {
	topUpMap := make(map[int]*TopUp, len(topUps))
	for _, topUp := range topUps {
		topUpMap[topUp.Id] = topUp
	}
	ordered := make([]*TopUp, 0, len(orderIds))
	for _, orderId := range orderIds {
		if topUp, ok := topUpMap[orderId]; ok {
			ordered = append(ordered, topUp)
		}
	}
	return ordered
}

func GetEligibleInvoiceOrders(userId int) ([]*TopUp, error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	usedSet, err := getProtectedInvoiceOrderSet(tx, userId)
	if err != nil {
		tx.Rollback()
		return nil, err
	}

	query := tx.Where("user_id = ? AND status = ?", userId, common.TopUpStatusSuccess)
	if len(usedSet) > 0 {
		usedIds := make([]int, 0, len(usedSet))
		for orderId := range usedSet {
			usedIds = append(usedIds, orderId)
		}
		query = query.Not("id IN ?", usedIds)
	}

	var topUps []*TopUp
	if err = query.Order("complete_time desc, id desc").Find(&topUps).Error; err != nil {
		tx.Rollback()
		return nil, err
	}
	if err = tx.Commit().Error; err != nil {
		return nil, err
	}
	return topUps, nil
}

func buildInvoiceSummaryMessage(params CreateInvoiceTicketParams, orderIds []int, totalMoney float64) string {
	lines := []string{
		"发票申请信息：",
		fmt.Sprintf("公司名称：%s", strings.TrimSpace(params.CompanyName)),
		fmt.Sprintf("税号：%s", strings.TrimSpace(params.TaxNumber)),
		fmt.Sprintf("接收邮箱：%s", strings.TrimSpace(params.Email)),
		fmt.Sprintf("关联订单：%v", orderIds),
		fmt.Sprintf("申请金额：%.2f", totalMoney),
	}
	if bankName := strings.TrimSpace(params.BankName); bankName != "" {
		lines = append(lines, fmt.Sprintf("开户行：%s", bankName))
	}
	if bankAccount := strings.TrimSpace(params.BankAccount); bankAccount != "" {
		lines = append(lines, fmt.Sprintf("银行账号：%s", bankAccount))
	}
	if companyAddress := strings.TrimSpace(params.CompanyAddress); companyAddress != "" {
		lines = append(lines, fmt.Sprintf("注册地址：%s", companyAddress))
	}
	if companyPhone := strings.TrimSpace(params.CompanyPhone); companyPhone != "" {
		lines = append(lines, fmt.Sprintf("联系电话：%s", companyPhone))
	}
	if content := strings.TrimSpace(params.Content); content != "" {
		lines = append(lines, "备注：")
		lines = append(lines, content)
	}
	return strings.Join(lines, "\n")
}

func CreateInvoiceTicket(params CreateInvoiceTicketParams) (*Ticket, *TicketInvoice, *TicketMessage, []*TopUp, error) {
	// eflow: 抬头（CompanyName）= 个人姓名 / 企业全称，统一必填；税号在个人发票场景可空，
	// 由调用方（前端）在企业类型下做必填校验。后端不再硬卡 TaxNumber，否则个人开票走不通。
	if strings.TrimSpace(params.CompanyName) == "" {
		return nil, nil, nil, nil, ErrTicketInvoiceCompanyEmpty
	}
	if strings.TrimSpace(params.Email) == "" {
		return nil, nil, nil, nil, ErrTicketInvoiceEmailEmpty
	}

	orderIds, err := normalizeTopUpOrderIDs(params.TopUpOrderIds)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	var (
		ticket        *Ticket
		message       *TicketMessage
		invoice       *TicketInvoice
		orderedTopUps []*TopUp
	)
	err = DB.Transaction(func(tx *gorm.DB) error {
		usedSet, err := getProtectedInvoiceOrderSet(tx, params.UserId)
		if err != nil {
			return err
		}
		for _, orderId := range orderIds {
			if _, ok := usedSet[orderId]; ok {
				return ErrTicketInvoiceOrderDuplicate
			}
		}

		topUps, err := fetchSuccessTopUpsByIds(tx, params.UserId, orderIds)
		if err != nil {
			return err
		}
		if len(topUps) != len(orderIds) {
			return ErrTicketInvoiceOrderInvalid
		}

		var totalMoney float64
		for _, topUp := range topUps {
			totalMoney += topUp.Money
		}
		// eflow: 最低开票金额门槛（0 = 不限）。合计金额不达标直接拒绝，
		// 阈值由调用方据 common.TicketInvoiceMinTopupMoney 拼错误文案。
		if common.TicketInvoiceMinTopupMoney > 0 && totalMoney < common.TicketInvoiceMinTopupMoney {
			return ErrTicketInvoiceAmountTooLow
		}
		orderedTopUps = orderTopUps(orderIds, topUps)

		subject := strings.TrimSpace(params.Subject)
		if subject == "" {
			subject = fmt.Sprintf("发票申请（%d 笔订单）", len(orderIds))
		}

		now := common.GetTimestamp()
		ticket = &Ticket{
			UserId:      params.UserId,
			Username:    strings.TrimSpace(params.Username),
			Subject:     subject,
			Type:        TicketTypeInvoice,
			Status:      TicketStatusOpen,
			Priority:    NormalizeTicketPriority(params.Priority),
			CreatedTime: now,
			UpdatedTime: now,
		}
		if err := tx.Create(ticket).Error; err != nil {
			return err
		}

		invoice = &TicketInvoice{
			TicketId:       ticket.Id,
			UserId:         params.UserId,
			CompanyName:    strings.TrimSpace(params.CompanyName),
			TaxNumber:      strings.TrimSpace(params.TaxNumber),
			BankName:       strings.TrimSpace(params.BankName),
			BankAccount:    strings.TrimSpace(params.BankAccount),
			CompanyAddress: strings.TrimSpace(params.CompanyAddress),
			CompanyPhone:   strings.TrimSpace(params.CompanyPhone),
			Email:          strings.TrimSpace(params.Email),
			TotalMoney:     totalMoney,
			InvoiceStatus:  InvoiceStatusPending,
			CreatedTime:    now,
		}
		if err := invoice.SetTopUpOrderIDs(orderIds); err != nil {
			return err
		}
		if err := tx.Create(invoice).Error; err != nil {
			return err
		}

		message = &TicketMessage{
			TicketId:    ticket.Id,
			UserId:      params.UserId,
			Username:    strings.TrimSpace(params.Username),
			Role:        common.RoleCommonUser,
			Content:     buildInvoiceSummaryMessage(params, orderIds, totalMoney),
			CreatedTime: now,
		}
		if err := tx.Create(message).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return ticket, invoice, message, orderedTopUps, nil
}

func GetTicketInvoiceDetail(ticketId int) (*TicketInvoice, []*TopUp, error) {
	invoice, err := GetTicketInvoiceByTicketId(ticketId)
	if err != nil {
		return nil, nil, err
	}
	orderIds, err := invoice.GetTopUpOrderIDs()
	if err != nil {
		return nil, nil, err
	}
	topUps, err := fetchSuccessTopUpsByIds(DB, invoice.UserId, orderIds)
	if err != nil {
		return nil, nil, err
	}
	return invoice, orderTopUps(orderIds, topUps), nil
}

// UpdateInvoiceStatus 管理员调整发票状态。
// 第三个返回值是修改前的工单主状态，供调用方触发状态已变化通知。
func UpdateInvoiceStatus(ticketId int, adminId int, invoiceStatus int) (*TicketInvoice, *Ticket, int, error) {
	if !IsValidInvoiceStatus(invoiceStatus) {
		return nil, nil, 0, ErrTicketInvoiceStatusInvalid
	}

	var (
		invoice    TicketInvoice
		ticket     Ticket
		prevStatus int
	)
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("ticket_id = ?", ticketId).First(&invoice).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrTicketInvoiceNotFound
			}
			return err
		}
		if err := tx.First(&ticket, "id = ?", ticketId).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrTicketNotFound
			}
			return err
		}
		prevStatus = ticket.Status

		// eflow: 开票(置为已开具)前必须已有客服上传的发票文件，否则用户拿不到发票。
		// 用同一事务计数，避免与并发删附件竞态。
		if invoiceStatus == InvoiceStatusIssued {
			var attCount int64
			if err := tx.Model(&TicketAttachment{}).
				Where("ticket_id = ? AND message_id <> 0 AND user_id <> ?", ticketId, ticket.UserId).
				Count(&attCount).Error; err != nil {
				return err
			}
			if attCount == 0 {
				return ErrTicketInvoiceAttachmentRequired
			}
		}

		now := common.GetTimestamp()
		invoiceUpdates := map[string]interface{}{
			"invoice_status": invoiceStatus,
		}
		ticketUpdates := map[string]interface{}{
			"updated_time": now,
			"admin_id":     adminId,
		}
		switch invoiceStatus {
		case InvoiceStatusIssued:
			invoiceUpdates["issued_time"] = now
			ticketUpdates["status"] = TicketStatusResolved
		case InvoiceStatusRejected:
			invoiceUpdates["issued_time"] = int64(0)
			ticketUpdates["status"] = TicketStatusProcessing
		default:
			invoiceUpdates["issued_time"] = int64(0)
		}

		if err := tx.Model(&TicketInvoice{}).Where("id = ?", invoice.Id).Updates(invoiceUpdates).Error; err != nil {
			return err
		}
		if err := tx.Model(&Ticket{}).Where("id = ?", ticket.Id).Updates(ticketUpdates).Error; err != nil {
			return err
		}

		invoice.InvoiceStatus = invoiceStatus
		invoice.IssuedTime = invoiceUpdates["issued_time"].(int64)
		if status, ok := ticketUpdates["status"].(int); ok {
			ticket.Status = status
		}
		ticket.AdminId = adminId
		ticket.UpdatedTime = now
		return nil
	})
	if err != nil {
		return nil, nil, 0, err
	}
	return &invoice, &ticket, prevStatus, nil
}
