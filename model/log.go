package model

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/pkg/requestip"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"

	"github.com/bytedance/gopkg/util/gopool"
	"gorm.io/gorm"
)

func applyExplicitLogTextFilter(tx *gorm.DB, column string, value string) (*gorm.DB, error) {
	if value == "" {
		return tx, nil
	}
	if strings.Contains(value, "%") {
		pattern, err := sanitizeLikePattern(value)
		if err != nil {
			return nil, err
		}
		return tx.Where(column+" LIKE ? ESCAPE '!'", pattern), nil
	}
	return tx.Where(column+" = ?", value), nil
}

type Log struct {
	Id               int    `json:"id" gorm:"index:idx_created_at_id,priority:1;index:idx_user_id_id,priority:2"`
	UserId           int    `json:"user_id" gorm:"index;index:idx_user_id_id,priority:1"`
	CreatedAt        int64  `json:"created_at" gorm:"bigint;index:idx_created_at_id,priority:2;index:idx_created_at_type"`
	Type             int    `json:"type" gorm:"index:idx_created_at_type"`
	Content          string `json:"content"`
	Username         string `json:"username" gorm:"index;index:index_username_model_name,priority:2;default:''"`
	TokenName        string `json:"token_name" gorm:"index;default:''"`
	ModelName        string `json:"model_name" gorm:"index;index:index_username_model_name,priority:1;default:''"`
	Quota            int    `json:"quota" gorm:"default:0"`
	PromptTokens     int    `json:"prompt_tokens" gorm:"default:0"`
	CompletionTokens int    `json:"completion_tokens" gorm:"default:0"`
	UseTime          int    `json:"use_time" gorm:"default:0"`
	IsStream         bool   `json:"is_stream"`
	ChannelId        int    `json:"channel" gorm:"index"`
	ChannelName      string `json:"channel_name" gorm:"->"`
	TokenId          int    `json:"token_id" gorm:"default:0;index"`
	Group            string `json:"group" gorm:"index"`
	Ip               string `json:"ip" gorm:"index;default:''"`
	RequestId         string `json:"request_id,omitempty" gorm:"type:varchar(64);index:idx_logs_request_id;default:''"`
	UpstreamRequestId string `json:"upstream_request_id,omitempty" gorm:"type:varchar(128);index:idx_logs_upstream_request_id;default:''"`
	Other             string `json:"other"`
}

// don't use iota, avoid change log type value
const (
	LogTypeUnknown = 0
	LogTypeTopup   = 1
	LogTypeConsume = 2
	LogTypeManage  = 3
	LogTypeSystem  = 4
	LogTypeError   = 5
	LogTypeRefund  = 6
)

// eflow: logQueryTimeout 限定单次 logs 表读查询的最长执行时间。
// 4C8G 服务器 + ~200 万行 logs 表，叠上 username/model_name LIKE '%xx%' 一不留神
// 就 7000s+ 慢查询打爆 MySQL 线程池（同行真出过事故）。
// MySQL 驱动（go-sql-driver）会把 ctx 取消翻译成 KILL QUERY，做最后一道兜底。
const logQueryTimeout = 8 * time.Second

// logQueryCtx 返回一个带 logQueryTimeout 的 ctx。调用方必须 defer cancel()。
func logQueryCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), logQueryTimeout)
}

// eflow: estimatedLogTotal 取 logs 表的估算行数（瞬时返回，不做全表扫描），
// 用于「无过滤的全部日志」总数，以及真实 COUNT 超时/失败时的降级值。
// 它替代了会撞 logQueryTimeout 的全表 COUNT(*) —— 200 万行冷缓存下正是那个
// COUNT 被 ctx 取消（KILL QUERY）后让整页报错、前端弹「context 异常」。
func estimatedLogTotal() int64 {
	var n int64
	switch {
	case common.UsingMySQL:
		// information_schema 统计值（InnoDB 估算，误差几个百分点但瞬时返回，不锁表不全扫）
		_ = LOG_DB.Raw("SELECT TABLE_ROWS FROM information_schema.TABLES WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?", "logs").Scan(&n).Error
	case common.UsingPostgreSQL:
		_ = LOG_DB.Raw("SELECT reltuples::bigint FROM pg_class WHERE relname = ?", "logs").Scan(&n).Error
	}
	if n <= 0 {
		// SQLite / 估算拿不到 → 退回真实 count（小库无压力），仍受 ctx 兜底
		ctx, cancel := logQueryCtx()
		defer cancel()
		_ = LOG_DB.WithContext(ctx).Model(&Log{}).Count(&n).Error
	}
	return n
}

