package controller

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/channel_limiter"
	"github.com/QuantumNous/new-api/service/multi_key_scheduler"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/samber/lo"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func relayHandler(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	var err *types.NewAPIError
	switch info.RelayMode {
	case relayconstant.RelayModeImagesGenerations, relayconstant.RelayModeImagesEdits:
		err = relay.ImageHelper(c, info)
	case relayconstant.RelayModeAudioSpeech:
		fallthrough
	case relayconstant.RelayModeAudioTranslation:
		fallthrough
	case relayconstant.RelayModeAudioTranscription:
		err = relay.AudioHelper(c, info)
	case relayconstant.RelayModeRerank:
		err = relay.RerankHelper(c, info)
	case relayconstant.RelayModeEmbeddings:
		err = relay.EmbeddingHelper(c, info)
	case relayconstant.RelayModeResponses, relayconstant.RelayModeResponsesCompact:
		err = relay.ResponsesHelper(c, info)
	default:
		err = relay.TextHelper(c, info)
	}
	return err
}

func geminiRelayHandler(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	var err *types.NewAPIError
	if strings.Contains(c.Request.URL.Path, "embed") {
		err = relay.GeminiEmbeddingHandler(c, info)
	} else {
		err = relay.GeminiHelper(c, info)
	}
	return err
}

