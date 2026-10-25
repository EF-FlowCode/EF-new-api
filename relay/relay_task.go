package relay

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/dto"
	"eflow.local/new-api/model"
	pluginhost "eflow.local/new-api/relay/channel/task/jsplugin"
	relaycommon "eflow.local/new-api/relay/common"
	relayconstant "eflow.local/new-api/relay/constant"
	"eflow.local/new-api/service"
	management "eflow.local/new-api/service/taskplugin"
	"github.com/gin-gonic/gin"
)

type TaskSubmitResult struct {
	SavedTask      *model.Task
	Replayed       bool
	UpstreamTaskID string
	TaskData       []byte
	Platform       constant.TaskPlatform
	Quota          int
	Plugin         *model.TaskPluginIdentity
	PluginContext  *model.TaskPluginContext
	//PerCallPrice   types.PriceData
}

// ResolveOriginTask 处理基于已有任务的提交（remix / continuation）：
// 查找原始任务、从中提取模型名称、将渠道锁定到原始任务的渠道
// （通过 info.LockedChannel，重试时复用同一渠道并轮换 key），
// 插件提交阶段按原任务的持久 usage 校验 remix 参数。
// 该函数在控制器的重试循环之前调用一次，其结果通过 info 字段和上下文持久化。
func ResolveOriginTask(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	// 检测 remix action
	path := c.Request.URL.Path
	if strings.Contains(path, "/v1/videos/") && strings.HasSuffix(path, "/remix") {
		info.Action = constant.TaskActionRemix
	}

	// 提取 remix 任务的 video_id
	if info.Action == constant.TaskActionRemix {
		videoID := c.Param("video_id")
		if strings.TrimSpace(videoID) == "" {
			return service.TaskErrorWrapperLocal(fmt.Errorf("video_id is required"), "invalid_request", http.StatusBadRequest)
		}
		info.OriginTaskID = videoID
	}

	if info.OriginTaskID == "" {
		return nil
	}

	// 查找原始任务
	originTask, exist, err := model.GetByTaskId(info.UserId, info.OriginTaskID)
	if err != nil {
		return service.TaskErrorWrapper(err, "get_origin_task_failed", http.StatusInternalServerError)
	}
	if !exist {
		return service.TaskErrorWrapperLocal(errors.New("task_origin_not_exist"), "task_not_exist", http.StatusBadRequest)
	}

	// 从原始任务推导模型名称
	if info.OriginModelName == "" {
		if originTask.Properties.OriginModelName != "" {
			info.OriginModelName = originTask.Properties.OriginModelName
		} else if originTask.Properties.UpstreamModelName != "" {
			info.OriginModelName = originTask.Properties.UpstreamModelName
		} else {
			var taskData map[string]any
			_ = common.Unmarshal(originTask.Data, &taskData)
			if m, ok := taskData["model"].(string); ok && m != "" {
				info.OriginModelName = m
			}
		}
	}

	// 锁定到原始任务的渠道（重试时复用同一渠道，轮换 key）
	ch, err := model.GetChannelById(originTask.ChannelId, true)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "channel_not_found", http.StatusBadRequest)
	}
	if ch.Status != common.ChannelStatusEnabled {
		return service.TaskErrorWrapperLocal(errors.New("the channel of the origin task is disabled"), "task_channel_disable", http.StatusBadRequest)
	}
	info.LockedChannel = ch

	if originTask.ChannelId != info.ChannelId {
		key, keyIdx, newAPIError := ch.GetNextEnabledKeyForModel(c, info.OriginModelName)
		if newAPIError != nil {
			return service.TaskErrorWrapper(newAPIError, "channel_no_available_key", newAPIError.StatusCode)
		}
		common.SetContextKey(c, constant.ContextKeyChannelKey, key)
		common.SetContextKey(c, constant.ContextKeyChannelType, ch.Type)
		common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, ch.GetBaseURL())
		common.SetContextKey(c, constant.ContextKeyChannelId, originTask.ChannelId)
		// 与 distributor 主路径对齐：multi-key 渠道把选中的 key index 写进 ctx，
		// 让后续 controller/relay.go 的 ReportError 能定位到精确 (key, model)。
		if ch.ChannelInfo.IsMultiKey {
			common.SetContextKey(c, constant.ContextKeyChannelIsMultiKey, true)
			common.SetContextKey(c, constant.ContextKeyChannelMultiKeyIndex, keyIdx)
		}

		info.ChannelBaseUrl = ch.GetBaseURL()
		info.ChannelId = originTask.ChannelId
		info.ChannelType = ch.Type
		info.ApiKey = key
	}

	return nil
}