func formatUserLogs(logs []*Log, startIdx int) {
	for i := range logs {
		logs[i].ChannelName = ""
		var otherMap map[string]interface{}
		otherMap, _ = common.StrToMap(logs[i].Other)
		if otherMap != nil {
			// Remove admin-only debug fields.
			delete(otherMap, "admin_info")
			// delete(otherMap, "reject_reason")
			// stream_status 保留给日志所有者排查流式中断。
		}
		logs[i].Other = common.MapToJsonStr(otherMap)
		logs[i].Id = startIdx + i + 1
	}
}

func GetLogByTokenId(tokenId int) (logs []*Log, err error) {
	err = LOG_DB.Model(&Log{}).Where("token_id = ?", tokenId).Order("id desc").Limit(common.MaxRecentItems).Find(&logs).Error
	formatUserLogs(logs, 0)
	return logs, err
}

func RecordLog(userId int, logType int, content string) {
	if logType == LogTypeConsume && !common.LogConsumeEnabled {
		return
	}
	username, _ := GetUsernameById(userId, false)
	log := &Log{
		UserId:    userId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      logType,
		Content:   content,
	}
	err := LOG_DB.Create(log).Error
	if err != nil {
		common.SysLog("failed to record log: " + err.Error())
	}
}

// RecordLogWithAdminInfo 记录操作日志，并将管理员相关信息存入 Other.admin_info，
func RecordLogWithAdminInfo(userId int, logType int, content string, adminInfo map[string]interface{}) {
	if logType == LogTypeConsume && !common.LogConsumeEnabled {
		return
	}
	username, _ := GetUsernameById(userId, false)
	log := &Log{
		UserId:    userId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      logType,
		Content:   content,
	}
	if len(adminInfo) > 0 {
		other := map[string]interface{}{
			"admin_info": adminInfo,
		}
		log.Other = common.MapToJsonStr(other)
	}
	if err := LOG_DB.Create(log).Error; err != nil {
		common.SysLog("failed to record log: " + err.Error())
	}
}

func RecordTopupLog(userId int, content string, callerIp string, paymentMethod string, callbackPaymentMethod string) {
	username, _ := GetUsernameById(userId, false)
	adminInfo := map[string]interface{}{
		"server_ip":               common.GetIp(),
		"node_name":               common.NodeName,
		"caller_ip":               callerIp,
		"payment_method":          paymentMethod,
		"callback_payment_method": callbackPaymentMethod,
		"version":                 common.Version,
	}
	other := map[string]interface{}{
		"admin_info": adminInfo,
	}
	log := &Log{
		UserId:    userId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      LogTypeTopup,
		Content:   content,
		Ip:        callerIp,
		Other:     common.MapToJsonStr(other),
	}
	err := LOG_DB.Create(log).Error
	if err != nil {
		common.SysLog("failed to record topup log: " + err.Error())
	}
}

func RecordErrorLog(c *gin.Context, userId int, channelId int, modelName string, tokenName string, content string, tokenId int, useTimeSeconds int,
	isStream bool, group string, other map[string]interface{}) {
	logger.LogInfo(c, fmt.Sprintf("record error log: userId=%d, channelId=%d, modelName=%s, tokenName=%s, content=%s", userId, channelId, modelName, tokenName, common.LocalLogPreview(content)))
	username := c.GetString("username")
	requestId := c.GetString(common.RequestIdKey)
	upstreamRequestId := c.GetString(common.UpstreamRequestIdKey)
	otherStr := common.MapToJsonStr(other)
	// 判断是否需要记录 IP
	needRecordIp := false
	if settingMap, err := GetUserSetting(userId, false); err == nil {
		if settingMap.RecordIpLog {
			needRecordIp = true
		}
	}
	log := &Log{
		UserId:           userId,
		Username:         username,
		CreatedAt:        common.GetTimestamp(),
		Type:             LogTypeError,
		Content:          content,
		PromptTokens:     0,
		CompletionTokens: 0,
		TokenName:        tokenName,
		ModelName:        modelName,
		Quota:            0,
		ChannelId:        channelId,
		TokenId:          tokenId,
		UseTime:          useTimeSeconds,
		IsStream:         isStream,
		Group:            group,
		Ip: func() string {
			if needRecordIp {
				return requestip.GetClientIP(c)
			}
			return ""
		}(),
		RequestId:         requestId,
		UpstreamRequestId: upstreamRequestId,
		Other:             otherStr,
	}
	err := LOG_DB.Create(log).Error
	if err != nil {
		logger.LogError(c, "failed to record log: "+err.Error())
	}
	if common.GroupMonitoringHook != nil {
		sc := 0
		if v, ok := other["status_code"]; ok {
			switch n := v.(type) {
			case int:
				sc = n
			case float64:
				sc = int(n)
			}
		}
		common.GroupMonitoringHook(group, channelId, false, 0, 0, useTimeSeconds*1000, 0, modelName, sc, content)
	}
}

