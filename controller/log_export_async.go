package controller

// eflow: 普通用户「离线导出」使用日志（报账用）。
// 同步导出（ExportUserLogs）一次取最多 10w 行、内存拼 ZIP 再回传，大区间会长时间
// 占着一个 in-flight 请求，吃限流额度还容易在 CF ~100s 边界超时。离线导出改为：
//   1) 立即入队、立刻返回（「任务已提交，完成后发到邮箱」）；
//   2) 后台 goroutine 跑查询（放宽到 30w 行 / 120s）→ 拼 ZIP → 发邮件附件。
// 复用 buildUserLogsExportZip / exportFilename（见 log_export.go）与
// common.SendEmailWithAttachments（见 common/email.go），不引新依赖。

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
)

const (
	// offlineExportRowLimit 离线导出单次最大行数（后台跑，比同步的 10w 放宽）。
	offlineExportRowLimit = 300000
	// offlineExportQueryTimeout 离线导出查询超时（后台跑，可比同步的 30s 放宽）。
	offlineExportQueryTimeout = 120 * time.Second
	// offlineExportMaxAttachBytes 邮件附件体积上限（约 25MB）。超出不发附件，
	// 改发提示邮件让用户缩小区间分批导出——避免 SMTP 静默丢弃。
	offlineExportMaxAttachBytes = 25 * 1024 * 1024
	// offlineExportGlobalConcurrency 全局同时在跑的离线导出任务数上限，保护机器内存。
	offlineExportGlobalConcurrency = 2
)

// offlineExportJobs 跟踪「每用户当前是否有离线导出在跑」。任务在后台 goroutine
// 全程持有（不是 HTTP 请求生命周期——UserConcurrencyLimit 在 handler return 即
// 释放，对异步任务无效，故这里自管）。
var (
	offlineExportMu   sync.Mutex
	offlineExportJobs = make(map[int]struct{})
	// offlineExportSlots 全局并发信号量。
	offlineExportSlots = make(chan struct{}, offlineExportGlobalConcurrency)
)

// tryAcquireOfflineExport 尝试为某用户占一个离线导出名额。返回是否成功、以及失败原因。
// 成功时同时占用「用户单任务」和「全局并发」两把锁，必须在任务结束时
// releaseOfflineExport 释放。
func tryAcquireOfflineExport(userId int) (ok bool, reason string) {
	offlineExportMu.Lock()
	if _, exists := offlineExportJobs[userId]; exists {
		offlineExportMu.Unlock()
		return false, "您有一个离线导出任务正在处理中，请等当前任务完成后再试"
	}
	// 先尝试占全局名额（非阻塞），占不到说明系统繁忙。
	select {
	case offlineExportSlots <- struct{}{}:
	default:
		offlineExportMu.Unlock()
		return false, "导出服务繁忙，请稍后再试"
	}
	offlineExportJobs[userId] = struct{}{}
	offlineExportMu.Unlock()
	return true, ""
}

// releaseOfflineExport 释放某用户的离线导出名额（用户单任务 + 全局并发）。
func releaseOfflineExport(userId int) {
	offlineExportMu.Lock()
	delete(offlineExportJobs, userId)
	offlineExportMu.Unlock()
	select {
	case <-offlineExportSlots:
	default:
	}
}

// ExportUserLogsAsync 处理离线导出请求：解析收件邮箱、占名额、起后台任务、立刻返回。
func ExportUserLogsAsync(c *gin.Context) {
	userId := c.GetInt("id")

	// 收件人：优先用请求里填的 email（需通过格式校验）；为空则回落到绑定邮箱。
	recipient := strings.TrimSpace(c.Query("email"))
	if recipient != "" {
		if err := common.Validate.Var(recipient, "required,email"); err != nil {
			common.ApiErrorMsg(c, "收件邮箱格式不正确，请检查后重试")
			return
		}
	} else {
		bound, err := model.GetUserEmail(userId)
		if err != nil || strings.TrimSpace(bound) == "" {
			common.ApiErrorMsg(c, "你尚未绑定邮箱，请先在个人设置绑定邮箱，或在此填写收件邮箱")
			return
		}
		recipient = strings.TrimSpace(bound)
	}

	// 占名额（用户单任务 + 全局并发），占不到直接拒绝。
	if ok, reason := tryAcquireOfflineExport(userId); !ok {
		common.ApiErrorMsg(c, reason)
		return
	}

	// 快照本次筛选参数，交给后台 goroutine（c 不能跨协程用）。
	logType, _ := strconv.Atoi(c.Query("type"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	job := offlineExportParams{
		userId:         userId,
		recipient:      recipient,
		logType:        logType,
		startTimestamp: startTimestamp,
		endTimestamp:   endTimestamp,
		tokenName:      c.Query("token_name"),
		modelName:      c.Query("model_name"),
		group:          c.Query("group"),
		requestId:      c.Query("request_id"),
	}

	gopool.Go(func() {
		defer releaseOfflineExport(userId)
		runOfflineExport(job)
	})

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": fmt.Sprintf("离线导出任务已提交，完成后将发送到 %s，请稍后查收邮件", common.MaskEmail(recipient)),
	})
}

