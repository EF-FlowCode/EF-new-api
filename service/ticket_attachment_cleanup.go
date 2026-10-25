package service

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/logger"
	"eflow.local/new-api/model"
	"eflow.local/new-api/service/attachment"
	"eflow.local/new-api/setting"

	"github.com/bytedance/gopkg/util/gopool"
)

// 工单附件孤儿清理任务：
// 用户可能先调用 /ticket/attachment 上传文件但最终没提交消息，产生未绑定的记录。
// 这个任务每 10 分钟扫描一次：删除超过 1 小时仍未绑定的附件，同时清理存储里的文件。
//
// 只在主节点运行，避免集群多节点重复删除同一份文件触发 "NoSuchKey" 噪声日志。

const (
	attachmentCleanupInterval = 10 * time.Minute
	attachmentOrphanAgeSec    = 3600 // 1 小时
	attachmentCleanupBatch    = 200
)

var (
	attachmentCleanupOnce    sync.Once
	attachmentCleanupRunning atomic.Bool
)

// StartTicketAttachmentCleanupTask 由 main.go 启动。
// 使用 sync.Once 保证即使被多次调用也只启动一个 goroutine。
func StartTicketAttachmentCleanupTask() {
	attachmentCleanupOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			logger.LogInfo(context.Background(), "ticket attachment cleanup task started")
			ticker := time.NewTicker(attachmentCleanupInterval)
			defer ticker.Stop()

			runTicketAttachmentCleanupOnce()
			for range ticker.C {
				runTicketAttachmentCleanupOnce()
			}
		})
	})
}

func runTicketAttachmentCleanupOnce() {
	if !attachmentCleanupRunning.CompareAndSwap(false, true) {
		return
	}
	defer attachmentCleanupRunning.Store(false)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	orphans, err := model.ListOrphanAttachments(attachmentOrphanAgeSec, attachmentCleanupBatch)
	if err != nil {
		logger.LogWarn(ctx, "list orphan ticket attachments failed: "+err.Error())
		return
	}
	if len(orphans) == 0 {
		return
	}

	settings := setting.GetTicketAttachmentSettings()

	removed := 0
	for _, a := range orphans {
		// 先条件软删记录：期间被绑定到消息的附件保持不动；成功后再幂等删文件。
		deleted, err := model.DeleteUnboundAttachment(a.Id)
		if err != nil {
			logger.LogWarn(ctx, "soft delete orphan ticket attachment record failed: "+err.Error())
			continue
		}
		if !deleted {
			continue
		}
		storage, err := attachment.ForStorageType(settings, a.StorageType)
		if err != nil {
			logger.LogWarn(ctx, "ticket attachment storage unavailable during cleanup: "+err.Error())
		} else if err := storage.Delete(ctx, a.StorageKey); err != nil {
			logger.LogWarn(ctx, "delete orphan ticket attachment storage failed: "+err.Error())
		}
		removed++
	}
	if removed > 0 {
		logger.LogInfo(ctx, fmt.Sprintf("ticket attachment cleanup removed %d orphan(s)", removed))
	}
}
