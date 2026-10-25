package model

import (
	"errors"

	"eflow.local/new-api/common"
	"gorm.io/gorm"
)

// TicketAttachment 描述工单消息附件元数据。
// 附件先通过独立接口上传（MessageId=0），在提交/回复消息时绑定到具体的 TicketMessage。
// 实际文件内容存储在 StorageType 指定的后端（本地磁盘 / 对象存储），DB 只保存必要的元信息。
type TicketAttachment struct {
	Id          int            `json:"id"`
	TicketId    int            `json:"ticket_id" gorm:"index;default:0"`
	MessageId   int            `json:"message_id" gorm:"index;default:0"`
	UserId      int            `json:"user_id" gorm:"index;not null"`
	FileName    string         `json:"file_name" gorm:"type:varchar(255);not null"`
	StoredName  string         `json:"-" gorm:"type:varchar(128);not null"`
	MimeType    string         `json:"mime_type" gorm:"type:varchar(128)"`
	Size        int64          `json:"size" gorm:"type:bigint;default:0"`
	StorageType string         `json:"storage_type" gorm:"type:varchar(16);default:'local'"`
	StorageKey  string         `json:"-" gorm:"type:varchar(512)"`
	Sha256      string         `json:"sha256" gorm:"type:varchar(64);index"`
	CreatedTime int64          `json:"created_time" gorm:"bigint;index"`
	DeletedAt   gorm.DeletedAt `json:"-" gorm:"index"`
}

var (
	ErrAttachmentNotFound  = errors.New("ticket attachment not found")
	ErrAttachmentForbidden = errors.New("ticket attachment forbidden")
	ErrAttachmentBound     = errors.New("ticket attachment already bound")
)

func (a *TicketAttachment) BeforeCreate(tx *gorm.DB) error {
	if a.CreatedTime == 0 {
		a.CreatedTime = common.GetTimestamp()
	}
	return nil
}

// CreateTicketAttachment 持久化一条附件记录（已落盘之后调用）。
func CreateTicketAttachment(a *TicketAttachment) error {
	return DB.Create(a).Error
}

// GetTicketAttachmentById 用于下载/预览前的鉴权查询。
// 返回记录不包含软删除，调用方需结合 ticket 归属进一步校验。
func GetTicketAttachmentById(id int) (*TicketAttachment, error) {
	var a TicketAttachment
	if err := DB.First(&a, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrAttachmentNotFound
		}
		return nil, err
	}
	return &a, nil
}

// GetAttachmentsByMessageIds 批量加载一组消息的附件，用于详情页一次性渲染。
func GetAttachmentsByMessageIds(messageIds []int) ([]*TicketAttachment, error) {
	if len(messageIds) == 0 {
		return []*TicketAttachment{}, nil
	}
	var list []*TicketAttachment
	if err := DB.Where("message_id IN ?", messageIds).Order("id asc").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// BindAttachmentsToMessage 把一批未绑定的附件挂到刚创建的 message 上。
// 必须在事务内调用，校验：
//  1. 附件存在且未被软删除；
//  2. 上传者必须是当前操作用户；
//  3. 附件未绑定到其它消息（MessageId == 0）。
//
// 严格失败：任何一条不通过，整个事务回滚，避免"部分附件丢失"导致用户困惑。
func BindAttachmentsToMessage(tx *gorm.DB, attachmentIds []int, ticketId int, messageId int, userId int) error {
	if len(attachmentIds) == 0 {
		return nil
	}
	var list []*TicketAttachment
	if err := tx.Where("id IN ?", attachmentIds).Find(&list).Error; err != nil {
		return err
	}
	if len(list) != len(attachmentIds) {
		return ErrAttachmentNotFound
	}
	for _, a := range list {
		if a.UserId != userId {
			return ErrAttachmentForbidden
		}
		if a.MessageId != 0 {
			return ErrAttachmentBound
		}
	}
	now := common.GetTimestamp()
	// 条件更新：并发撤回/清理/另一条消息先绑定时行数不足，整条消息回滚。
	result := tx.Model(&TicketAttachment{}).
		Where("id IN ? AND user_id = ? AND message_id = 0", attachmentIds, userId).
		Updates(map[string]any{
			"ticket_id":    ticketId,
			"message_id":   messageId,
			"created_time": now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != int64(len(attachmentIds)) {
		return ErrAttachmentBound
	}
	return nil
}

// GetStaffAttachmentsByTicket 取该工单所有"由客服/管理员上传并已绑定到消息"的附件。
//
// eflow: 判定口径——附件上传者不是工单提交者(user_id <> ownerUserId)且已绑定消息
// (message_id <> 0)。用于：(1) 开票前置校验是否已上传发票文件；(2) 开票通知邮件随附
// 发票文件。软删除记录由 GORM 的 DeletedAt 自动排除。
func GetStaffAttachmentsByTicket(ticketId int, ownerUserId int) ([]*TicketAttachment, error) {
	var list []*TicketAttachment
	if err := DB.Where("ticket_id = ? AND message_id <> 0 AND user_id <> ?", ticketId, ownerUserId).
		Order("id asc").
		Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// DeleteUnboundAttachment 仅在附件仍未绑定消息时软删除记录，返回是否删除。
// 调用方只在返回 true 后再删真实文件，避免与发送消息并发时删掉已绑定附件。
func DeleteUnboundAttachment(id int) (bool, error) {
	result := DB.Where("message_id = 0").Delete(&TicketAttachment{}, id)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

// ListOrphanAttachments 返回超过 olderThan 秒仍未绑定到消息的附件，供清理任务使用。
func ListOrphanAttachments(olderThan int64, limit int) ([]*TicketAttachment, error) {
	var list []*TicketAttachment
	cutoff := common.GetTimestamp() - olderThan
	err := DB.Where("message_id = 0 AND created_time < ?", cutoff).
		Order("id asc").
		Limit(limit).
		Find(&list).Error
	return list, err
}