type RecordConsumeLogParams struct {
	ChannelId        int                    `json:"channel_id"`
	PromptTokens     int                    `json:"prompt_tokens"`
	CompletionTokens int                    `json:"completion_tokens"`
	ModelName        string                 `json:"model_name"`
	TokenName        string                 `json:"token_name"`
	Quota            int                    `json:"quota"`
	Content          string                 `json:"content"`
	TokenId          int                    `json:"token_id"`
	UseTimeSeconds   int                    `json:"use_time_seconds"`
	IsStream         bool                   `json:"is_stream"`
	Group            string                 `json:"group"`
	Other            map[string]interface{} `json:"other"`
}

func RecordConsumeLog(c *gin.Context, userId int, params RecordConsumeLogParams) {
	if !common.LogConsumeEnabled {
		return
	}
	logger.LogInfo(c, fmt.Sprintf("record consume log: userId=%d, params=%s", userId, common.GetJsonString(params)))
	username := c.GetString("username")
	requestId := c.GetString(common.RequestIdKey)
	upstreamRequestId := c.GetString(common.UpstreamRequestIdKey)
	otherStr := common.MapToJsonStr(params.Other)
	// 判断是否需要记录 IP
	needRecordIp := false
	if settingMap, err := GetUserSetting(userId, false); err == nil {
		if settingMap.RecordIpLog {
			needRecordIp = true
		}
	}
	log := &Log{
		UserId:           userId,
		Username:         username,
		CreatedAt:        common.GetTimestamp(),
		Type:             LogTypeConsume,
		Content:          params.Content,
		PromptTokens:     params.PromptTokens,
		CompletionTokens: params.CompletionTokens,
		TokenName:        params.TokenName,
		ModelName:        params.ModelName,
		Quota:            params.Quota,
		ChannelId:        params.ChannelId,
		TokenId:          params.TokenId,
		UseTime:          params.UseTimeSeconds,
		IsStream:         params.IsStream,
		Group:            params.Group,
		Ip: func() string {
			if needRecordIp {
				return requestip.GetClientIP(c)
			}
			return ""
		}(),
		RequestId:         requestId,
		UpstreamRequestId: upstreamRequestId,
		Other:             otherStr,
	}
	err := LOG_DB.Create(log).Error
	if err != nil {
		logger.LogError(c, "failed to record log: "+err.Error())
	}
	if common.DataExportEnabled {
		gopool.Go(func() {
			LogQuotaData(userId, username, params.ModelName, params.Quota, common.GetTimestamp(), params.PromptTokens+params.CompletionTokens)
		})
	}
	if common.GroupMonitoringHook != nil {
		frtMs := 0
		if frt, ok := params.Other["frt"]; ok {
			if v, ok := frt.(float64); ok {
				frtMs = int(v)
			}
		}
		cacheTokens := 0
		if ct, ok := params.Other["cache_tokens"]; ok {
			switch v := ct.(type) {
			case int:
				cacheTokens = v
			case float64:
				cacheTokens = int(v)
			}
		}
		monitorPromptTokens := params.PromptTokens
		if sem, ok := params.Other["usage_semantic"]; ok && sem == "anthropic" && cacheTokens > 0 {
			monitorPromptTokens += cacheTokens
		}
		common.GroupMonitoringHook(params.Group, params.ChannelId, true, monitorPromptTokens, cacheTokens, params.UseTimeSeconds*1000, frtMs, params.ModelName, 0, "")
	}
}