func Relay(c *gin.Context, relayFormat types.RelayFormat) {

	requestId := c.GetString(common.RequestIdKey)
	//group := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	//originalModel := common.GetContextKeyString(c, constant.ContextKeyOriginalModel)

	var (
		newAPIError *types.NewAPIError
		ws          *websocket.Conn
	)

	if relayFormat == types.RelayFormatOpenAIRealtime {
		var err error
		ws, err = upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			helper.WssError(c, ws, types.NewError(err, types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry()).ToOpenAIError())
			return
		}
		defer ws.Close()
	}

	defer func() {
		if newAPIError != nil {
			logger.LogError(c, fmt.Sprintf("relay error: %s", common.LocalLogPreview(newAPIError.Error())))
			newAPIError.SetMessage(common.MessageWithRequestId(newAPIError.Error(), requestId))
			switch relayFormat {
			case types.RelayFormatOpenAIRealtime:
				helper.WssError(c, ws, newAPIError.ToOpenAIError())
			case types.RelayFormatClaude:
				c.JSON(newAPIError.StatusCode, gin.H{
					"type":  "error",
					"error": newAPIError.ToClaudeError(),
				})
			default:
				c.JSON(newAPIError.StatusCode, gin.H{
					"error": newAPIError.ToOpenAIError(),
				})
			}
		}
	}()

	request, err := helper.GetAndValidateRequest(c, relayFormat)
	if err != nil {
		// Map "request body too large" to 413 so clients can handle it correctly
		if common.IsRequestBodyTooLargeError(err) || errors.Is(err, common.ErrRequestBodyTooLarge) {
			newAPIError = types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusRequestEntityTooLarge, types.ErrOptionWithSkipRetry())
		} else {
			newAPIError = types.NewError(err, types.ErrorCodeInvalidRequest)
		}
		return
	}

	relayInfo, err := relaycommon.GenRelayInfo(c, relayFormat, request, ws)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeGenRelayInfoFailed)
		return
	}
	c.Set("risk_audit", relayInfo.RiskAudit)
	if riskErr := service.RiskControlBeforeRelay(c, relayInfo); riskErr != nil {
		newAPIError = riskErr
		c.Set("risk_audit", relayInfo.RiskAudit)
		service.RecordRiskBlockedAccess(c, relayInfo, service.GetBlockingDecisionFromAudit(relayInfo.RiskAudit))
		return
	}
	// meta is hoisted above the defer so the moderation hook can reference it.
	var meta *types.TokenCountMeta
	defer func() {
		c.Set("risk_audit", relayInfo.RiskAudit)
		service.RiskControlAfterRelay(c, relayInfo, newAPIError)
		// Async OpenAI moderation scoring; never blocks the relay path.
		// Failed-upstream requests (no client output) are filtered inside
		// EnqueueModerationFromRelay via the relay error.
		service.EnqueueModerationFromRelay(c, relayInfo, meta, newAPIError)
	}()

	needSensitiveCheck := setting.ShouldCheckPromptSensitive()
	needCountToken := constant.CountToken
	// Avoid building huge CombineText (strings.Join) when token counting and sensitive check are both disabled.
	if needSensitiveCheck || needCountToken {
		meta = request.GetTokenCountMeta()
	} else {
		meta = fastTokenCountMetaForPricing(request)
	}

	if needSensitiveCheck && meta != nil {
		contains, words := service.CheckSensitiveText(meta.CombineText)
		if contains {
			logger.LogWarn(c, fmt.Sprintf("user sensitive words detected: %s", strings.Join(words, ", ")))
			newAPIError = types.NewError(err, types.ErrorCodeSensitiveWordsDetected)
			return
		}
	}

	tokens, err := service.EstimateRequestToken(c, meta, relayInfo)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeCountTokenFailed)
		return
	}

	relayInfo.SetEstimatePromptTokens(tokens)

	priceData, err := helper.ModelPriceHelper(c, relayInfo, tokens, meta)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeModelPriceError, types.ErrOptionWithStatusCode(http.StatusBadRequest))
		return
	}

	// common.SetContextKey(c, constant.ContextKeyTokenCountMeta, meta)

	if priceData.FreeModel {
		logger.LogInfo(c, fmt.Sprintf("模型 %s 免费，跳过预扣费", relayInfo.OriginModelName))
	} else {
		newAPIError = service.PreConsumeBilling(c, priceData.QuotaToPreConsume, relayInfo)
		if newAPIError != nil {
			return
		}
	}

	defer func() {
		// Only return quota if downstream failed and quota was actually pre-consumed
		if newAPIError != nil {
			newAPIError = service.NormalizeViolationFeeError(newAPIError)
			if relayInfo.Billing != nil {
				relayInfo.Billing.Refund(c)
			}
			service.ChargeViolationFeeIfNeeded(c, relayInfo, newAPIError)
		}
	}()

	retryParam := &service.RetryParam{
		Ctx:        c,
		TokenGroup: relayInfo.TokenGroup,
		ModelName:  relayInfo.OriginModelName,
		Retry:      common.GetPointer(0),
	}
	relayInfo.RetryIndex = 0
	relayInfo.LastError = nil

	// Channel rate-limit token from current iteration. We release it at the top of
	// the next iteration (before acquiring a new one) and via defer on function exit.
	// Token.Release uses sync.Once so double-release is safe.
	var rateLimitToken *channel_limiter.Token
	defer func() {
		if rateLimitToken != nil {
			rateLimitToken.Release()
		}
	}()

	for ; retryParam.GetRetry() <= common.RetryTimes; retryParam.IncreaseRetry() {
		relayInfo.RetryIndex = retryParam.GetRetry()
		if rateLimitToken != nil {
			rateLimitToken.Release()
			rateLimitToken = nil
		}
		channel, channelErr := getChannel(c, relayInfo, retryParam)
		if channelErr != nil {
			logger.LogError(c, channelErr.Error())
			newAPIError = channelErr
			break
		}
		if setupErr := middleware.SetupContextForSelectedChannel(c, channel, relayInfo.OriginModelName); setupErr != nil {
			relayInfo.LastError = setupErr
			newAPIError = setupErr
			if setupErr.IsSkipRetry() {
				break
			}
			continue
		}
		reservation := captureSchedulerReservation(c, channel, relayInfo.OriginModelName)
		// Safety net for panics and future early returns. Normal branches release
		// immediately; sync.Once makes the deferred call harmless.
		defer reservation.release()

		addUsedChannel(c, channel.Id)

		// Channel-level rate limit (RPM / concurrency).
		if rateLimitCfg := channel.GetSetting().RateLimit; channel_limiter.IsActive(rateLimitCfg) {
			token, decision := channel_limiter.Acquire(c.Request.Context(), channel.Id, rateLimitCfg)
			if decision.Allowed {
				rateLimitToken = token
			} else {
				// Hard affinity (SkipRetryOnFailure=true) demands that requests
				// stay on the original channel even at the cost of failing.
				// Honor that by overriding skip/queue with reject so we 429
				// instead of silently routing to a different channel.
				effectiveOnLimit := rateLimitCfg.OnLimit
				if service.ShouldSkipRetryAfterChannelAffinityFailure(c) {
					effectiveOnLimit = channel_limiter.OnLimitReject
				}

				logger.LogWarn(c, fmt.Sprintf("channel %d (%s) rate-limited: reason=%s, action=%s", channel.Id, channel.Name, decision.Reason, effectiveOnLimit))
				rlErr := types.NewErrorWithStatusCode(
					fmt.Errorf("渠道 %s 已达限流 (%s)", channel.Name, decision.Reason),
					types.ErrorCodeChannelRateLimited,
					http.StatusTooManyRequests,
				)
				relayInfo.LastError = rlErr
				if effectiveOnLimit == channel_limiter.OnLimitReject {
					newAPIError = types.NewErrorWithStatusCode(
						fmt.Errorf("渠道 %s 已达限流 (%s)", channel.Name, decision.Reason),
						types.ErrorCodeChannelRateLimited,
						http.StatusTooManyRequests,
						types.ErrOptionWithSkipRetry(),
					)
					reservation.release()
					break
				}
				// skip / queue-timeout / queue-full: signal the affinity layer
				// that this detour is rate-limit driven so it won't displace
				// the original cache-friendly channel via SwitchOnSuccess.
				common.SetContextKey(c, constant.ContextKeyRateLimitSkipped, true)
				reservation.release()
				continue
			}
		}

		bodyStorage, bodyErr := common.GetBodyStorage(c)
		if bodyErr != nil {
			// Ensure consistent 413 for oversized bodies even when error occurs later (e.g., retry path)
			if common.IsRequestBodyTooLargeError(bodyErr) || errors.Is(bodyErr, common.ErrRequestBodyTooLarge) {
				newAPIError = types.NewErrorWithStatusCode(bodyErr, types.ErrorCodeReadRequestBodyFailed, http.StatusRequestEntityTooLarge, types.ErrOptionWithSkipRetry())
			} else {
				newAPIError = types.NewErrorWithStatusCode(bodyErr, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
			}
			reservation.release()
			break
		}
		c.Request.Body = io.NopCloser(bodyStorage)

		switch relayFormat {
		case types.RelayFormatOpenAIRealtime:
			newAPIError = relay.WssHelper(c, relayInfo)
		case types.RelayFormatClaude:
			newAPIError = relay.ClaudeHelper(c, relayInfo)
		case types.RelayFormatGemini:
			newAPIError = geminiRelayHandler(c, relayInfo)
		default:
			newAPIError = relayHandler(c, relayInfo)
		}

		if newAPIError == nil {
			relayInfo.LastError = nil
			reservation.success(relayInfo.GetEstimatePromptTokens())
			return
		}

		matchStatusCode, matchErrorCode, matchMessage := getErrorFilterMatchInput(newAPIError)
		newAPIError = service.NormalizeViolationFeeError(newAPIError)
		applyChannelErrorFilter(newAPIError, channel.GetSetting().ErrorFilterRules, matchStatusCode, matchErrorCode, matchMessage)
		relayInfo.LastError = newAPIError
		// 给 multi-key 调度器报上游错误：撞 RPM/429 时该 (key,model) 写 cool；
		// 其它错误只释放并发槽位。判断细节见 multi_key_scheduler.isRateLimitError。
		reservation.fail(matchStatusCode, matchMessage)

		processChannelError(c, *types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey, common.GetContextKeyString(c, constant.ContextKeyChannelKey), channel.GetAutoBan()), newAPIError)

		if !shouldRetry(c, newAPIError, common.RetryTimes-retryParam.GetRetry()) {
			break
		}
	}

	useChannel := c.GetStringSlice("use_channel")
	if len(useChannel) > 1 {
		retryLogStr := fmt.Sprintf("重试：%s", strings.Trim(strings.Join(strings.Fields(fmt.Sprint(useChannel)), "->"), "[]"))
		logger.LogInfo(c, retryLogStr)
	}
	if newAPIError != nil {
		gopool.Go(func() {
			perfmetrics.RecordRelaySample(relayInfo, false, 0)
		})
	}
}

