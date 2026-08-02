package model

// eflow: 用户使用日志导出（报账明细）。复用 GetUserLogs 的 WHERE 过滤，
// 但不分页、按时间升序，便于财务直接贴报销单。带硬行数上限保护 4C8G 机器。

import (
	"context"
	"errors"
	"time"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

// logExportRowLimit 同步导出（直接下载）单次最大行数，超出截断（取最早的部分）。
const logExportRowLimit = 100000

// logExportRowLimitOffline 离线导出（后台异步发邮件）单次最大行数。
// 后台跑、不占 HTTP 请求与限流额度，可比同步导出放宽。
const logExportRowLimitOffline = 300000

// GetUserLogsForExport 返回某用户在筛选条件下的全部日志（无分页），
// 按 created_at 升序排列。第二个返回值标记是否触达上限被截断。
// 同步导出口径：10w 行 / 30s。
func GetUserLogsForExport(userId int, logType int, startTimestamp int64, endTimestamp int64, modelName string, tokenName string, group string, requestId string) (logs []*Log, truncated bool, err error) {
	return GetUserLogsForExportWithLimit(userId, logType, startTimestamp, endTimestamp, modelName, tokenName, group, requestId, logExportRowLimit, 30*time.Second)
}

// GetUserLogsForExportWithLimit 是 GetUserLogsForExport 的可调版本：
// rowLimit 控制最大导出行数、queryTimeout 控制查询超时。离线导出用更大的上限
// 和更长的超时（后台跑得起），同步导出沿用 10w/30s。
func GetUserLogsForExportWithLimit(userId int, logType int, startTimestamp int64, endTimestamp int64, modelName string, tokenName string, group string, requestId string, rowLimit int, queryTimeout time.Duration) (logs []*Log, truncated bool, err error) {
	// eflow: 导出要全字段读出，比普通查询贵得多——给一个比 logQueryTimeout 长、
	// 但有界的超时，ctx 取消会 KILL QUERY。
	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()
	logDB := LOG_DB.WithContext(ctx)

	var tx *gorm.DB
	if logType == LogTypeUnknown {
		tx = logDB.Where("logs.user_id = ?", userId)
	} else {
		tx = logDB.Where("logs.user_id = ? and logs.type = ?", userId, logType)
	}

	if modelName != "" {
		modelNamePattern, perr := sanitizeLikePattern(modelName)
		if perr != nil {
			return nil, false, perr
		}
		tx = tx.Where("logs.model_name LIKE ? ESCAPE '!'", modelNamePattern)
	}
	if tokenName != "" {
		tx = tx.Where("logs.token_name = ?", tokenName)
	}
	if requestId != "" {
		tx = tx.Where("logs.request_id = ?", requestId)
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

	err = tx.Order("logs.created_at asc, logs.id asc").Limit(rowLimit + 1).Find(&logs).Error
	if err != nil {
		common.SysError("failed to export user logs: " + err.Error())
		return nil, false, errors.New("导出日志失败")
	}

	if len(logs) > rowLimit {
		logs = logs[:rowLimit]
		truncated = true
	}

	// 与 /self 一致：抹掉渠道名等管理员字段，仅保留用户可见信息。
	formatUserLogs(logs, 0)
	return logs, truncated, nil
}