type RecordTaskBillingLogParams struct {
	UserId    int
	LogType   int
	Content   string
	ChannelId int
	ModelName string
	Quota     int
	TokenId   int
	Group     string
	Other     map[string]interface{}
}

func RecordTaskBillingLog(params RecordTaskBillingLogParams) {
	if params.LogType == LogTypeConsume && !common.LogConsumeEnabled {
		return
	}
	username, _ := GetUsernameById(params.UserId, false)
	tokenName := ""
	if params.TokenId > 0 {
		if token, err := GetTokenById(params.TokenId); err == nil {
			tokenName = token.Name
		}
	}
	log := &Log{
		UserId:    params.UserId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      params.LogType,
		Content:   params.Content,
		TokenName: tokenName,
		ModelName: params.ModelName,
		Quota:     params.Quota,
		ChannelId: params.ChannelId,
		TokenId:   params.TokenId,
		Group:     params.Group,
		Other:     common.MapToJsonStr(params.Other),
	}
	err := LOG_DB.Create(log).Error
	if err != nil {
		common.SysLog("failed to record task billing log: " + err.Error())
	}
}

// LogSortOptions 用法跟 ChannelSortOptions 类似——白名单字段 + asc/desc。
// 用户传非法字段时回退到默认 id desc。
type LogSortOptions struct {
	SortBy    string
	SortOrder string
}

var logSortColumns = map[string]bool{
	"id":                true,
	"created_at":        true,
	"quota":             true,
	"use_time":          true,
	"prompt_tokens":     true,
	"completion_tokens": true,
}

func NewLogSortOptions(sortBy string, sortOrder string) LogSortOptions {
	by := strings.ToLower(strings.TrimSpace(sortBy))
	order := strings.ToLower(strings.TrimSpace(sortOrder))
	if !logSortColumns[by] {
		by = ""
		order = ""
	} else if order != "asc" {
		order = "desc"
	}
	return LogSortOptions{SortBy: by, SortOrder: order}
}

// Apply 给查询追加 ORDER BY；未指定时回退到 logs.id desc，跟历史行为兼容。
func (o LogSortOptions) Apply(tx *gorm.DB) *gorm.DB {
	if o.SortBy == "" {
		return tx.Order("logs.id desc")
	}
	return tx.Order("logs." + o.SortBy + " " + o.SortOrder + ", logs.id desc")
}