var upgrader = websocket.Upgrader{
	Subprotocols: []string{"realtime"}, // WS 握手支持的协议，如果有使用 Sec-WebSocket-Protocol，则必须在此声明对应的 Protocol TODO add other protocol
	CheckOrigin: func(r *http.Request) bool {
		return true // 允许跨域
	},
}

func addUsedChannel(c *gin.Context, channelId int) {
	useChannel := c.GetStringSlice("use_channel")
	useChannel = append(useChannel, fmt.Sprintf("%d", channelId))
	c.Set("use_channel", useChannel)
}

func fastTokenCountMetaForPricing(request dto.Request) *types.TokenCountMeta {
	if request == nil {
		return &types.TokenCountMeta{}
	}
	meta := &types.TokenCountMeta{
		TokenType: types.TokenTypeTokenizer,
	}
	switch r := request.(type) {
	case *dto.GeneralOpenAIRequest:
		maxCompletionTokens := lo.FromPtrOr(r.MaxCompletionTokens, uint(0))
		maxTokens := lo.FromPtrOr(r.MaxTokens, uint(0))
		if maxCompletionTokens > maxTokens {
			meta.MaxTokens = int(maxCompletionTokens)
		} else {
			meta.MaxTokens = int(maxTokens)
		}
	case *dto.OpenAIResponsesRequest:
		meta.MaxTokens = int(lo.FromPtrOr(r.MaxOutputTokens, uint(0)))
	case *dto.ClaudeRequest:
		meta.MaxTokens = int(lo.FromPtr(r.MaxTokens))
	case *dto.ImageRequest:
		// Pricing for image requests depends on ImagePriceRatio; safe to compute even when CountToken is disabled.
		return r.GetTokenCountMeta()
	default:
		// Best-effort: leave CombineText empty to avoid large allocations.
	}
	return meta
}

