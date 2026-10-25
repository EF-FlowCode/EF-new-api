package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"eflow.local/new-api/constant"
	"eflow.local/new-api/model"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
	management "eflow.local/new-api/service/taskplugin"
)

// RecoverPluginSubmission only interprets already persisted provider evidence.
// It never invokes a submission endpoint, even after a process restart.
func RecoverPluginSubmission(ctx context.Context, m *management.Manager, id int64) (*model.Task, error) {
	sub, err := model.GetPluginSubmission(ctx, id)
	if err != nil {
		return nil, err
	}
	if sub.Phase != "response" {
		return nil, errors.New("submission has no recoverable response")
	}
	h, task, err := m.StoredSubmitHandle(ctx, id)
	if err != nil {
		return nil, err
	}
	if task.Status != model.TaskStatusSubmitUnknown {
		return task, nil
	}
	var input map[string]any
	var response host.Response
	if json.Unmarshal([]byte(sub.ParseInput), &input) != nil || json.Unmarshal([]byte(sub.Response), &response) != nil {
		return nil, errors.New("invalid saved submission evidence")
	}
	// HTTP errors can be produced after acceptance by a gateway. Keep the
	// reservation until evidence or a reviewed provider decision resolves it.
	if response.Status < 200 || response.Status >= 300 {
		return nil, errors.New("provider acceptance requires reconciliation")
	}
	parsed, err := h.ParseSubmit(ctx, input, response)
	if err != nil {
		return nil, err
	}
	upstream, _ := parsed["taskId"].(string)
	data := response.Body
	if value, ok := parsed["taskData"]; ok {
		data = value
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	state, _ := parsed["state"].(map[string]any)
	return model.AcceptPluginSubmission(ctx, id, upstream, raw, state)
}

func recoverPluginSubmissions(ctx context.Context) {
	var rows []model.TaskPluginSubmission
	// Backoff is independent of the original send deadline. A failed parse must
	// not keep extending the lock that prevents a Root review.
	now := time.Now().Unix()
	if err := model.DB.WithContext(ctx).Where("phase IN ? AND deadline <= ? AND retry_after <= ?", []string{"prepared", "response"}, now, now).Order("retry_after, task_id").Limit(constant.TaskQueryLimit).Find(&rows).Error; err != nil {
		return
	}
	for _, sub := range rows {
		if ctx.Err() != nil {
			return
		}
		claimed := model.DB.WithContext(ctx).Model(&model.TaskPluginSubmission{}).Where("task_id = ? AND phase = ? AND retry_after <= ?", sub.TaskID, sub.Phase, now).UpdateColumn("retry_after", time.Now().Add(time.Minute).Unix())
		if claimed.Error != nil || claimed.RowsAffected != 1 {
			continue
		}
		if sub.Phase == "prepared" {
			_ = model.RejectUnsentPluginSubmission(ctx, sub.TaskID)
			continue
		}
		_, _ = RecoverPluginSubmission(ctx, management.Default, sub.TaskID)
	}
}