func GetAllLogs(logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string, startIdx int, num int, channel int, group string, requestId string, upstreamRequestId string, userId int, sortOpts LogSortOptions) (logs []*Log, total int64, err error) {
	// eflow: 8s 兜底，防止慢查询堆积。
	ctx, cancel := logQueryCtx()
	defer cancel()
	logDB := LOG_DB.WithContext(ctx)

	// eflow: 是否带任何过滤条件。无过滤的「全部日志」总数走估算（秒回），
	// 不再跑撞 8s 的全表 COUNT。基于原始入参判断（username 后面可能被
	// resolveLogUsername 改写为 user_id 等值，但那同样算「有过滤」）。
	hasFilter := logType != LogTypeUnknown || userId != 0 || modelName != "" || username != "" ||
		tokenName != "" || requestId != "" || upstreamRequestId != "" ||
		startTimestamp != 0 || endTimestamp != 0 || channel != 0 || group != ""

	// eflow: COUNT 缓存 key —— 基于全部筛选条件（用原始入参，不含分页 startIdx/num）。
	// 同一组筛选下翻页/跳页复用，不再每页重跑 COUNT。
	countKey := fmt.Sprintf("cnt:all|%d|%d|%d|%s|%s|%s|%d|%s|%s|%s|%d",
		logType, startTimestamp, endTimestamp, modelName, username, tokenName,
		channel, group, requestId, upstreamRequestId, userId)

	var tx *gorm.DB
	if logType == LogTypeUnknown {
		tx = logDB
	} else {
		tx = logDB.Where("logs.type = ?", logType)
	}

	if userId != 0 {
		tx = tx.Where("logs.user_id = ?", userId)
	}
	tx = applyLogModelFilter(tx, "logs.model_name", modelName)
	// eflow: 未指定 user_id 时，精确用户名走 user_id 等值，避开 username LIKE 全表扫。
	if userId == 0 {
		if uid, ok := resolveLogUsername(username); ok {
			tx = tx.Where("logs.user_id = ?", uid)
			username = ""
		}
	}
	tx = applyLogContainsFilter(tx, "logs.username", username)
	tx = applyLogTokenFilter(tx, "logs.token_name", tokenName)
	if requestId != "" {
		tx = tx.Where("logs.request_id = ?", requestId)
	}
	if upstreamRequestId != "" {
		tx = tx.Where("logs.upstream_request_id = ?", upstreamRequestId)
	}
	if startTimestamp != 0 {
		tx = tx.Where("logs.created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("logs.created_at <= ?", endTimestamp)
	}
	if channel != 0 {
		tx = tx.Where("logs.channel_id = ?", channel)
	}
	if group != "" {
		tx = tx.Where("logs."+logGroupCol+" = ?", group)
	}
	// eflow: 无过滤 → 估算总数（秒回，不撞 logQueryTimeout）；有过滤 → 真实 COUNT
	// （被过滤收窄、走索引）；count 失败/超时 → 降级估算，绝不让整页报错弹 context 异常。
	if !hasFilter {
		total = estimatedLogTotal()
	} else if cached, ok := getCachedLogAgg(countKey); ok {
		total = cached
	} else if cErr := tx.Model(&Log{}).Count(&total).Error; cErr != nil {
		common.SysError("eflow: GetAllLogs count failed, fallback to estimate: " + cErr.Error())
		total = estimatedLogTotal()
	} else {
		setCachedLogAgg(countKey, total)
	}
	err = sortOpts.Apply(tx).Limit(num).Offset(startIdx).Find(&logs).Error
	if err != nil {
		return nil, 0, err
	}

	channelIds := types.NewSet[int]()
	for _, log := range logs {
		if log.ChannelId != 0 {
			channelIds.Add(log.ChannelId)
		}
	}

	if channelIds.Len() > 0 {
		var channels []struct {
			Id   int    `gorm:"column:id"`
			Name string `gorm:"column:name"`
		}
		if common.MemoryCacheEnabled {
			// Cache get channel
			for _, channelId := range channelIds.Items() {
				if cacheChannel, err := CacheGetChannel(channelId); err == nil {
					channels = append(channels, struct {
						Id   int    `gorm:"column:id"`
						Name string `gorm:"column:name"`
					}{
						Id:   channelId,
						Name: cacheChannel.Name,
					})
				}
			}
		} else {
			// Bulk query channels from DB
			if err = DB.Table("channels").Select("id, name").Where("id IN ?", channelIds.Items()).Find(&channels).Error; err != nil {
				return logs, total, err
			}
		}
		channelMap := make(map[int]string, len(channels))
		for _, channel := range channels {
			channelMap[channel.Id] = channel.Name
		}
		for i := range logs {
			logs[i].ChannelName = channelMap[logs[i].ChannelId]
		}
	}

	return logs, total, err
}

func GetUserLogs(userId int, logType int, startTimestamp int64, endTimestamp int64, modelName string, tokenName string, startIdx int, num int, group string, requestId string, upstreamRequestId string, sortOpts LogSortOptions) (logs []*Log, total int64, err error) {
	// eflow: 8s 兜底，防止慢查询堆积。
	ctx, cancel := logQueryCtx()
	defer cancel()
	logDB := LOG_DB.WithContext(ctx)

	// eflow: COUNT 缓存 key —— 同一组筛选下翻页/跳页复用，不再每页重跑 COUNT。
	countKey := fmt.Sprintf("cnt:user|%d|%d|%d|%d|%s|%s|%s|%s|%s",
		userId, logType, startTimestamp, endTimestamp, modelName, tokenName, group, requestId, upstreamRequestId)

	var tx *gorm.DB
	if logType == LogTypeUnknown {
		tx = logDB.Where("logs.user_id = ?", userId)
	} else {
		tx = logDB.Where("logs.user_id = ? and logs.type = ?", userId, logType)
	}

	tx = applyLogModelFilter(tx, "logs.model_name", modelName)
	tx = applyLogTokenFilter(tx, "logs.token_name", tokenName)
	if requestId != "" {
		tx = tx.Where("logs.request_id = ?", requestId)
	}
	if upstreamRequestId != "" {
		tx = tx.Where("logs.upstream_request_id = ?", upstreamRequestId)
	}
	if startTimestamp != 0 {
		tx = tx.Where("logs.created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("logs.created_at <= ?", endTimestamp)
	}
	if group != "" {
		tx = tx.Where("logs."+logGroupCol+" = ?", group)
	}
	// eflow: 去掉失效的 Limit(logSearchCountLimit)（GORM 的 Count 不理会 LIMIT，
	// 那个封顶一直是空操作）。count 改为非致命：失败也不连累日志行返回，
	// 总数降级为 0（极少触发，user_id 走索引很快）。
	if cached, ok := getCachedLogAgg(countKey); ok {
		total = cached
	} else if cErr := tx.Model(&Log{}).Count(&total).Error; cErr != nil {
		common.SysError("eflow: GetUserLogs count failed: " + cErr.Error())
		total = 0
	} else {
		setCachedLogAgg(countKey, total)
	}
	err = sortOpts.Apply(tx).Limit(num).Offset(startIdx).Find(&logs).Error
	if err != nil {
		common.SysError("failed to search user logs: " + err.Error())
		return nil, 0, errors.New("查询日志失败")
	}

	formatUserLogs(logs, startIdx)
	return logs, total, err
}

type Stat struct {
	Quota int `json:"quota"`
	Rpm   int `json:"rpm"`
	Tpm   int `json:"tpm"`
}

// eflow: 短于该长度的 LIKE '%xx%' 过滤被忽略——单字符子串等于全表扫，
// 既无业务价值（命中太多）又是把 logs 表打爆的快通道。
// 静默忽略而非报错，对正常使用零感知；想搜单字的极端情况自己加一个字符。
const logContainsMinRunes = 2

func logContainsPattern(input string) (string, bool) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", false
	}
	if utf8.RuneCountInString(input) < logContainsMinRunes {
		return "", false
	}

	replacer := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_")
	return "%" + replacer.Replace(input) + "%", true
}

