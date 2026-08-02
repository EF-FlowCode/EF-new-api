package model

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"gorm.io/gorm"
)

const (
	RefundStatusPending  = 1 // 待审核（冻结中）
	RefundStatusRefunded = 2 // 已退款
	RefundStatusRejected = 3 // 已驳回
)

const (
	RefundPayeeTypeAlipay = "alipay"
	RefundPayeeTypeWechat = "wechat"
	RefundPayeeTypeBank   = "bank"
	RefundPayeeTypeOther  = "other"
)

// 批准退款时的额度操作模式。默认按 RefundQuotaModeWriteOff（核销冻结）。
const (
	RefundQuotaModeWriteOff = "write_off" // 核销冻结额度（默认）
	RefundQuotaModeSubtract = "subtract"  // 自定义核销金额：先全部解冻，再从 quota 扣除指定金额
	RefundQuotaModeOverride = "override"  // 覆盖最终余额：先全部解冻，再把 quota 设置为指定值
)

var (
	ErrTicketRefundNotFound          = errors.New("ticket refund not found")
	ErrTicketRefundStatusInvalid     = errors.New("ticket refund status invalid")
	ErrTicketRefundQuotaInvalid      = errors.New("ticket refund quota invalid")
	ErrTicketRefundQuotaExceed       = errors.New("ticket refund quota exceed")
	ErrTicketRefundPayeeTypeEmpty    = errors.New("ticket refund payee type empty")
	ErrTicketRefundPayeeNameEmpty    = errors.New("ticket refund payee name empty")
	ErrTicketRefundPayeeAccountEmpty = errors.New("ticket refund payee account empty")
	ErrTicketRefundPayeeBankEmpty    = errors.New("ticket refund payee bank empty")
	ErrTicketRefundContactEmpty      = errors.New("ticket refund contact empty")
	ErrTicketRefundNotPending        = errors.New("ticket refund not pending")
	ErrTicketRefundQuotaModeInvalid  = errors.New("ticket refund quota mode invalid")
)

type TicketRefund struct {
	Id                int    `json:"id"`
	TicketId          int    `json:"ticket_id" gorm:"uniqueIndex;not null"`
	UserId            int    `json:"user_id" gorm:"index;not null"`
	RefundQuota       int    `json:"refund_quota" gorm:"type:int;not null"`
	FrozenQuota       int    `json:"frozen_quota" gorm:"type:int;default:0"`
	UserQuotaSnapshot int    `json:"user_quota_snapshot" gorm:"type:int;default:0"`
	PayeeType         string `json:"payee_type" gorm:"type:varchar(16);not null"`
	PayeeName         string `json:"payee_name" gorm:"type:varchar(128);not null"`
	PayeeAccount      string `json:"payee_account" gorm:"type:varchar(128);not null"`
	PayeeBank         string `json:"payee_bank" gorm:"type:varchar(255)"`
	Contact           string `json:"contact" gorm:"type:varchar(128);not null"`
	Reason            string `json:"reason" gorm:"type:text"`
	RefundStatus      int    `json:"refund_status" gorm:"type:int;default:1"`
	ProcessedTime     int64  `json:"processed_time" gorm:"bigint;default:0"`
	CreatedTime       int64  `json:"created_time" gorm:"bigint"`
}

type CreateRefundTicketParams struct {
	UserId       int
	Username     string
	Subject      string
	Priority     int
	RefundQuota  int
	PayeeType    string
	PayeeName    string
	PayeeAccount string
	PayeeBank    string
	Contact      string
	Reason       string
}

func (refund *TicketRefund) BeforeCreate(tx *gorm.DB) error {
	if refund.CreatedTime == 0 {
		refund.CreatedTime = common.GetTimestamp()
	}
	return nil
}

func IsValidRefundStatus(status int) bool {
	switch status {
	case RefundStatusPending, RefundStatusRefunded, RefundStatusRejected:
		return true
	default:
		return false
	}
}

func NormalizeRefundPayeeType(payeeType string) string {
	return strings.ToLower(strings.TrimSpace(payeeType))
}