// RelayTaskSubmit dispatches task submissions exclusively through the plugin host.
func RelayTaskSubmit(c *gin.Context, info *relaycommon.RelayInfo) (*TaskSubmitResult, *dto.TaskError) {
	info.InitChannelMeta(c)
	platform := constant.TaskPlatform(c.GetString("platform"))
	if platform == "" {
		platform = GetTaskPlatform(c)
	}
	if model.TaskPluginKeyForChannel(info.ChannelType) == "" {
		return nil, service.TaskErrorWrapperLocal(fmt.Errorf("invalid api platform: %s", platform), "invalid_api_platform", http.StatusBadRequest)
	}
	return relayPluginTaskSubmit(c, info, platform)
}

func submitPreparedTaskPlugin(c *gin.Context, info *relaycommon.RelayInfo, platform constant.TaskPlatform, prepared *management.ProductionPrepared, executor pluginhost.Executor) (result *TaskSubmitResult, taskErr *dto.TaskError) {
	return submitPreparedTaskPluginFiles(c, info, platform, prepared, executor, nil)
}

func submitPreparedTaskPluginFiles(c *gin.Context, info *relaycommon.RelayInfo, platform constant.TaskPlatform, prepared *management.ProductionPrepared, executor pluginhost.Executor, files map[string]pluginhost.File) (result *TaskSubmitResult, taskErr *dto.TaskError) {
	defer func() {
		if taskErr != nil {
			taskErr.SkipRetry = true
		}
	}()
	response, err := prepared.Execute(c.Request.Context(), executor, info.ChannelBaseUrl, files)
	if err != nil {
		return nil, service.TaskErrorWrapper(err, "plugin_request_failed", http.StatusBadGateway)
	}
	if response.Status < 200 || response.Status >= 300 {
		return nil, service.TaskErrorWrapper(fmt.Errorf("plugin upstream returned HTTP %d", response.Status), "plugin_upstream_failed", http.StatusBadGateway)
	}
	parsed, err := prepared.ParseSubmit(c.Request.Context(), response)
	if err != nil {
		return nil, service.TaskErrorWrapperLocal(err, "plugin_response_invalid", http.StatusBadGateway)
	}
	taskID, _ := parsed["taskId"].(string)
	data := response.Body
	if value, ok := parsed["taskData"]; ok {
		data = value
	}
	taskData, _ := common.Marshal(data)
	state, _ := parsed["state"].(map[string]any)
	facts := prepared.Facts
	identity := &model.TaskPluginIdentity{IdentityVersion: 1, APIVersion: prepared.Handle.Meta().APIVersion, Key: prepared.Identity.Key, Version: prepared.Identity.Version, SourceHash: prepared.Identity.SourceHash, Factory: prepared.Binding.Factory}
	return &TaskSubmitResult{UpstreamTaskID: taskID, TaskData: taskData, Platform: platform, Quota: info.PriceData.Quota, Plugin: identity, PluginContext: &model.TaskPluginContext{State: state, Usage: facts}}, nil
}

var fetchRespBuilders = map[int]func(c *gin.Context) (respBody []byte, taskResp *dto.TaskError){
	relayconstant.RelayModeSunoFetchByID:  sunoFetchByIDRespBodyBuilder,
	relayconstant.RelayModeSunoFetch:      sunoFetchRespBodyBuilder,
	relayconstant.RelayModeVideoFetchByID: videoFetchByIDRespBodyBuilder,
}

func RelayTaskFetch(c *gin.Context, relayMode int) (taskResp *dto.TaskError) {
	respBuilder, ok := fetchRespBuilders[relayMode]
	if !ok {
		taskResp = service.TaskErrorWrapperLocal(errors.New("invalid_relay_mode"), "invalid_relay_mode", http.StatusBadRequest)
	}

	respBody, taskErr := respBuilder(c)
	if taskErr != nil {
		return taskErr
	}
	if len(respBody) == 0 {
		respBody = []byte("{\"code\":\"success\",\"data\":null}")
	}

	c.Writer.Header().Set("Content-Type", "application/json")
	_, err := io.Copy(c.Writer, bytes.NewBuffer(respBody))
	if err != nil {
		taskResp = service.TaskErrorWrapper(err, "copy_response_body_failed", http.StatusInternalServerError)
		return
	}
	return
}

func sunoFetchRespBodyBuilder(c *gin.Context) (respBody []byte, taskResp *dto.TaskError) {
	userId := c.GetInt("id")
	var condition = struct {
		IDs    []any  `json:"ids"`
		Action string `json:"action"`
	}{}
	err := c.BindJSON(&condition)
	if err != nil {
		taskResp = service.TaskErrorWrapper(err, "invalid_request", http.StatusBadRequest)
		return
	}
	var tasks []any
	if len(condition.IDs) > 0 {
		taskModels, err := model.GetByTaskIds(userId, condition.IDs)
		if err != nil {
			taskResp = service.TaskErrorWrapper(err, "get_tasks_failed", http.StatusInternalServerError)
			return
		}
		for _, task := range taskModels {
			tasks = append(tasks, taskModel2UserDto(task))
		}
	} else {
		tasks = make([]any, 0)
	}
	respBody, err = common.Marshal(dto.TaskResponse[[]any]{
		Code: "success",
		Data: tasks,
	})
	return
}