func applyLogContainsFilter(tx *gorm.DB, column string, value string) *gorm.DB {
	pattern, ok := logContainsPattern(value)
	if !ok {
		return tx
	}
	return tx.Where(column+" LIKE ? ESCAPE '!'", pattern)
}

// eflow: 把精确用户名解析成 user_id（users.username 唯一且有索引，瞬时命中）。
// logs 表 200 万+ 行，username 上的 LIKE '%x%' 是前导通配，用不了索引 → 全表扫
// （实测空载就 3.2s，叠加线上负载破 8s logQueryTimeout 被 KILL，前端报
// "context deadline exceeded"）。精确命中时改用 user_id 等值（走 idx_user_id_id，
// ~0.01s）。命不中（子串/历史用户名）才退回 LIKE，保留模糊检索能力。
func resolveLogUsername(username string) (int, bool) {
	username = strings.TrimSpace(username)
	if username == "" {
		return 0, false
	}
	var ids []int
	if err := DB.Model(&User{}).Where("username = ?", username).Limit(1).Pluck("id", &ids).Error; err != nil {
		return 0, false
	}
	if len(ids) == 0 || ids[0] == 0 {
		return 0, false
	}
	return ids[0], true
}

// eflow: resolveLogModelName 判断 modelName 是否是「已知模型」（abilities 里存在）。
// logs 表 200 万+ 行，model_name 上的 LIKE '%x%' 是前导通配，用不了
// index_username_model_name(priority:1=model_name) → 全表扫，撞 8s logQueryTimeout
// 被 KILL，前端报 "context deadline exceeded"。管理员「筛某个模型的使用」基本都是
// 完整模型名，精确命中已知模型时改走 model_name = ? 等值（命中索引，~0.01s）。
// abilities 表很小（几百~几千行），存在性探测 sub-ms。
func resolveLogModelName(modelName string) (string, bool) {
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		return "", false
	}
	var models []string
	if err := DB.Table("abilities").Where("model = ?", modelName).Limit(1).Pluck("model", &models).Error; err != nil {
		return "", false
	}
	if len(models) == 0 {
		return "", false
	}
	return modelName, true
}

