package service

import (
	"testing"

	"eflow.local/new-api/model"
	"github.com/stretchr/testify/require"
)

// 按上游 b22a43b6e：任务结算的差额 / 退款日志也要带 is_task，否则会被当作普通 relay 日志展示。
func TestTaskBillingOtherMarksTaskLogs(t *testing.T) {
	for _, task := range []*model.Task{
		{},
		{PrivateData: model.TaskPrivateData{BillingContext: &model.TaskBillingContext{ModelPrice: 0.1, GroupRatio: 1}}},
	} {
		require.Equal(t, true, taskBillingOther(task)["is_task"])
	}
}