func sunoFetchByIDRespBodyBuilder(c *gin.Context) (respBody []byte, taskResp *dto.TaskError) {
	taskId := c.Param("id")
	userId := c.GetInt("id")

	originTask, exist, err := model.GetByTaskId(userId, taskId)
	if err != nil {
		taskResp = service.TaskErrorWrapper(err, "get_task_failed", http.StatusInternalServerError)
		return
	}
	if !exist {
		taskResp = service.TaskErrorWrapperLocal(errors.New("task_not_exist"), "task_not_exist", http.StatusBadRequest)
		return
	}

	respBody, err = common.Marshal(dto.TaskResponse[any]{
		Code: "success",
		Data: taskModel2UserDto(originTask),
	})
	return
}

func videoFetchByIDRespBodyBuilder(c *gin.Context) (respBody []byte, taskResp *dto.TaskError) {
	taskId := c.Param("task_id")
	if taskId == "" {
		taskId = c.GetString("task_id")
	}
	userId := c.GetInt("id")

	originTask, exist, err := model.GetByTaskId(userId, taskId)
	if err != nil {
		taskResp = service.TaskErrorWrapper(err, "get_task_failed", http.StatusInternalServerError)
		return
	}
	if !exist {
		taskResp = service.TaskErrorWrapperLocal(errors.New("task_not_exist"), "task_not_exist", http.StatusBadRequest)
		return
	}

	isOpenAIVideoAPI := strings.HasPrefix(c.Request.RequestURI, "/v1/videos/")

	// OpenAI Video API uses the host presentation of persisted task state.
	if isOpenAIVideoAPI {
		if originTask.PrivateData.PluginContext != nil && originTask.PrivateData.PluginContext.PublicProtocol == "openai_video" {
			task, handle, err := management.Default.PublicTask(c.Request.Context(), taskId, userId, "openai_video")
			if err != nil {
				return nil, service.TaskErrorWrapperLocal(errors.New("task is unavailable"), "task_unavailable", http.StatusServiceUnavailable)
			}
			out, err := management.RenderPublicVideo(c.Request.Context(), handle, task)
			if err != nil {
				return nil, service.TaskErrorWrapperLocal(errors.New("task cannot be rendered"), "task_render_failed", http.StatusInternalServerError)
			}
			raw, err := common.Marshal(out)
			if err != nil {
				return nil, service.TaskErrorWrapperLocal(err, "marshal_response_failed", http.StatusInternalServerError)
			}
			return raw, nil
		}
		if originTask.PrivateData.Plugin != nil || model.TaskPluginKeyForPlatform(originTask.Platform) != "" {
			return pluginVideoResponse(originTask), nil
		}
		return nil, service.TaskErrorWrapperLocal(fmt.Errorf("invalid channel id: %d", originTask.ChannelId), "invalid_channel_id", http.StatusBadRequest)
	}

	// 通用 TaskDto 格式
	respBody, err = common.Marshal(dto.TaskResponse[any]{
		Code: "success",
		Data: taskModel2UserDto(originTask),
	})
	if err != nil {
		taskResp = service.TaskErrorWrapper(err, "marshal_response_failed", http.StatusInternalServerError)
	}
	return
}

// taskModel2UserDto is the relay query view: callers only see their own tasks,
// but which upstream channel served a task is operator-only information.
func taskModel2UserDto(task *model.Task) *dto.TaskDto {
	out := TaskModel2Dto(task)
	out.ChannelId = 0
	return out
}

func TaskModel2Dto(task *model.Task) *dto.TaskDto {
	return &dto.TaskDto{
		ID:         task.ID,
		CreatedAt:  task.CreatedAt,
		UpdatedAt:  task.UpdatedAt,
		TaskID:     task.TaskID,
		Platform:   string(task.Platform),
		UserId:     task.UserId,
		Group:      task.Group,
		ChannelId:  task.ChannelId,
		Quota:      task.Quota,
		Action:     task.Action,
		Status:     string(task.Status),
		FailReason: task.FailReason,
		ResultURL:  task.GetResultURL(),
		SubmitTime: task.SubmitTime,
		StartTime:  task.StartTime,
		FinishTime: task.FinishTime,
		Progress:   task.Progress,
		Properties: task.Properties,
		Username:   task.Username,
		Data:       task.Data,
	}
}