// eflow: model_name 过滤优先走精确等值（命中索引，秒回）；命不中已知模型（子串检索/
// 历史下线模型）才退回 applyLogContainsFilter 的 LIKE '%x%'，保留模糊搜索能力，
// 慢路径仍受 logQueryTimeout 兜底。
func applyLogModelFilter(tx *gorm.DB, column string, value string) *gorm.DB {
	if exact, ok := resolveLogModelName(value); ok {
		return tx.Where(column+" = ?", exact)
	}
	return applyLogContainsFilter(tx, column, value)
}

// eflow: resolveLogTokenName 判断 tokenName 是否是「已知令牌名」（tokens 表存在）。
// 同 model_name：logs.token_name 上的 LIKE '%x%' 是前导通配，用不了已有的
// token_name 单列索引 → 全表扫，撞 8s logQueryTimeout 被 KILL。精确命中已知令牌名时
// 改走 token_name = ? 等值（走已有索引，~0.01s）。命不中（子串/已删除令牌）才退回
// LIKE，保留模糊检索能力。tokens.name 有索引，存在性探测 sub-ms。
func resolveLogTokenName(tokenName string) (string, bool) {
	tokenName = strings.TrimSpace(tokenName)
	if tokenName == "" {
		return "", false
	}
	var names []string
	if err := DB.Model(&Token{}).Where("name = ?", tokenName).Limit(1).Pluck("name", &names).Error; err != nil {
		return "", false
	}
	if len(names) == 0 {
		return "", false
	}
	return tokenName, true
}

// eflow: token_name 过滤优先走精确等值（命中已有索引），命不中已知令牌名才退回子串 LIKE。
func applyLogTokenFilter(tx *gorm.DB, column string, value string) *gorm.DB {
	if exact, ok := resolveLogTokenName(value); ok {
		return tx.Where(column+" = ?", exact)
	}
	return applyLogContainsFilter(tx, column, value)
}

// eflow: logs 的 COUNT/SUM 聚合缓存。按「筛选条件（不含分页 offset/num）」做 key，
// 同一组筛选下翻页/跳页复用——避免每翻一页都重跑 COUNT(*)（非选择性筛选下那条
// COUNT 正是撞 8s logQueryTimeout、前端弹 "context deadline exceeded" 的主因）。
// 跳到任意页码与精确总数都保留：页码只改 OFFSET，与总数无关。换一组筛选后第一页
// 仍真算一次（key 含全部筛选条件，不同筛选不同 key）。
// 单实例部署，进程内缓存足够；TTL 短，容忍总数在窗口内差几条新日志。
const (
	logAggCacheTTL = 45 * time.Second
	logAggCacheCap = 1024
)

type logAggEntry struct {
	value    int64
	expireAt time.Time
}

var (
	logAggCacheMu  sync.RWMutex
	logAggCacheMap = make(map[string]logAggEntry)
)

func getCachedLogAgg(key string) (int64, bool) {
	logAggCacheMu.RLock()
	e, ok := logAggCacheMap[key]
	logAggCacheMu.RUnlock()
	if !ok || time.Now().After(e.expireAt) {
		return 0, false
	}
	return e.value, true
}

func setCachedLogAgg(key string, value int64) {
	now := time.Now()
	logAggCacheMu.Lock()
	defer logAggCacheMu.Unlock()
	// 容量保护：满了先清过期项，仍满则整体重置（聚合值重算成本低，绝不让缓存无限涨）。
	if len(logAggCacheMap) >= logAggCacheCap {
		for k, e := range logAggCacheMap {
			if now.After(e.expireAt) {
				delete(logAggCacheMap, k)
			}
		}
		if len(logAggCacheMap) >= logAggCacheCap {
			logAggCacheMap = make(map[string]logAggEntry, logAggCacheCap)
		}
	}
	logAggCacheMap[key] = logAggEntry{value: value, expireAt: now.Add(logAggCacheTTL)}
}