func IsValidRefundPayeeType(payeeType string) bool {
	switch NormalizeRefundPayeeType(payeeType) {
	case RefundPayeeTypeAlipay, RefundPayeeTypeWechat, RefundPayeeTypeBank, RefundPayeeTypeOther:
		return true
	default:
		return false
	}
}

func refundPayeeTypeText(payeeType string) string {
	switch NormalizeRefundPayeeType(payeeType) {
	case RefundPayeeTypeAlipay:
		return "支付宝"
	case RefundPayeeTypeWechat:
		return "微信"
	case RefundPayeeTypeBank:
		return "银行卡"
	default:
		return "其他"
	}
}

func buildRefundSummaryMessage(params CreateRefundTicketParams) string {
	lines := []string{
		"退款申请信息：",
		fmt.Sprintf("申请退款额度：%s", logger.LogQuota(params.RefundQuota)),
		fmt.Sprintf("收款方式：%s", refundPayeeTypeText(params.PayeeType)),
		fmt.Sprintf("收款人：%s", strings.TrimSpace(params.PayeeName)),
		fmt.Sprintf("收款账号：%s", strings.TrimSpace(params.PayeeAccount)),
	}
	if bank := strings.TrimSpace(params.PayeeBank); bank != "" {
		lines = append(lines, fmt.Sprintf("开户行：%s", bank))
	}
	lines = append(lines, fmt.Sprintf("联系方式：%s", strings.TrimSpace(params.Contact)))
	if reason := strings.TrimSpace(params.Reason); reason != "" {
		lines = append(lines, "退款原因：")
		lines = append(lines, reason)
	}
	return strings.Join(lines, "\n")
}

