package relay

import (
	"errors"
	"net/http"
	"strings"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/dto"
	"eflow.local/new-api/model"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
	relaycommon "eflow.local/new-api/relay/common"
	"eflow.local/new-api/relay/helper"
	"eflow.local/new-api/service"
	management "eflow.local/new-api/service/taskplugin"
	"eflow.local/new-api/setting/billing_setting"
	"github.com/gin-gonic/gin"
)

func pluginSubmitInput(info *relaycommon.RelayInfo, body map[string]any) map[string]any {
	action := info.Action
	if action == constant.TaskActionRemix {
		action = "remix"
	}
	return map[string]any{"requestBody": body, "model": info.OriginModelName, "upstreamModel": info.UpstreamModelName, "baseUrl": info.ChannelBaseUrl, "apiKey": info.ApiKey, "publicTaskId": info.PublicTaskID, "action": action}
}

func pluginVideoResponse(task *model.Task) []byte {
	response := dto.NewOpenAIVideo()
	response.ID, response.TaskID = task.TaskID, task.TaskID
	response.Model = task.Properties.OriginModelName
	response.CreatedAt, response.CompletedAt = task.CreatedAt, task.FinishTime
	response.SetProgressStr(task.Progress)
	response.SetMetadata("url", task.GetResultURL())
	switch task.Status {
	case model.TaskStatusSuccess:
		response.Status = dto.VideoStatusCompleted
	case model.TaskStatusFailure:
		response.Status = dto.VideoStatusFailed
		response.Error = &dto.OpenAIVideoError{Code: "task_failed", Message: task.FailReason}
	case model.TaskStatusInProgress, model.TaskStatusBillingPending:
		response.Status = dto.VideoStatusInProgress
	case model.TaskStatusSubmitting, model.TaskStatusSubmitUnknown:
		response.Status = dto.VideoStatusUnknown
	default:
		response.Status = dto.VideoStatusQueued
	}
	raw, _ := common.Marshal(response)
	return raw
}

// relayPluginTaskSubmit owns the complete plugin submission lifecycle up to
// acceptance. Provider validation/conversion happens only in the pinned script;
// pricing, reservation, persistence and final settlement remain host-owned.
func relayPluginTaskSubmit(c *gin.Context, info *relaycommon.RelayInfo, platform constant.TaskPlatform) (result *TaskSubmitResult, taskErr *dto.TaskError) {
	executor, err := service.NewTaskPluginHTTPExecutorForProxy(info.ChannelSetting.Proxy)
	if err != nil {
		e := service.TaskErrorWrapperLocal(err, "plugin_proxy_invalid", 400)
		e.SkipRetry = true
		return nil, e
	}
	if info.ChannelSetting.Proxy != "" {
		defer executor.Client.CloseIdleConnections()
	}
	return relayPluginTaskSubmitWithExecutor(c, info, platform, management.Default, executor)
}

func relayPluginTaskSubmitWithExecutor(c *gin.Context, info *relaycommon.RelayInfo, platform constant.TaskPlatform, manager *management.Manager, executor host.Executor) (result *TaskSubmitResult, taskErr *dto.TaskError) {
	return relayPluginTaskSubmitWithAuth(c, info, platform, manager, executor, service.PrepareTaskPluginAuth)
}

