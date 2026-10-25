package model

import (
	"bytes"
	"database/sql/driver"
	"eflow.local/new-api/pkg/billingexpr"
	"encoding/json"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/dto"
	commonRelay "eflow.local/new-api/relay/common"
)

type TaskStatus string

func (t TaskStatus) ToVideoStatus() string {
	var status string
	switch t {
	case TaskStatusQueued, TaskStatusSubmitted:
		status = dto.VideoStatusQueued
	case TaskStatusInProgress, TaskStatusBillingPending:
		status = dto.VideoStatusInProgress
	case TaskStatusSuccess:
		status = dto.VideoStatusCompleted
	case TaskStatusFailure:
		status = dto.VideoStatusFailed
	default:
		status = dto.VideoStatusUnknown // Default fallback
	}
	return status
}

const (
	TaskStatusNotStart       TaskStatus = "NOT_START"
	TaskStatusSubmitted                 = "SUBMITTED"
	TaskStatusQueued                    = "QUEUED"
	TaskStatusInProgress                = "IN_PROGRESS"
	TaskStatusFailure                   = "FAILURE"
	TaskStatusSuccess                   = "SUCCESS"
	TaskStatusUnknown                   = "UNKNOWN"
	TaskStatusBillingPending            = "BILLING_PENDING"
)

type Task struct {
	Revision        uint64                `json:"-" gorm:"not null;default:0"`
	BatchLeaseUntil int64                 `json:"-" gorm:"not null;default:0"`
	ID              int64                 `json:"id" gorm:"primary_key;AUTO_INCREMENT"`
	CreatedAt       int64                 `json:"created_at" gorm:"index"`
	UpdatedAt       int64                 `json:"updated_at"`
	TaskID          string                `json:"task_id" gorm:"type:varchar(191);index"` // 第三方id，不一定有/ song id\ Task id
	Platform        constant.TaskPlatform `json:"platform" gorm:"type:varchar(30);index"` // 平台
	UserId          int                   `json:"user_id" gorm:"index"`
	Group           string                `json:"group" gorm:"type:varchar(50)"` // 修正计费用
	ChannelId       int                   `json:"channel_id" gorm:"index"`
	Quota           int                   `json:"quota"`
	Action          string                `json:"action" gorm:"type:varchar(40);index"` // 任务类型, song, lyrics, description-mode
	Status          TaskStatus            `json:"status" gorm:"type:varchar(20);index"` // 任务状态
	FailReason      string                `json:"fail_reason"`
	SubmitTime      int64                 `json:"submit_time" gorm:"index"`
	StartTime       int64                 `json:"start_time" gorm:"index"`
	FinishTime      int64                 `json:"finish_time" gorm:"index"`
	Progress        string                `json:"progress" gorm:"type:varchar(20);index"`
	Properties      Properties            `json:"properties" gorm:"type:json"`
	Username        string                `json:"username,omitempty" gorm:"-"`
	// ChannelDisabled is set only by public reads that serve a terminal result
	// after its channel/binding was disabled; never persisted.
	ChannelDisabled bool `json:"-" gorm:"-"`
	// 禁止返回给用户，内部可能包含key等隐私信息
	PrivateData TaskPrivateData `json:"-" gorm:"column:private_data;type:json"`
	Data        json.RawMessage `json:"data" gorm:"type:json"`
}

func (t *Task) SetData(data any) {
	b, _ := common.Marshal(data)
	t.Data = json.RawMessage(b)
}

func (t *Task) GetData(v any) error {
	return common.Unmarshal(t.Data, &v)
}

type Properties struct {
	Input             string `json:"input"`
	UpstreamModelName string `json:"upstream_model_name,omitempty"`
	OriginModelName   string `json:"origin_model_name,omitempty"`
}

func (m *Properties) Scan(val any) error {
	bytesValue, _ := val.([]byte)
	if len(bytesValue) == 0 {
		*m = Properties{}
		return nil
	}
	return common.Unmarshal(bytesValue, m)
}

func (m Properties) Value() (driver.Value, error) {
	if m == (Properties{}) {
		return nil, nil
	}
	return common.Marshal(m)
}