// CreateRefundTicket 创建退款工单，并立即通过 DecreaseUserQuota 从用户余额中扣除申请金额。
//
// 设计说明（为什么"冻结"=直接扣减）：
//   - "冻结"是业务语义（用户端展示、审计追溯），账户层面走系统统一的余额扣减接口（DecreaseUserQuota），
//     这样可以保证 Redis 缓存、BatchUpdate、消费路径看到的余额始终一致，不引入新的一致性窗口。
//   - 驳回时通过 IncreaseUserQuota 退还；审核通过时余额不变（扣减已在提交时完成）。
//   - 仅工单业务数据（Ticket/TicketRefund/TicketMessage）在事务中原子写入；
//     账户扣减在事务前完成，事务失败时通过 Increase 退还，保证一致。
func CreateRefundTicket(params CreateRefundTicketParams) (*Ticket, *TicketRefund, *TicketMessage, error) {
	payeeType := NormalizeRefundPayeeType(params.PayeeType)
	if payeeType == "" {
		return nil, nil, nil, ErrTicketRefundPayeeTypeEmpty
	}
	if !IsValidRefundPayeeType(payeeType) {
		return nil, nil, nil, ErrTicketRefundPayeeTypeEmpty
	}
	if params.RefundQuota <= 0 {
		return nil, nil, nil, ErrTicketRefundQuotaInvalid
	}
	if strings.TrimSpace(params.PayeeName) == "" {
		return nil, nil, nil, ErrTicketRefundPayeeNameEmpty
	}
	if strings.TrimSpace(params.PayeeAccount) == "" {
		return nil, nil, nil, ErrTicketRefundPayeeAccountEmpty
	}
	if payeeType == RefundPayeeTypeBank && strings.TrimSpace(params.PayeeBank) == "" {
		return nil, nil, nil, ErrTicketRefundPayeeBankEmpty
	}
	if strings.TrimSpace(params.Contact) == "" {
		return nil, nil, nil, ErrTicketRefundContactEmpty
	}

	// 预检用户当前可用余额。直接读 DB，确保不被缓存/批量更新延迟误导。
	var userQuota int
	if err := DB.Model(&User{}).Where("id = ?", params.UserId).
		Select("quota").Find(&userQuota).Error; err != nil {
		return nil, nil, nil, err
	}
	if params.RefundQuota > userQuota {
		return nil, nil, nil, ErrTicketRefundQuotaExceed
	}

	// 先扣除余额（db=true 跳过 BatchUpdate，立即落库）。
	if err := DecreaseUserQuota(params.UserId, params.RefundQuota, true); err != nil {
		return nil, nil, nil, err
	}
	quotaDeducted := true
	defer func() {
		// 事务后半段如果失败，补偿性退还已扣金额，避免用户资产凭空消失。
		if quotaDeducted {
			return
		}
		if err := IncreaseUserQuota(params.UserId, params.RefundQuota, true); err != nil {
			common.SysError(fmt.Sprintf(
				"refund create failed and quota refund also failed, user_id=%d, quota=%d, err=%v",
				params.UserId, params.RefundQuota, err))
		} else {
			_ = InvalidateUserCache(params.UserId)
		}
	}()

	var (
		ticket  *Ticket
		refund  *TicketRefund
		message *TicketMessage
	)
	err := DB.Transaction(func(tx *gorm.DB) error {
		subject := strings.TrimSpace(params.Subject)
		if subject == "" {
			subject = fmt.Sprintf("退款申请（%s）", logger.LogQuota(params.RefundQuota))
		}

		now := common.GetTimestamp()
		ticket = &Ticket{
			UserId:      params.UserId,
			Username:    strings.TrimSpace(params.Username),
			Subject:     subject,
			Type:        TicketTypeRefund,
			Status:      TicketStatusOpen,
			Priority:    NormalizeTicketPriority(params.Priority),
			CreatedTime: now,
			UpdatedTime: now,
		}
		if err := tx.Create(ticket).Error; err != nil {
			return err
		}

		refund = &TicketRefund{
			TicketId:          ticket.Id,
			UserId:            params.UserId,
			RefundQuota:       params.RefundQuota,
			FrozenQuota:       params.RefundQuota,
			UserQuotaSnapshot: userQuota,
			PayeeType:         payeeType,
			PayeeName:         strings.TrimSpace(params.PayeeName),
			PayeeAccount:      strings.TrimSpace(params.PayeeAccount),
			PayeeBank:         strings.TrimSpace(params.PayeeBank),
			Contact:           strings.TrimSpace(params.Contact),
			Reason:            strings.TrimSpace(params.Reason),
			RefundStatus:      RefundStatusPending,
			CreatedTime:       now,
		}
		if err := tx.Create(refund).Error; err != nil {
			return err
		}

		message = &TicketMessage{
			TicketId:    ticket.Id,
			UserId:      params.UserId,
			Username:    strings.TrimSpace(params.Username),
			Role:        common.RoleCommonUser,
			Content:     buildRefundSummaryMessage(params),
			CreatedTime: now,
		}
		if err := tx.Create(message).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		quotaDeducted = false // 触发 defer 退款
		return nil, nil, nil, err
	}
	_ = InvalidateUserCache(params.UserId)
	RecordLog(params.UserId, LogTypeSystem,
		fmt.Sprintf("提交退款申请，扣减余额 %s（工单 #%d，冻结待审核）",
			logger.LogQuota(params.RefundQuota), ticket.Id))
	return ticket, refund, message, nil
}

// SumUserPendingRefundQuota 汇总用户当前处于 Pending 状态的退款工单累计申请额度。
//
// 注意：这笔钱在退款工单提交时已通过 DecreaseUserQuota 从 user.quota 中**扣除**，
// 并不是账户层面意义上的"冻结"。此处数字仅供管理员审计参考（"这位用户还有 X 额度在等审核"），
// 切勿把它再加回到可用余额上。对应字段 TicketRefund.RefundQuota（与 FrozenQuota 同值，取任一即可）。
func SumUserPendingRefundQuota(userId int) (int64, error) {
	var total int64
	err := DB.Model(&TicketRefund{}).
		Where("user_id = ? AND refund_status = ?", userId, RefundStatusPending).
		Select("COALESCE(SUM(refund_quota), 0)").
		Scan(&total).Error
	return total, err
}

func GetTicketRefundByTicketId(ticketId int) (*TicketRefund, error) {
	var refund TicketRefund
	if err := DB.Where("ticket_id = ?", ticketId).First(&refund).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTicketRefundNotFound
		}
		return nil, err
	}
	return &refund, nil
}