type offlineExportParams struct {
	userId                       int
	recipient                    string
	logType                      int
	startTimestamp, endTimestamp int64
	tokenName, modelName         string
	group, requestId             string
}

// runOfflineExport 在后台执行查询 + 拼 ZIP + 发邮件。所有错误只记日志（用户侧已
// 提示「稍后查收」），不阻塞、不抛给请求方。
func runOfflineExport(p offlineExportParams) {
	logs, truncated, err := model.GetUserLogsForExportWithLimit(
		p.userId, p.logType, p.startTimestamp, p.endTimestamp,
		p.modelName, p.tokenName, p.group, p.requestId,
		offlineExportRowLimit, offlineExportQueryTimeout,
	)
	if err != nil {
		common.SysError(fmt.Sprintf("offline export: query failed for user %d: %s", p.userId, err.Error()))
		_ = common.SendEmail("离线导出失败", p.recipient,
			"很抱歉，本次离线导出生成失败（查询超时或数据量过大）。请缩小时间范围后重试。")
		return
	}

	zipBytes, err := buildUserLogsExportZip(logs, truncated)
	if err != nil {
		common.SysError(fmt.Sprintf("offline export: build zip failed for user %d: %s", p.userId, err.Error()))
		_ = common.SendEmail("离线导出失败", p.recipient,
			"很抱歉，本次离线导出打包失败，请稍后重试或缩小时间范围。")
		return
	}

	rangeText := exportRangeText(p.startTimestamp, p.endTimestamp)

	// 附件过大兜底：不发附件，改发提示邮件。
	if len(zipBytes) > offlineExportMaxAttachBytes {
		common.SysError(fmt.Sprintf("offline export: zip too large (%d bytes) for user %d", len(zipBytes), p.userId))
		body := fmt.Sprintf(
			"您本次导出（%s，共 %d 条记录）打包后约 %.1f MB，超过邮件附件上限，无法发送。<br/>请缩小时间范围分批导出。",
			rangeText, len(logs), float64(len(zipBytes))/1024/1024)
		_ = common.SendEmail("离线导出文件过大", p.recipient, body)
		return
	}

	filename := exportFilename(p.userId, p.startTimestamp, p.endTimestamp)
	body := fmt.Sprintf(
		"您好，<br/><br/>您申请的使用日志离线导出已生成（%s，共 %d 条记录）。<br/>"+
			"附件为 ZIP 压缩包，内含两份 CSV：<b>使用明细.csv</b>（逐条调用记录）与 <b>消费汇总.csv</b>（按模型聚合 + 总计），"+
			"均为 UTF-8 BOM，Excel 可直接打开。<br/>",
		rangeText, len(logs))
	if truncated {
		body += fmt.Sprintf("<br/>注：本次数据已达单次导出上限 %d 条，仅含时间最早的部分，请缩小时间范围分批导出。<br/>", offlineExportRowLimit)
	}
	body += "<br/>—— " + common.SystemName

	attach := []common.EmailAttachment{{
		FileName:    filename,
		ContentType: "application/zip",
		Data:        zipBytes,
	}}
	if err := common.SendEmailWithAttachments("使用日志离线导出", p.recipient, body, attach); err != nil {
		common.SysError(fmt.Sprintf("offline export: send mail failed for user %d: %s", p.userId, err.Error()))
	}
}

// exportRangeText 把导出时间区间渲染成可读文本，用于邮件正文。
func exportRangeText(startTimestamp, endTimestamp int64) string {
	if startTimestamp != 0 && endTimestamp != 0 {
		return time.Unix(startTimestamp, 0).Format("2006-01-02") + " 至 " + time.Unix(endTimestamp, 0).Format("2006-01-02")
	}
	return "全部时间"
}
