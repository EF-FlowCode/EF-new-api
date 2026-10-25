package service

import (
	"context"
	"sync"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/model"
	"github.com/bytedance/gopkg/util/gopool"
)

var usageRefundNoticeOnce sync.Once

func StartUsageRefundNoticeTask() {
	usageRefundNoticeOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				if err := model.DeliverUsageRefundNotices(ctx, ""); err != nil {
					common.SysError("usage refund notice delivery: " + err.Error())
				}
				cancel()
				<-ticker.C
			}
		})
	})
}