// UpdateRefundStatusParams 描述管理员处理退款时的参数。
// ActualRefundQuota 的语义随 QuotaMode 变化（仅在 RefundStatus == RefundStatusRefunded 时生效）：
//   - QuotaMode == "" 或 RefundQuotaModeWriteOff：核销已扣余额，ActualRefundQuota 被忽略；
//   - RefundQuotaModeSubtract：实际核销 ActualRefundQuota（可大于或小于原申请额度）；
//     若 Y > RefundQuota 则额外扣减 (Y-RefundQuota)；若 Y < RefundQuota 则退还 (RefundQuota-Y)；
//     ActualRefundQuota 必须 > 0；
//   - RefundQuotaModeOverride：先退还原已扣 RefundQuota，再把用户余额强制设置为 ActualRefundQuota；
//     ActualRefundQuota 必须 ≥ 0。
//
// 驳回（RefundStatusRejected）：忽略 ActualRefundQuota / QuotaMode，永远退还 RefundQuota 到用户余额。
type UpdateRefundStatusParams struct {
	TicketId          int
	AdminId           int
	RefundStatus      int
	QuotaMode         string
	ActualRefundQuota int
}

// UpdateRefundStatus 管理员处理退款。
// 只有处于 pending 的退款才能被处理，防止二次扣减/退还。
//
// 账户余额变更一律走 IncreaseUserQuota / DecreaseUserQuota，
// 工单/退款业务数据的状态流转在事务中，账户操作在事务前或事务后完成，保证：
//   - 事务失败不会遗留已扣/已退的账户变更（通过补偿回滚）；
//   - 账户变更经由系统统一路径，Redis、BatchUpdate、消费路径等都自动一致。
//
// 第三个返回值是更新前的工单主状态，供调用方判断是否需要发出状态变更通知。
func UpdateRefundStatus(params UpdateRefundStatusParams) (*TicketRefund, *Ticket, int, error) {
	if params.RefundStatus != RefundStatusRefunded && params.RefundStatus != RefundStatusRejected {
		return nil, nil, 0, ErrTicketRefundStatusInvalid
	}
	mode := strings.TrimSpace(params.QuotaMode)
	if mode == "" {
		mode = RefundQuotaModeWriteOff
	}
	if params.RefundStatus == RefundStatusRefunded {
		switch mode {
		case RefundQuotaModeWriteOff:
			// ok
		case RefundQuotaModeSubtract:
			if params.ActualRefundQuota <= 0 {
				return nil, nil, 0, ErrTicketRefundQuotaInvalid
			}
		case RefundQuotaModeOverride:
			if params.ActualRefundQuota < 0 {
				return nil, nil, 0, ErrTicketRefundQuotaInvalid
			}
		default:
			return nil, nil, 0, ErrTicketRefundQuotaModeInvalid
		}
	}

	// 1) 先加载并校验状态。
	var refund TicketRefund
	if err := DB.Where("ticket_id = ?", params.TicketId).First(&refund).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, 0, ErrTicketRefundNotFound
		}
		return nil, nil, 0, err
	}
	var ticket Ticket
	if err := DB.First(&ticket, "id = ?", params.TicketId).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, 0, ErrTicketNotFound
		}
		return nil, nil, 0, err
	}
	if refund.RefundStatus != RefundStatusPending {
		return nil, nil, 0, ErrTicketRefundNotPending
	}

	prevStatus := ticket.Status
	originalQuota := refund.RefundQuota // 提交时已经扣除的金额
	userId := refund.UserId

	// 2) 在更新工单状态前，先完成账户余额变更；失败直接返回，工单状态保持 pending 不变，下次还能处理。
	var (
		finalQuotaForOverride int
		oldQuotaForOverride   int
	)
	switch params.RefundStatus {
	case RefundStatusRefunded:
		switch mode {
		case RefundQuotaModeWriteOff:
			// 提交时已扣除，此处不再变更账户。
		case RefundQuotaModeSubtract:
			// 实际核销金额 Y：若 Y > 原扣额，追加扣减 Y-原；若 Y < 原扣额，退还 原-Y；相等则无账户变更。
			diff := params.ActualRefundQuota - originalQuota
			if diff > 0 {
				// 追加扣减前预检一次，避免扣成负数。
				var curQuota int
				if err := DB.Model(&User{}).Where("id = ?", userId).
					Select("quota").Find(&curQuota).Error; err != nil {
					return nil, nil, 0, err
				}
				if diff > curQuota {
					return nil, nil, 0, ErrTicketRefundQuotaExceed
				}
				if err := DecreaseUserQuota(userId, diff, true); err != nil {
					return nil, nil, 0, err
				}
			} else if diff < 0 {
				if err := IncreaseUserQuota(userId, -diff, true); err != nil {
					return nil, nil, 0, err
				}
			}
		case RefundQuotaModeOverride:
			// 先把已扣金额退还，相当于"当作没有过退款"；然后把余额覆盖为目标值。
			if originalQuota > 0 {
				if err := IncreaseUserQuota(userId, originalQuota, true); err != nil {
					return nil, nil, 0, err
				}
			}
			// 读此时余额作为快照，再覆盖。
			if err := DB.Model(&User{}).Where("id = ?", userId).
				Select("quota").Find(&oldQuotaForOverride).Error; err != nil {
				// 已退还，但快照失败：回滚退还，保证账户总额守恒。
				if rErr := DecreaseUserQuota(userId, originalQuota, true); rErr != nil {
					common.SysError(fmt.Sprintf(
						"refund override snapshot failed and compensation decrease also failed: user=%d, quota=%d, err=%v",
						userId, originalQuota, rErr))
				}
				return nil, nil, 0, err
			}
			if err := DB.Model(&User{}).Where("id = ?", userId).
				Update("quota", params.ActualRefundQuota).Error; err != nil {
				// 覆盖失败：回滚退还。
				if rErr := DecreaseUserQuota(userId, originalQuota, true); rErr != nil {
					common.SysError(fmt.Sprintf(
						"refund override write failed and compensation decrease also failed: user=%d, quota=%d, err=%v",
						userId, originalQuota, rErr))
				}
				return nil, nil, 0, err
			}
			_ = InvalidateUserCache(userId)
			finalQuotaForOverride = params.ActualRefundQuota
		}
	case RefundStatusRejected:
		if originalQuota > 0 {
			if err := IncreaseUserQuota(userId, originalQuota, true); err != nil {
				return nil, nil, 0, err
			}
		}
	}

	// 3) 业务数据状态流转（工单/退款记录）；失败则补偿账户变更。
	now := common.GetTimestamp()
	txErr := DB.Transaction(func(tx *gorm.DB) error {
		refundUpdates := map[string]interface{}{
			"refund_status":  params.RefundStatus,
			"processed_time": now,
			"frozen_quota":   0,
		}
		ticketUpdates := map[string]interface{}{
			"updated_time": now,
			"admin_id":     params.AdminId,
		}
		if params.RefundStatus == RefundStatusRefunded {
			ticketUpdates["status"] = TicketStatusResolved
		} else {
			ticketUpdates["status"] = TicketStatusProcessing
		}
		res := tx.Model(&TicketRefund{}).
			Where("id = ? AND refund_status = ?", refund.Id, RefundStatusPending).
			Updates(refundUpdates)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			// 并发保护：已被其他管理员处理过。
			return ErrTicketRefundNotPending
		}
		if err := tx.Model(&Ticket{}).Where("id = ?", ticket.Id).Updates(ticketUpdates).Error; err != nil {
			return err
		}
		refund.RefundStatus = params.RefundStatus
		refund.ProcessedTime = now
		refund.FrozenQuota = 0
		if status, ok := ticketUpdates["status"].(int); ok {
			ticket.Status = status
		}
		ticket.AdminId = params.AdminId
		ticket.UpdatedTime = now
		return nil
	})
	if txErr != nil {
		// 补偿：刚刚对账户余额做的变更需要反向执行，避免"账户已动但工单仍处于 pending"的不一致。
		compensateErr := compensateAccountChange(
			userId, params.RefundStatus, mode, originalQuota, params.ActualRefundQuota,
			oldQuotaForOverride,
		)
		if compensateErr != nil {
			// 补偿也失败：只能记录严重错误并返回事务错误；工单仍是 pending，管理员可重试。
			common.SysError(fmt.Sprintf(
				"refund status update failed and account compensation also failed: user=%d, ticket=%d, tx_err=%v, compensate_err=%v",
				userId, ticket.Id, txErr, compensateErr))
		}
		return nil, nil, 0, txErr
	}

	_ = InvalidateUserCache(userId)

	// 4) 写审计日志（账户变更 + 工单动作）。
	adminInfo := map[string]interface{}{"admin_id": params.AdminId}
	switch params.RefundStatus {
	case RefundStatusRefunded:
		var logContent string
		switch mode {
		case RefundQuotaModeWriteOff:
			logContent = fmt.Sprintf("管理员批准退款，核销已扣余额 %s（工单 #%d）",
				logger.LogQuota(originalQuota), ticket.Id)
		case RefundQuotaModeSubtract:
			diff := params.ActualRefundQuota - originalQuota
			var delta string
			switch {
			case diff > 0:
				delta = fmt.Sprintf("追加扣减 %s", logger.LogQuota(diff))
			case diff < 0:
				delta = fmt.Sprintf("退还 %s", logger.LogQuota(-diff))
			default:
				delta = "余额无变动"
			}
			logContent = fmt.Sprintf("管理员批准退款，实际核销 %s（工单 #%d，原申请 %s，%s）",
				logger.LogQuota(params.ActualRefundQuota), ticket.Id,
				logger.LogQuota(originalQuota), delta)
		case RefundQuotaModeOverride:
			logContent = fmt.Sprintf("管理员批准退款并覆盖余额：工单 #%d，原申请 %s；解冻后余额 %s → 覆盖为 %s",
				ticket.Id, logger.LogQuota(originalQuota),
				logger.LogQuota(oldQuotaForOverride), logger.LogQuota(finalQuotaForOverride))
		}
		RecordLogWithAdminInfo(userId, LogTypeManage, logContent, adminInfo)
	case RefundStatusRejected:
		RecordLogWithAdminInfo(userId, LogTypeManage,
			fmt.Sprintf("管理员驳回退款，退还额度 %s（工单 #%d）",
				logger.LogQuota(originalQuota), ticket.Id), adminInfo)
	}

	return &refund, &ticket, prevStatus, nil
}