func relayPluginTaskSubmitWithAuth(c *gin.Context, info *relaycommon.RelayInfo, platform constant.TaskPlatform, manager *management.Manager, executor host.Executor, auth service.TaskPluginAuthFunc) (result *TaskSubmitResult, taskErr *dto.TaskError) {
	defer func() {
		if taskErr != nil {
			taskErr.SkipRetry = true
		}
	}()
	body, files, descriptions, err := readPluginInput(c, model.TaskPluginKeyForChannel(info.ChannelType))
	if err != nil {
		return nil, service.TaskErrorWrapperLocal(err, "plugin_request_body_invalid", 400)
	}
	if info.TaskRelayInfo == nil {
		info.TaskRelayInfo = &relaycommon.TaskRelayInfo{}
	}
	if info.ChannelType == constant.ChannelTypeSunoAPI {
		info.Action = strings.ToUpper(c.Param("action"))
		if info.Action == "" {
			info.Action, _ = body["action"].(string)
			info.Action = strings.ToUpper(info.Action)
		}
		if info.OriginModelName == "" {
			info.OriginModelName = "suno_" + strings.ToLower(info.Action)
		}
	}
	modelName, _ := body["model"].(string)
	if info.OriginModelName == "" {
		info.OriginModelName = modelName
	}
	if info.OriginModelName == "" {
		return nil, service.TaskErrorWrapperLocal(errors.New("model is required"), "missing_model", 400)
	}
	if result, taskErr, found := replayPluginSubmission(c, info, body, files); found {
		return result, taskErr
	}
	info.UpstreamModelName = info.OriginModelName
	if err := helper.ModelMappedHelper(c, info, nil); err != nil {
		return nil, service.TaskErrorWrapperLocal(err, "model_mapping_failed", 400)
	}
	if info.PublicTaskID == "" {
		info.PublicTaskID = model.GenerateTaskID()
	}
	if info.ChannelBaseUrl == "" {
		info.ChannelBaseUrl = constant.GetChannelBaseURL(info.ChannelType)
	}
	if err := manager.CheckProductionEnabled(c.Request.Context(), info.ChannelId); err != nil {
		return nil, service.TaskErrorWrapperLocal(err, "plugin_disabled", 503)
	}
	env, err := auth(c.Request.Context(), info.ChannelType, info.ChannelBaseUrl, info.ApiKey, info.ApiVersion, executor)
	if err != nil {
		return nil, service.TaskErrorWrapperLocal(err, "plugin_auth_failed", 502)
	}
	info.ChannelBaseUrl = env.BaseURL
	input := pluginSubmitInput(info, body)
	input["auth"], input["authHeader"], input["userSetting"], input["files"] = env.Auth, env.AuthHeader, env.UserSetting, descriptions
	if route := service.TaskPublicRoute(c); route != nil {
		channel, channelErr := model.GetChannelById(info.ChannelId, false)
		if channelErr != nil || !manager.Allows(route, channel) {
			return nil, service.TaskErrorWrapperLocal(errors.New("public task binding changed"), "plugin_binding_changed", 409)
		}
		input["upstreamModel"] = route.Targets[info.ChannelId].Model
		input["publicProtocol"] = route.Protocol
		if route.Native != nil {
			input["nativePath"], input["nativeMethod"], input["nativeQuery"] = route.Native.Path, route.Native.Method, route.Native.Query
		}
		protocolBody, bodyErr := readPluginProtocolBody(c, descriptions)
		if bodyErr != nil {
			return nil, service.TaskErrorWrapperLocal(bodyErr, "plugin_protocol_invalid", 400)
		}
		input["protocolBody"] = protocolBody
	}
	var originUpstreamModel string
	if info.OriginTaskID != "" {
		origin, exists, err := model.GetByTaskId(info.UserId, info.OriginTaskID)
		if err != nil || !exists || origin.ChannelId != info.ChannelId || origin.Status != model.TaskStatusSuccess || origin.PrivateData.Plugin == nil || origin.PrivateData.Plugin.Key != model.TaskPluginKeyForChannel(info.ChannelType) {
			return nil, service.TaskErrorWrapperLocal(errors.New("invalid origin task"), "plugin_origin_invalid", 400)
		}
		originUpstreamModel = origin.Properties.UpstreamModelName
		if originUpstreamModel == "" {
			originUpstreamModel = origin.Properties.OriginModelName
		}
		if origin.PrivateData.Key == "" || origin.Properties.OriginModelName != info.OriginModelName {
			return nil, service.TaskErrorWrapperLocal(errors.New("origin model or credential does not match"), "plugin_origin_invalid", 400)
		}
		input["originTaskId"] = origin.GetUpstreamTaskID()
		// A remix belongs to the same provider account as its source video.
		info.ApiKey = origin.PrivateData.Key
		input["apiKey"] = info.ApiKey
		if origin.PrivateData.PluginContext != nil {
			input["originUsage"] = origin.PrivateData.PluginContext.Usage
		}
	}
	prepared, err := manager.PrepareProductionSubmit(c.Request.Context(), info.ChannelId, input)
	if err != nil {
		if errors.Is(err, management.ErrPublicProtocolRequest) {
			return nil, service.TaskErrorWrapperLocal(err, "plugin_protocol_invalid", http.StatusBadRequest)
		}
		return nil, service.TaskErrorWrapperLocal(err, "plugin_prepare_failed", http.StatusServiceUnavailable)
	}
	if model.TaskPluginKeyForChannel(info.ChannelType) != prepared.Binding.Key {
		return nil, service.TaskErrorWrapperLocal(model.ErrTaskPluginIdentity, "plugin_channel_mismatch", 400)
	}
	if route := service.TaskPublicRoute(c); route != nil && prepared.Binding != route.Targets[info.ChannelId].Binding {
		return nil, service.TaskErrorWrapperLocal(errors.New("public task binding changed"), "plugin_binding_changed", 409)
	}
	// Provider ratios, completion snapshots and persisted task properties must
	// use the same bound declaration as the submit and usage hooks.
	info.UpstreamModelName = prepared.UpstreamModel
	if info.OriginTaskID != "" && originUpstreamModel != info.UpstreamModelName {
		return nil, service.TaskErrorWrapperLocal(errors.New("origin model or credential does not match"), "plugin_origin_invalid", 400)
	}
	// Streaming/immediate image protocols require a different result lifecycle.
	if prepared.Descriptor.ResponseType == "sse" {
		return nil, service.TaskErrorWrapperLocal(errors.New("streaming tasks are not supported"), "plugin_protocol_unsupported", 400)
	}
	facts, err := prepared.Usage(c.Request.Context())
	if err != nil {
		return nil, service.TaskErrorWrapperLocal(err, "plugin_usage_invalid", 400)
	}
	if info.Action == constant.TaskActionRemix {
		if originFacts, ok := input["originUsage"].(map[string]any); ok {
			facts = originFacts
		}
	}
	prepared.Facts = facts
	if prepared.Descriptor.Action != "" {
		info.Action = prepared.Descriptor.Action
	}
	schema, _ := prepared.Handle.Meta().UsageForModel(info.UpstreamModelName)
	priceInfo := *info
	if route := service.TaskPublicRoute(c); route != nil {
		// Case variants of an advertised alias share its explicit price. The
		// original spelling is still retained for permissions and public echo.
		original := billing_setting.ResolveTaskBillingRule(prepared.Binding.Key, info.OriginModelName, info.OriginModelName)
		if original.Source == "missing" || original.Source == "explicit_task_rule_required" {
			priceInfo.OriginModelName = route.RoutingModel
		}
	}
	price, err := helper.ModelPriceHelperForPlugin(c, &priceInfo, prepared.Binding.Key, schema, facts)
	info.TieredBillingSnapshot = priceInfo.TieredBillingSnapshot
	if err != nil {
		return nil, service.TaskErrorWrapperLocal(err, "model_price_error", 400)
	}
	info.PriceData = price
	var ratios map[string]float64
	if !price.TaskFixedTotal {
		ratios, err = service.TaskPluginProviderRatios(prepared.Binding.Key, info.UpstreamModelName, facts)
		if err != nil {
			return nil, service.TaskErrorWrapperLocal(err, "plugin_billing_unsupported", 400)
		}
	}
	for key, value := range ratios {
		info.PriceData.AddOtherRatio(key, value)
	}
	fixedTotal := price.TaskFixedTotal || common.StringsContains(constant.TaskPricePatches, info.OriginModelName)
	completionBilling, err := service.NewTaskPluginBillingSnapshot(prepared.Binding.Key, info.UpstreamModelName, price.Quota, fixedTotal, price.UsePrice)
	if err != nil {
		return nil, service.TaskErrorWrapperLocal(err, "plugin_price_snapshot_invalid", 400)
	}
	if !fixedTotal {
		quota, err := service.TaskPluginQuota(price.Quota, ratios)
		if err != nil {
			return nil, service.TaskErrorWrapperLocal(err, "plugin_quota_invalid", 400)
		}
		info.PriceData.Quota = quota
	}
	return submitDurablePluginTask(c, info, platform, manager, prepared, executor, files, body, completionBilling)
}