type TaskPrivateData struct {
	Plugin         *TaskPluginIdentity  `json:"plugin,omitempty"`
	PluginContext  *TaskPluginContext   `json:"plugin_context,omitempty"`
	KnowledgeBatch *KnowledgeBatchState `json:"knowledge_batch,omitempty"`
	Key            string               `json:"key,omitempty"`
	UpstreamTaskID string               `json:"upstream_task_id,omitempty"` // 上游真实 task ID
	ResultURL      string               `json:"result_url,omitempty"`       // 任务成功后的结果 URL（视频地址等）
	// 计费上下文：用于异步退款/差额结算（轮询阶段读取）
	TokenId        int                 `json:"token_id,omitempty"`        // 令牌 ID，用于令牌额度退款
	BillingContext *TaskBillingContext `json:"billing_context,omitempty"` // 计费参数快照（用于轮询阶段重新计算）
}

// PluginContext retains protocol state and validated observations separately
// from immutable source identity and host-owned financial fields.
type TaskPluginContext struct {
	PublicProtocol         string                       `json:"public_protocol,omitempty"`
	NativePath             string                       `json:"native_path,omitempty"`
	NativeMethod           string                       `json:"native_method,omitempty"`
	PublicMode             string                       `json:"public_mode,omitempty"`
	PublicBackground       bool                         `json:"public_background,omitempty"`
	PriceRule              json.RawMessage              `json:"price_rule,omitempty"`
	TokenBilling           *TaskPluginTokenBilling      `json:"token_billing,omitempty"`
	CompletionTokens       *int                         `json:"completion_tokens,omitempty"`
	Expression             *billingexpr.BillingSnapshot `json:"expression,omitempty"`
	State                  map[string]any               `json:"state,omitempty"`
	Usage                  map[string]any               `json:"usage,omitempty"`
	Billing                *TaskPluginBilling           `json:"billing,omitempty"`
	CompletionUsage        map[string]any               `json:"completion_usage,omitempty"`
	CompletionUsageInvalid bool                         `json:"completion_usage_invalid,omitempty"`
	CompletionReady        bool                         `json:"completion_ready,omitempty"`
	BillingPendingReason   string                       `json:"billing_pending_reason,omitempty"`
	BillingRetryAfter      int64                        `json:"billing_retry_after,omitempty"`
}

// Versioned authority for new plugin tasks' optional completion-token pricing.
// ModelRatio is the legacy quota-per-token coefficient, not a USD expression.
// A present disabled snapshot must not fall back to subsequently configured prices.
type TaskPluginTokenBilling struct {
	Version         int      `json:"version"`
	Mode            string   `json:"mode"`
	ModelRatio      float64  `json:"model_ratio,omitempty"`
	GroupRatio      *float64 `json:"group_ratio,omitempty"`
	OtherMultiplier float64  `json:"other_multiplier,omitempty"`
}

// All monetary inputs are captured before submission. Completion hooks supply
// observations only; neither live pricing settings nor scripts supply prices.
type TaskPluginBilling struct {
	Version          int                `json:"version"`
	Mode             string             `json:"mode"`
	BaseQuota        int                `json:"base_quota"`
	ResolutionRatios map[string]float64 `json:"resolution_ratios,omitempty"`
}

// TaskBillingContext 记录任务提交时的计费参数，以便轮询阶段可以重新计算额度。
type TaskBillingContext struct {
	ModelPrice      float64            `json:"model_price,omitempty"`       // 模型单价
	GroupRatio      float64            `json:"group_ratio,omitempty"`       // 分组倍率
	ModelRatio      float64            `json:"model_ratio,omitempty"`       // 模型倍率
	OtherRatios     map[string]float64 `json:"other_ratios,omitempty"`      // 附加倍率（时长、分辨率等）
	OriginModelName string             `json:"origin_model_name,omitempty"` // 模型名称，必须为OriginModelName
	PerCallBilling  bool               `json:"per_call_billing,omitempty"`  // 按次计费：跳过轮询阶段的差额结算
}

// GetUpstreamTaskID 获取上游真实 task ID（用于与 provider 通信）
// 旧数据没有 UpstreamTaskID 时，TaskID 本身就是上游 ID
func (t *Task) GetUpstreamTaskID() string {
	if t.PrivateData.UpstreamTaskID != "" {
		return t.PrivateData.UpstreamTaskID
	}
	return t.TaskID
}