func getChannel(c *gin.Context, info *relaycommon.RelayInfo, retryParam *service.RetryParam) (*model.Channel, *types.NewAPIError) {
	if retryParam.GetRetry() == 0 {
		if channel, ok := common.GetContextKeyType[*model.Channel](c, constant.ContextKeySelectedChannel); ok && channel != nil {
			return channel, nil
		}
		return nil, types.NewError(errors.New("selected channel is missing"), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}
	channel, selectGroup, err := service.CacheGetRandomSatisfiedChannel(retryParam)

	info.PriceData.GroupRatioInfo = helper.HandleGroupRatio(c, info)

	if err != nil {
		return nil, types.NewError(fmt.Errorf("获取分组 %s 下模型 %s 的可用渠道失败（retry）: %s", selectGroup, info.OriginModelName, err.Error()), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}
	if channel == nil {
		return nil, types.NewError(fmt.Errorf("分组 %s 下模型 %s 的可用渠道不存在（retry）", selectGroup, info.OriginModelName), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}

	return channel, nil
}

// schedulerReservation snapshots one successful scheduler Acquire. It owns
// exactly one release; context/channel mutations during retries cannot make it
// release another key's slot.
type schedulerReservation struct {
	once      sync.Once
	ctx       context.Context
	channelID int
	keyFp     string
	model     string
	cfg       *dto.SchedulerConfig
}

func captureSchedulerReservation(c *gin.Context, ch *model.Channel, modelName string) *schedulerReservation {
	if ch == nil || !ch.ChannelInfo.IsMultiKey || !multi_key_scheduler.IsEnabledFor(ch.ChannelInfo.SchedulerConfig) {
		return nil
	}
	keyFp := schedulerKeyFp(c)
	if keyFp == "" {
		return nil
	}
	return &schedulerReservation{
		ctx:       c.Request.Context(),
		channelID: ch.Id,
		keyFp:     keyFp,
		model:     modelName,
		cfg:       ch.ChannelInfo.SchedulerConfig,
	}
}

func (r *schedulerReservation) release() {
	if r == nil {
		return
	}
	r.once.Do(func() {
		multi_key_scheduler.ReportError(r.ctx, r.channelID, r.keyFp, r.model, 0, "", r.cfg)
	})
}

func (r *schedulerReservation) success(tokens int) {
	if r == nil {
		return
	}
	r.once.Do(func() {
		multi_key_scheduler.ReportSuccess(r.ctx, r.channelID, r.keyFp, r.model, tokens, r.cfg)
	})
}

func (r *schedulerReservation) fail(statusCode int, message string) {
	if r == nil {
		return
	}
	r.once.Do(func() {
		multi_key_scheduler.ReportError(r.ctx, r.channelID, r.keyFp, r.model, statusCode, message, r.cfg)
	})
}

// schedulerKeyFp 从 ctx 反推当前请求选中的 key 指纹。
// distributor.go 设置 ContextKeyChannelKey 为明文 key，本函数 hash 一次返回 16 字节指纹，
// 与 scheduler.Acquire 当初写 Redis 时用的指纹一致。
func schedulerKeyFp(c *gin.Context) string {
	key := common.GetContextKeyString(c, constant.ContextKeyChannelKey)
	if key == "" {
		return ""
	}
	return multi_key_scheduler.KeyFingerprint(key)
}

func getErrorFilterMatchInput(apiErr *types.NewAPIError) (statusCode int, errorCode string, message string) {
	if apiErr == nil {
		return 0, "", ""
	}
	oaiErr := apiErr.ToOpenAIError()
	if oaiErr.Code == nil {
		return apiErr.StatusCode, "", oaiErr.Message
	}
	return apiErr.StatusCode, fmt.Sprintf("%v", oaiErr.Code), oaiErr.Message
}

func applyChannelErrorFilter(apiErr *types.NewAPIError, rules []dto.ErrorFilterRule, statusCode int, errorCode string, message string) {
	if apiErr == nil || len(rules) == 0 {
		return
	}

	matched, rule := service.MatchErrorFilter(rules, statusCode, errorCode, message)
	if !matched || rule == nil {
		return
	}

	switch strings.ToLower(strings.TrimSpace(rule.Action)) {
	case "rewrite":
		if rule.RewriteMessage != "" {
			apiErr.SetMessage(rule.RewriteMessage)
		}
		apiErr.SetSkipRetry(true)
	case "replace":
		if rule.ReplaceStatusCode > 0 {
			apiErr.SetStatusCode(rule.ReplaceStatusCode)
		}
		if rule.ReplaceMessage != "" {
			apiErr.SetMessage(rule.ReplaceMessage)
		}
		apiErr.SetSkipRetry(true)
	case "retry":
		apiErr.SetForceRetry(true)
	}
}

func shouldRetry(c *gin.Context, openaiErr *types.NewAPIError, retryTimes int) bool {
	if openaiErr == nil {
		return false
	}
	if openaiErr.IsForceRetry() {
		return true
	}
	if service.ShouldSkipRetryAfterChannelAffinityFailure(c) {
		// Hard affinity normally refuses cross-channel retry to keep the
		// per-conversation prompt cache warm on one channel. But a dead channel
		// serves no cache anyway — when failover-on-channel-down is enabled and
		// the pinned channel returned a channel-down error, fall through to the
		// normal retry rules so the request escapes to a sibling channel instead
		// of failing outright. Non-channel errors (e.g. 400) still skip retry.
		if !(service.ChannelAffinityFailoverOnChannelDownEnabled() && service.IsChannelDownError(openaiErr)) {
			return false
		}
	}
	if types.IsChannelError(openaiErr) {
		return true
	}
	if types.IsSkipRetryError(openaiErr) {
		return false
	}
	if retryTimes <= 0 {
		return false
	}
	if _, ok := c.Get("specific_channel_id"); ok {
		return false
	}
	code := openaiErr.StatusCode
	if code >= 200 && code < 300 {
		return false
	}
	if code < 100 || code > 599 {
		return true
	}
	if operation_setting.IsAlwaysSkipRetryCode(openaiErr.GetErrorCode()) {
		return false
	}
	return operation_setting.ShouldRetryByStatusCode(code)
}

func processChannelError(c *gin.Context, channelError types.ChannelError, err *types.NewAPIError) {
	logger.LogError(c, fmt.Sprintf("channel error (channel #%d, status code: %d): %s", channelError.ChannelId, err.StatusCode, common.LocalLogPreview(err.Error())))
	// Self-heal channel affinity: if the affinity-pinned channel just failed with
	// a channel-down error, evict its stale pin so the next request for this
	// conversation re-selects a healthy channel instead of re-hitting the corpse
	// until the affinity TTL expires. No-op unless failover-on-channel-down is on.
	service.EvictChannelAffinityOnChannelDown(c, err)
	// 不要使用context获取渠道信息，异步处理时可能会出现渠道信息不一致的情况
	// do not use context to get channel info, there may be inconsistent channel info when processing asynchronously
	if !err.IsSkipRetry() && service.ShouldDisableChannel(err) && channelError.AutoBan {
		gopool.Go(func() {
			service.DisableChannel(channelError, err.ErrorWithStatusCode())
		})
	}

	if constant.ErrorLogEnabled && types.IsRecordErrorLog(err) {
		// 保存错误日志到mysql中
		userId := c.GetInt("id")
		tokenName := c.GetString("token_name")
		modelName := c.GetString("original_model")
		tokenId := c.GetInt("token_id")
		userGroup := c.GetString("group")
		channelId := c.GetInt("channel_id")
		other := make(map[string]interface{})
		if c.Request != nil && c.Request.URL != nil {
			other["request_path"] = c.Request.URL.Path
		}
		other["error_type"] = err.GetErrorType()
		other["error_code"] = err.GetErrorCode()
		other["status_code"] = err.StatusCode
		other["channel_id"] = channelId
		other["channel_name"] = c.GetString("channel_name")
		other["channel_type"] = c.GetInt("channel_type")
		adminInfo := make(map[string]interface{})
		adminInfo["use_channel"] = c.GetStringSlice("use_channel")
		isMultiKey := common.GetContextKeyBool(c, constant.ContextKeyChannelIsMultiKey)
		if isMultiKey {
			adminInfo["is_multi_key"] = true
			adminInfo["multi_key_index"] = common.GetContextKeyInt(c, constant.ContextKeyChannelMultiKeyIndex)
		}
		service.AppendChannelAffinityAdminInfo(c, adminInfo)
		other["admin_info"] = adminInfo
		if rawAudit, ok := c.Get("risk_audit"); ok {
			if audit, ok := rawAudit.(*types.RiskAudit); ok {
				service.AppendRiskAuditToOther(other, audit)
			}
		}
		startTime := common.GetContextKeyTime(c, constant.ContextKeyRequestStartTime)
		if startTime.IsZero() {
			startTime = time.Now()
		}
		useTimeSeconds := int(time.Since(startTime).Seconds())
		model.RecordErrorLog(c, userId, channelId, modelName, tokenName, err.MaskSensitiveErrorWithStatusCode(), tokenId, useTimeSeconds, common.GetContextKeyBool(c, constant.ContextKeyIsStream), userGroup, other)
	}

}

func RelayMidjourney(c *gin.Context) {
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatMjProxy, nil, nil)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"description": fmt.Sprintf("failed to generate relay info: %s", err.Error()),
			"type":        "upstream_error",
			"code":        4,
		})
		return
	}

	var mjErr *dto.MidjourneyResponse
	switch relayInfo.RelayMode {
	case relayconstant.RelayModeMidjourneyNotify:
		mjErr = relay.RelayMidjourneyNotify(c)
	case relayconstant.RelayModeMidjourneyTaskFetch, relayconstant.RelayModeMidjourneyTaskFetchByCondition:
		mjErr = relay.RelayMidjourneyTask(c, relayInfo.RelayMode)
	case relayconstant.RelayModeMidjourneyTaskImageSeed:
		mjErr = relay.RelayMidjourneyTaskImageSeed(c)
	case relayconstant.RelayModeSwapFace:
		mjErr = relay.RelaySwapFace(c, relayInfo)
	default:
		mjErr = relay.RelayMidjourneySubmit(c, relayInfo)
	}
	//err = relayMidjourneySubmit(c, relayMode)
	log.Println(mjErr)
	if mjErr != nil {
		statusCode := http.StatusBadRequest
		if mjErr.Code == 30 {
			mjErr.Result = "当前分组负载已饱和，请稍后再试，或升级账户以提升服务质量。"
			statusCode = http.StatusTooManyRequests
		}
		c.JSON(statusCode, gin.H{
			"description": fmt.Sprintf("%s %s", mjErr.Description, mjErr.Result),
			"type":        "upstream_error",
			"code":        mjErr.Code,
		})
		channelId := c.GetInt("channel_id")
		logger.LogError(c, fmt.Sprintf("relay error (channel #%d, status code %d): %s", channelId, statusCode, fmt.Sprintf("%s %s", mjErr.Description, mjErr.Result)))
	}
}