func SumUsedQuota(logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string, channel int, group string, userId int) (stat Stat, err error) {
	// eflow: 8s 兜底，防止慢查询堆积。
	ctx, cancel := logQueryCtx()
	defer cancel()
	logDB := LOG_DB.WithContext(ctx)

	// eflow: 只缓存「消费额求和」这条贵查询（key 用原始入参，username 后面会被改写）。
	// rpm/tpm 是最近 60s 窗口、且本就便宜，保持每次实算，不进缓存。
	sumKey := fmt.Sprintf("sum:quota|%d|%d|%d|%s|%s|%s|%d|%s|%d",
		logType, startTimestamp, endTimestamp, modelName, username, tokenName, channel, group, userId)

	tx := logDB.Table("logs").Select("sum(quota) quota")

	// 为rpm和tpm创建单独的查询
	rpmTpmQuery := logDB.Table("logs").Select("count(*) rpm, sum(prompt_tokens) + sum(completion_tokens) tpm")

	if userId != 0 {
		tx = tx.Where("user_id = ?", userId)
		rpmTpmQuery = rpmTpmQuery.Where("user_id = ?", userId)
	}
	// eflow: 未指定 user_id 时，精确用户名走 user_id 等值，避开 username LIKE 全表扫。
	if userId == 0 {
		if uid, ok := resolveLogUsername(username); ok {
			tx = tx.Where("user_id = ?", uid)
			rpmTpmQuery = rpmTpmQuery.Where("user_id = ?", uid)
			username = ""
		}
	}
	tx = applyLogContainsFilter(tx, "username", username)
	rpmTpmQuery = applyLogContainsFilter(rpmTpmQuery, "username", username)
	tx = applyLogTokenFilter(tx, "token_name", tokenName)
	rpmTpmQuery = applyLogTokenFilter(rpmTpmQuery, "token_name", tokenName)
	if startTimestamp != 0 {
		tx = tx.Where("created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("created_at <= ?", endTimestamp)
	}
	tx = applyLogModelFilter(tx, "model_name", modelName)
	rpmTpmQuery = applyLogModelFilter(rpmTpmQuery, "model_name", modelName)
	if channel != 0 {
		tx = tx.Where("channel_id = ?", channel)
		rpmTpmQuery = rpmTpmQuery.Where("channel_id = ?", channel)
	}
	if group != "" {
		tx = tx.Where(logGroupCol+" = ?", group)
		rpmTpmQuery = rpmTpmQuery.Where(logGroupCol+" = ?", group)
	}

	tx = tx.Where("type = ?", LogTypeConsume)
	rpmTpmQuery = rpmTpmQuery.Where("type = ?", LogTypeConsume)

	// 只统计最近60秒的rpm和tpm
	rpmTpmQuery = rpmTpmQuery.Where("created_at >= ?", time.Now().Add(-60*time.Second).Unix())

	// 执行查询
	if cached, ok := getCachedLogAgg(sumKey); ok {
		stat.Quota = int(cached)
	} else if err := tx.Scan(&stat).Error; err != nil {
		common.SysError("failed to query log stat: " + err.Error())
		return stat, errors.New("查询统计数据失败")
	} else {
		setCachedLogAgg(sumKey, int64(stat.Quota))
	}
	if err := rpmTpmQuery.Scan(&stat).Error; err != nil {
		common.SysError("failed to query rpm/tpm stat: " + err.Error())
		return stat, errors.New("查询统计数据失败")
	}

	return stat, nil
}

func SumUsedToken(logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string) (token int) {
	tx := LOG_DB.Table("logs").Select("ifnull(sum(prompt_tokens),0) + ifnull(sum(completion_tokens),0)")
	if username != "" {
		tx = tx.Where("username = ?", username)
	}
	if tokenName != "" {
		tx = tx.Where("token_name = ?", tokenName)
	}
	if startTimestamp != 0 {
		tx = tx.Where("created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("created_at <= ?", endTimestamp)
	}
	if modelName != "" {
		tx = tx.Where("model_name = ?", modelName)
	}
	tx.Where("type = ?", LogTypeConsume).Scan(&token)
	return token
}

func DeleteOldLog(ctx context.Context, targetTimestamp int64, limit int) (int64, error) {
	var total int64 = 0

	for {
		if nil != ctx.Err() {
			return total, ctx.Err()
		}

		result := LOG_DB.Where("created_at < ?", targetTimestamp).Limit(limit).Delete(&Log{})
		if nil != result.Error {
			return total, result.Error
		}

		total += result.RowsAffected

		if result.RowsAffected < int64(limit) {
			break
		}
	}

	return total, nil
}