// GetResultURL 获取任务结果 URL（视频地址等）
// 新数据存在 PrivateData.ResultURL 中；旧数据回退到 FailReason（历史兼容）
func (t *Task) GetResultURL() string {
	if t.PrivateData.ResultURL != "" {
		return t.PrivateData.ResultURL
	}
	return t.FailReason
}

// GenerateTaskID 生成对外暴露的 task_xxxx 格式 ID
func GenerateTaskID() string {
	key, _ := common.GenerateRandomCharsKey(32)
	return "task_" + key
}

func (p *TaskPrivateData) Scan(val any) error {
	bytesValue, _ := val.([]byte)
	if len(bytesValue) == 0 {
		*p = TaskPrivateData{}
		return nil
	}
	var decoded TaskPrivateData
	if err := common.Unmarshal(bytesValue, &decoded); err != nil {
		return err
	}
	*p = decoded
	return nil
}

func (p TaskPrivateData) Value() (driver.Value, error) {
	if (p == TaskPrivateData{}) {
		return nil, nil
	}
	return common.Marshal(p)
}

// SyncTaskQueryParams 用于包含所有搜索条件的结构体，可以根据需求添加更多字段
type SyncTaskQueryParams struct {
	Platform       constant.TaskPlatform
	ChannelID      string
	TaskID         string
	UserID         string
	Action         string
	Status         string
	StartTimestamp int64
	EndTimestamp   int64
	UserIDs        []int
}

func InitTask(platform constant.TaskPlatform, relayInfo *commonRelay.RelayInfo) *Task {
	properties := Properties{}
	privateData := TaskPrivateData{}
	if relayInfo != nil && relayInfo.ChannelMeta != nil {
		// eflow: 所有 channel 类型都把提交时实际命中的 key 绑到 task 上，
		// 让异步任务轮询走 PrivateData.Key 而不是 fallback 到整段 channel.Key
		// （多 key 渠道下 channel.Key 是多行文本，回退会把 N 个 key 一起塞进 Authorization）
		privateData.Key = relayInfo.ChannelMeta.ApiKey
		if relayInfo.UpstreamModelName != "" {
			properties.UpstreamModelName = relayInfo.UpstreamModelName
		}
		if relayInfo.OriginModelName != "" {
			properties.OriginModelName = relayInfo.OriginModelName
		}
	}

	// 使用预生成的公开 ID（如果有），否则新生成
	taskID := ""
	if relayInfo.TaskRelayInfo != nil && relayInfo.TaskRelayInfo.PublicTaskID != "" {
		taskID = relayInfo.TaskRelayInfo.PublicTaskID
	} else {
		taskID = GenerateTaskID()
	}

	t := &Task{
		TaskID:      taskID,
		UserId:      relayInfo.UserId,
		Group:       relayInfo.UsingGroup,
		SubmitTime:  time.Now().Unix(),
		Status:      TaskStatusNotStart,
		Progress:    "0%",
		ChannelId:   relayInfo.ChannelId,
		Platform:    platform,
		Properties:  properties,
		PrivateData: privateData,
	}
	return t
}