func RelayNotImplemented(c *gin.Context) {
	err := types.OpenAIError{
		Message: "API not implemented",
		Type:    "new_api_error",
		Param:   "",
		Code:    "api_not_implemented",
	}
	c.JSON(http.StatusNotImplemented, gin.H{
		"error": err,
	})
}

func RelayNotFound(c *gin.Context) {
	err := types.OpenAIError{
		Message: fmt.Sprintf("Invalid URL (%s %s)", c.Request.Method, c.Request.URL.Path),
		Type:    "invalid_request_error",
		Param:   "",
		Code:    "",
	}
	c.JSON(http.StatusNotFound, gin.H{
		"error": err,
	})
}

func RelayTaskFetch(c *gin.Context) {
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatTask, nil, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, &dto.TaskError{
			Code:       "gen_relay_info_failed",
			Message:    err.Error(),
			StatusCode: http.StatusInternalServerError,
		})
		return
	}
	if taskErr := relay.RelayTaskFetch(c, relayInfo.RelayMode); taskErr != nil {
		respondTaskError(c, taskErr)
	}
}

func RelayTask(c *gin.Context) {
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatTask, nil, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, &dto.TaskError{
			Code:       "gen_relay_info_failed",
			Message:    err.Error(),
			StatusCode: http.StatusInternalServerError,
		})
		return
	}

	if taskErr := relay.ResolveOriginTask(c, relayInfo); taskErr != nil {
		respondTaskError(c, taskErr)
		return
	}

	var result *relay.TaskSubmitResult
	var taskErr *dto.TaskError
	defer func() {
		if taskErr != nil && relayInfo.Billing != nil {
			relayInfo.Billing.Refund(c)
		}
	}()

	retryParam := &service.RetryParam{
		Ctx:        c,
		TokenGroup: relayInfo.TokenGroup,
		ModelName:  relayInfo.OriginModelName,
		Retry:      common.GetPointer(0),
	}

	for ; retryParam.GetRetry() <= common.RetryTimes; retryParam.IncreaseRetry() {
		var channel *model.Channel

		if lockedCh, ok := relayInfo.LockedChannel.(*model.Channel); ok && lockedCh != nil {
			channel = lockedCh
		} else {
			var channelErr *types.NewAPIError
			channel, channelErr = getChannel(c, relayInfo, retryParam)
			if channelErr != nil {
				logger.LogError(c, channelErr.Error())
				taskErr = service.TaskErrorWrapperLocal(channelErr.Err, "get_channel_failed", http.StatusInternalServerError)
				break
			}
		}
		if setupErr := middleware.SetupContextForSelectedChannel(c, channel, relayInfo.OriginModelName); setupErr != nil {
			taskErr = service.TaskErrorFromAPIError(setupErr)
			if setupErr.IsSkipRetry() {
				break
			}
			continue
		}
		reservation := captureSchedulerReservation(c, channel, relayInfo.OriginModelName)
		defer reservation.release()

		addUsedChannel(c, channel.Id)
		bodyStorage, bodyErr := common.GetBodyStorage(c)
		if bodyErr != nil {
			if common.IsRequestBodyTooLargeError(bodyErr) || errors.Is(bodyErr, common.ErrRequestBodyTooLarge) {
				taskErr = service.TaskErrorWrapperLocal(bodyErr, "read_request_body_failed", http.StatusRequestEntityTooLarge)
			} else {
				taskErr = service.TaskErrorWrapperLocal(bodyErr, "read_request_body_failed", http.StatusBadRequest)
			}
			reservation.release()
			break
		}
		c.Request.Body = io.NopCloser(bodyStorage)

		result, taskErr = relay.RelayTaskSubmit(c, relayInfo)
		if taskErr == nil {
			reservation.success(0)
			break
		}

		if !taskErr.LocalError {
			reservation.fail(taskErr.StatusCode, taskErr.Message)
			processChannelError(c,
				*types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey,
					common.GetContextKeyString(c, constant.ContextKeyChannelKey), channel.GetAutoBan()),
				types.NewOpenAIError(taskErr.Error, types.ErrorCodeBadResponseStatusCode, taskErr.StatusCode))
		} else {
			reservation.release()
		}

		if !shouldRetryTaskRelay(c, channel.Id, taskErr, common.RetryTimes-retryParam.GetRetry()) {
			break
		}
	}

	useChannel := c.GetStringSlice("use_channel")
	if len(useChannel) > 1 {
		retryLogStr := fmt.Sprintf("重试：%s", strings.Trim(strings.Join(strings.Fields(fmt.Sprint(useChannel)), "->"), "[]"))
		logger.LogInfo(c, retryLogStr)
	}

	// ── 成功：结算 + 日志 + 插入任务 ──
	if taskErr == nil {
		if settleErr := service.SettleBilling(c, relayInfo, result.Quota); settleErr != nil {
			common.SysError("settle task billing error: " + settleErr.Error())
			taskErr = service.TaskErrorWrapperLocal(settleErr, "settle_task_billing_failed", http.StatusInternalServerError)
		} else {
			task := model.InitTask(result.Platform, relayInfo)
			task.PrivateData.UpstreamTaskID = result.UpstreamTaskID
			task.PrivateData.TokenId = relayInfo.TokenId
			task.PrivateData.BillingContext = &model.TaskBillingContext{
				ModelPrice:      relayInfo.PriceData.ModelPrice,
				GroupRatio:      relayInfo.PriceData.GroupRatioInfo.GroupRatio,
				ModelRatio:      relayInfo.PriceData.ModelRatio,
				OtherRatios:     relayInfo.PriceData.OtherRatios,
				OriginModelName: relayInfo.OriginModelName,
				PerCallBilling:  common.StringsContains(constant.TaskPricePatches, relayInfo.OriginModelName) || relayInfo.PriceData.UsePrice,
			}
			task.Quota = result.Quota
			task.Data = result.TaskData
			task.Action = relayInfo.Action
			if insertErr := task.Insert(); insertErr != nil {
				common.SysError("insert task error: " + insertErr.Error())
				if session, ok := relayInfo.Billing.(*service.BillingSession); ok {
					if compensateErr := session.Compensate(c); compensateErr != nil {
						common.SysError("compensate task billing error: " + compensateErr.Error())
					}
				}
				taskErr = service.TaskErrorWrapperLocal(insertErr, "insert_task_failed", http.StatusInternalServerError)
			} else {
				service.LogTaskConsumption(c, relayInfo)
			}
		}
	}

	if taskErr != nil {
		respondTaskError(c, taskErr)
	}
}