// compensateAccountChange 反向执行 UpdateRefundStatus 在工单事务失败前已完成的账户变更。
// 仅在"账户变更已完成但后续事务失败"时调用，目的是保证账户总额守恒、工单仍停留在 pending 可重试。
func compensateAccountChange(
	userId int,
	refundStatus int,
	mode string,
	originalQuota int,
	actualRefundQuota int,
	oldQuotaForOverride int,
) error {
	switch refundStatus {
	case RefundStatusRefunded:
		switch mode {
		case RefundQuotaModeWriteOff:
			return nil
		case RefundQuotaModeSubtract:
			diff := actualRefundQuota - originalQuota
			if diff > 0 {
				return IncreaseUserQuota(userId, diff, true)
			} else if diff < 0 {
				return DecreaseUserQuota(userId, -diff, true)
			}
			return nil
		case RefundQuotaModeOverride:
			// 把余额改回 oldQuotaForOverride（"已退还 + 未覆盖"那一刻），再扣回 originalQuota。
			if err := DB.Model(&User{}).Where("id = ?", userId).
				Update("quota", oldQuotaForOverride).Error; err != nil {
				return err
			}
			_ = InvalidateUserCache(userId)
			if originalQuota > 0 {
				return DecreaseUserQuota(userId, originalQuota, true)
			}
			return nil
		}
	case RefundStatusRejected:
		if originalQuota > 0 {
			return DecreaseUserQuota(userId, originalQuota, true)
		}
		return nil
	}
	return nil
}
