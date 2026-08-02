package model

import (
	"errors"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func nowSecondsMinus(seconds int64) int64 {
	return common.GetTimestamp() - seconds
}

// 确保 time 包被使用，避免未使用导入。
var _ = time.Second

func seedAttachment(t *testing.T, userId int) *TicketAttachment {
	t.Helper()
	a := &TicketAttachment{
		UserId:      userId,
		FileName:    "seed.png",
		StoredName:  "seed.png",
		MimeType:    "image/png",
		Size:        42,
		StorageType: "local",
		StorageKey:  "seed.png",
		Sha256:      "deadbeef",
	}
	require.NoError(t, CreateTicketAttachment(a))
	return a
}

func TestBindAttachmentsToMessage_HappyPath(t *testing.T) {
	truncateAttachments(t)
	a1 := seedAttachment(t, 101)
	a2 := seedAttachment(t, 101)

	err := DB.Transaction(func(tx *gorm.DB) error {
		return BindAttachmentsToMessage(tx, []int{a1.Id, a2.Id}, 99, 77, 101)
	})
	require.NoError(t, err)

	// 两条都应更新到指定消息/工单
	var reloaded []TicketAttachment
	require.NoError(t, DB.Find(&reloaded, []int{a1.Id, a2.Id}).Error)
	for _, a := range reloaded {
		require.Equal(t, 77, a.MessageId)
		require.Equal(t, 99, a.TicketId)
	}
}

func TestBindAttachmentsToMessage_ForeignUserDenied(t *testing.T) {
	truncateAttachments(t)
	a := seedAttachment(t, 101)
	err := DB.Transaction(func(tx *gorm.DB) error {
		return BindAttachmentsToMessage(tx, []int{a.Id}, 1, 1, 202 /* 不是上传者 */)
	})
	if !errors.Is(err, ErrAttachmentForbidden) {
		t.Fatalf("want ErrAttachmentForbidden, got %v", err)
	}
}

func TestBindAttachmentsToMessage_AlreadyBoundDenied(t *testing.T) {
	truncateAttachments(t)
	a := seedAttachment(t, 101)
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		return BindAttachmentsToMessage(tx, []int{a.Id}, 1, 5, 101)
	}))
	err := DB.Transaction(func(tx *gorm.DB) error {
		return BindAttachmentsToMessage(tx, []int{a.Id}, 2, 6, 101)
	})
	if !errors.Is(err, ErrAttachmentBound) {
		t.Fatalf("want ErrAttachmentBound, got %v", err)
	}
}

func TestBindAttachmentsToMessage_EmptyIsNoop(t *testing.T) {
	err := DB.Transaction(func(tx *gorm.DB) error {
		return BindAttachmentsToMessage(tx, nil, 1, 1, 1)
	})
	require.NoError(t, err)
}

func TestBindAttachmentsToMessage_MissingRowDenied(t *testing.T) {
	truncateAttachments(t)
	a := seedAttachment(t, 101)
	// 其中一个 id 不存在
	err := DB.Transaction(func(tx *gorm.DB) error {
		return BindAttachmentsToMessage(tx, []int{a.Id, 99999}, 1, 1, 101)
	})
	if !errors.Is(err, ErrAttachmentNotFound) {
		t.Fatalf("want ErrAttachmentNotFound, got %v", err)
	}
}

func TestListOrphanAttachments(t *testing.T) {
	truncateAttachments(t)
	// 新鲜的不应被返回
	_ = seedAttachment(t, 1)
	// 旧的孤儿：手动将 created_time 设为 2 小时前
	old := seedAttachment(t, 1)
	DB.Model(&TicketAttachment{}).Where("id = ?", old.Id).Update("created_time", nowSecondsMinus(7200))

	orphans, err := ListOrphanAttachments(3600, 100)
	require.NoError(t, err)
	if len(orphans) != 1 || orphans[0].Id != old.Id {
		t.Fatalf("orphans mismatch: %+v", orphans)
	}
}

func truncateAttachments(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.Exec("DELETE FROM ticket_attachments").Error)
}