func TaskGetAllUserTask(userId int, startIdx int, num int, queryParams SyncTaskQueryParams) []*Task {
	var tasks []*Task
	var err error

	// 初始化查询构建器
	query := DB.Where("user_id = ?", userId)

	if queryParams.TaskID != "" {
		query = query.Where("task_id = ?", queryParams.TaskID)
	}
	if queryParams.Action != "" {
		query = query.Where("action = ?", queryParams.Action)
	}
	if queryParams.Status != "" {
		query = query.Where("status = ?", queryParams.Status)
	}
	if queryParams.Platform != "" {
		query = query.Where("platform = ?", queryParams.Platform)
	}
	if queryParams.StartTimestamp != 0 {
		// 假设您已将前端传来的时间戳转换为数据库所需的时间格式，并处理了时间戳的验证和解析
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != 0 {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}

	// 获取数据
	err = query.Omit("channel_id").Order("id desc").Limit(num).Offset(startIdx).Find(&tasks).Error
	if err != nil {
		return nil
	}

	return tasks
}

func TaskGetAllTasks(startIdx int, num int, queryParams SyncTaskQueryParams) []*Task {
	var tasks []*Task
	var err error

	// 初始化查询构建器
	query := DB

	// 添加过滤条件
	if queryParams.ChannelID != "" {
		query = query.Where("channel_id = ?", queryParams.ChannelID)
	}
	if queryParams.Platform != "" {
		query = query.Where("platform = ?", queryParams.Platform)
	}
	if queryParams.UserID != "" {
		query = query.Where("user_id = ?", queryParams.UserID)
	}
	if len(queryParams.UserIDs) != 0 {
		query = query.Where("user_id in (?)", queryParams.UserIDs)
	}
	if queryParams.TaskID != "" {
		query = query.Where("task_id = ?", queryParams.TaskID)
	}
	if queryParams.Action != "" {
		query = query.Where("action = ?", queryParams.Action)
	}
	if queryParams.Status != "" {
		query = query.Where("status = ?", queryParams.Status)
	}
	if queryParams.StartTimestamp != 0 {
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != 0 {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}

	// 获取数据
	err = query.Order("id desc").Limit(num).Offset(startIdx).Find(&tasks).Error
	if err != nil {
		return nil
	}

	return tasks
}

func GetTimedOutUnfinishedTasks(cutoffUnix int64, limit int) []*Task {
	var tasks []*Task
	err := DB.Where("platform != ?", KnowledgeBatchPlatform).Where("progress != ?", "100%").
		Where(pollableTaskCondition).
		Where("status NOT IN ?", []string{TaskStatusFailure, TaskStatusSuccess, TaskStatusBillingPending, TaskStatusSubmitting, TaskStatusSubmitUnknown}).
		Where("submit_time < ?", cutoffUnix).
		Order("submit_time").
		Limit(limit).
		Find(&tasks).Error
	if err != nil {
		return nil
	}
	return tasks
}

// Filter before LIMIT so retired, unadopted tasks cannot starve other workers.
// MySQL is the sole supported database; a JSON object marks plugin identity.
const pollableTaskCondition = "(platform NOT IN ('1','17','24','35','36','41','45','50','51','52','54','55','1001','suno') OR JSON_TYPE(JSON_EXTRACT(private_data, '$.plugin')) = 'OBJECT')"

func GetAllUnFinishSyncTasks(limit int) []*Task {
	var tasks []*Task
	var err error
	// get all tasks progress is not 100%
	err = DB.Where("platform != ?", KnowledgeBatchPlatform).Where(pollableTaskCondition).
		Where("(status != ? OR COALESCE(JSON_EXTRACT(private_data, '$.plugin_context.billing_retry_after'), 0) <= ?)", TaskStatusBillingPending, time.Now().Unix()).
		Where("status NOT IN ?", []TaskStatus{TaskStatusSubmitting, TaskStatusSubmitUnknown}).Where("progress != ?", "100%").Where("status != ?", TaskStatusFailure).Where("status != ?", TaskStatusSuccess).Limit(limit).Order("updated_at, id").Find(&tasks).Error
	if err != nil {
		return nil
	}
	return tasks
}

func GetByTaskId(userId int, taskId string) (*Task, bool, error) {
	if taskId == "" {
		return nil, false, nil
	}
	var task *Task
	var err error
	err = DB.Where("user_id = ? and task_id = ?", userId, taskId).
		First(&task).Error
	exist, err := RecordExist(err)
	if err != nil {
		return nil, false, err
	}
	return task, exist, err
}

func GetByTaskIds(userId int, taskIds []any) ([]*Task, error) {
	if len(taskIds) == 0 {
		return nil, nil
	}
	var task []*Task
	var err error
	err = DB.Where("user_id = ? and task_id in (?)", userId, taskIds).
		Find(&task).Error
	if err != nil {
		return nil, err
	}
	return task, nil
}

func (Task *Task) Insert() error {
	if Task.PrivateData.Plugin != nil {
		return ErrTaskPluginIdentity
	}
	var err error
	err = DB.Create(Task).Error
	return err
}

type taskSnapshot struct {
	Status     TaskStatus
	Progress   string
	StartTime  int64
	FinishTime int64
	FailReason string
	ResultURL  string
	Data       json.RawMessage
}

func (s taskSnapshot) Equal(other taskSnapshot) bool {
	return s.Status == other.Status &&
		s.Progress == other.Progress &&
		s.StartTime == other.StartTime &&
		s.FinishTime == other.FinishTime &&
		s.FailReason == other.FailReason &&
		s.ResultURL == other.ResultURL &&
		bytes.Equal(s.Data, other.Data)
}

func (t *Task) Snapshot() taskSnapshot {
	return taskSnapshot{
		Status:     t.Status,
		Progress:   t.Progress,
		StartTime:  t.StartTime,
		FinishTime: t.FinishTime,
		FailReason: t.FailReason,
		ResultURL:  t.PrivateData.ResultURL,
		Data:       t.Data,
	}
}

func (Task *Task) Update() error {
	var err error
	err = DB.Save(Task).Error
	return err
}

// UpdateWithStatus performs a conditional UPDATE guarded by fromStatus (CAS).
// Returns (true, nil) if this caller won the update, (false, nil) if
// another process already moved the task out of fromStatus.
//
// Uses Model().Select("*").Updates() instead of Save() because GORM's Save
// falls back to INSERT ON CONFLICT when the WHERE-guarded UPDATE matches
// zero rows, which silently bypasses the CAS guard.
func (t *Task) UpdateWithStatus(fromStatus TaskStatus) (bool, error) {
	previous := t.Revision
	t.Revision++
	result := DB.Model(t).Where("status = ? AND revision = ?", fromStatus, previous).Select("*").Updates(t)
	if result.Error != nil || result.RowsAffected == 0 {
		t.Revision = previous
	}
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

type TaskQuotaUsage struct {
	Mode  string  `json:"mode"`
	Count float64 `json:"count"`
}

// TaskCountAllTasks returns total tasks that match the given query params (admin usage)
func TaskCountAllTasks(queryParams SyncTaskQueryParams) int64 {
	var total int64
	query := DB.Model(&Task{})
	if queryParams.ChannelID != "" {
		query = query.Where("channel_id = ?", queryParams.ChannelID)
	}
	if queryParams.Platform != "" {
		query = query.Where("platform = ?", queryParams.Platform)
	}
	if queryParams.UserID != "" {
		query = query.Where("user_id = ?", queryParams.UserID)
	}
	if len(queryParams.UserIDs) != 0 {
		query = query.Where("user_id in (?)", queryParams.UserIDs)
	}
	if queryParams.TaskID != "" {
		query = query.Where("task_id = ?", queryParams.TaskID)
	}
	if queryParams.Action != "" {
		query = query.Where("action = ?", queryParams.Action)
	}
	if queryParams.Status != "" {
		query = query.Where("status = ?", queryParams.Status)
	}
	if queryParams.StartTimestamp != 0 {
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != 0 {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}
	_ = query.Count(&total).Error
	return total
}

// TaskCountAllUserTask returns total tasks for given user
func TaskCountAllUserTask(userId int, queryParams SyncTaskQueryParams) int64 {
	var total int64
	query := DB.Model(&Task{}).Where("user_id = ?", userId)
	if queryParams.TaskID != "" {
		query = query.Where("task_id = ?", queryParams.TaskID)
	}
	if queryParams.Action != "" {
		query = query.Where("action = ?", queryParams.Action)
	}
	if queryParams.Status != "" {
		query = query.Where("status = ?", queryParams.Status)
	}
	if queryParams.Platform != "" {
		query = query.Where("platform = ?", queryParams.Platform)
	}
	if queryParams.StartTimestamp != 0 {
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != 0 {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}
	_ = query.Count(&total).Error
	return total
}
func (t *Task) ToOpenAIVideo() *dto.OpenAIVideo {
	openAIVideo := dto.NewOpenAIVideo()
	openAIVideo.ID = t.TaskID
	openAIVideo.Status = t.Status.ToVideoStatus()
	openAIVideo.Model = t.Properties.OriginModelName
	openAIVideo.SetProgressStr(t.Progress)
	openAIVideo.CreatedAt = t.CreatedAt
	openAIVideo.CompletedAt = t.UpdatedAt
	openAIVideo.SetMetadata("url", t.GetResultURL())
	return openAIVideo
}