// respondTaskError 统一输出 Task 错误响应（含 429 限流提示改写）
func respondTaskError(c *gin.Context, taskErr *dto.TaskError) {
	if taskErr.StatusCode == http.StatusTooManyRequests {
		taskErr.Message = "当前分组上游负载已饱和，请稍后再试"
	}
	c.JSON(taskErr.StatusCode, taskErr)
}

func shouldRetryTaskRelay(c *gin.Context, channelId int, taskErr *dto.TaskError, retryTimes int) bool {
	if taskErr == nil {
		return false
	}
	if service.ShouldSkipRetryAfterChannelAffinityFailure(c) {
		return false
	}
	if retryTimes <= 0 {
		return false
	}
	if _, ok := c.Get("specific_channel_id"); ok {
		return false
	}
	if taskErr.StatusCode == http.StatusTooManyRequests {
		return true
	}
	if taskErr.StatusCode == 307 {
		return true
	}
	if taskErr.StatusCode/100 == 5 {
		// 超时不重试
		if operation_setting.IsAlwaysSkipRetryStatusCode(taskErr.StatusCode) {
			return false
		}
		return true
	}
	if taskErr.StatusCode == http.StatusBadRequest {
		return false
	}
	if taskErr.StatusCode == 408 {
		// azure处理超时不重试
		return false
	}
	if taskErr.LocalError {
		return false
	}
	if taskErr.StatusCode/100 == 2 {
		return false
	}
	return true
}
