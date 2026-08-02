package controller

// eflow: 普通用户自助导出使用日志（报账用）。
// 输出一个 ZIP，内含两份 UTF-8 BOM 的 CSV：
//   使用明细.csv —— 逐条调用记录
//   消费汇总.csv —— 按模型聚合 + 总计
// 复用 /api/log/self 同款筛选参数；纯标准库，不引新依赖（少给 eflow rebase 添负担）。

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

var logTypeLabelZh = map[int]string{
	model.LogTypeUnknown: "未知",
	model.LogTypeTopup:   "充值",
	model.LogTypeConsume: "消费",
	model.LogTypeManage:  "管理",
	model.LogTypeSystem:  "系统",
	model.LogTypeError:   "错误",
	model.LogTypeRefund:  "退款",
}

// quotaToYuan 把内部额度换算成金额（元）。本站汇率 1元=1美元，
// 口径与前端日志列展示完全一致：amount = quota / QuotaPerUnit。
func quotaToYuan(quota int) string {
	if common.QuotaPerUnit <= 0 {
		return "0"
	}
	return strconv.FormatFloat(float64(quota)/common.QuotaPerUnit, 'f', 6, 64)
}

// writeCSVWithBOM 在 zip 条目里写 UTF-8 BOM + CSV，保证 Excel 打开中文不乱码。
// Flags 0x800 = 通用位 11，声明条目名为 UTF-8，否则 Windows 资源管理器
// 按 GBK 解中文文件名（「使用明细.csv」会乱码）。
func writeCSVWithBOM(zw *zip.Writer, name string, rows [][]string) error {
	w, err := zw.CreateHeader(&zip.FileHeader{
		Name:   name,
		Method: zip.Deflate,
		Flags:  0x800,
	})
	if err != nil {
		return err
	}
	if _, err = w.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		return err
	}
	cw := csv.NewWriter(w)
	if err = cw.WriteAll(rows); err != nil {
		return err
	}
	cw.Flush()
	return cw.Error()
}

func ExportUserLogs(c *gin.Context) {
	userId := c.GetInt("id")
	logType, _ := strconv.Atoi(c.Query("type"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	tokenName := c.Query("token_name")
	modelName := c.Query("model_name")
	group := c.Query("group")
	requestId := c.Query("request_id")

	logs, truncated, err := model.GetUserLogsForExport(userId, logType, startTimestamp, endTimestamp, modelName, tokenName, group, requestId)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	zipBytes, err := buildUserLogsExportZip(logs, truncated)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	filename := exportFilename(userId, startTimestamp, endTimestamp)
	c.Header("Content-Disposition", "attachment; filename=\""+filename+"\"")
	c.Header("Content-Type", "application/zip")
	c.Header("Content-Length", strconv.Itoa(len(zipBytes)))
	c.Data(http.StatusOK, "application/zip", zipBytes)
}

// exportFilename 生成导出 ZIP 的文件名，带用户 ID 与时间区间标签。
func exportFilename(userId int, startTimestamp, endTimestamp int64) string {
	rangeTag := time.Now().Format("20060102")
	if startTimestamp != 0 && endTimestamp != 0 {
		rangeTag = time.Unix(startTimestamp, 0).Format("20060102") + "-" + time.Unix(endTimestamp, 0).Format("20060102")
	}
	return fmt.Sprintf("usage-logs-%d-%s.zip", userId, rangeTag)
}

// buildUserLogsExportZip 把一批用户日志拼成报账 ZIP（明细 + 汇总两份 CSV）的字节。
// 同步下载（ExportUserLogs）与离线发邮件（ExportUserLogsAsync）共用，保证两条路径
// 输出格式完全一致。
func buildUserLogsExportZip(logs []*model.Log, truncated bool) ([]byte, error) {
	// 明细 sheet
	detail := [][]string{{
		"时间", "类型", "模型", "分组", "Token名称",
		"输入Tokens", "输出Tokens", "用时(秒)", "是否流式", "消费金额(元)", "RequestID",
	}}

	// 汇总按模型聚合
	type agg struct {
		count, prompt, completion, quota int
	}
	summary := make(map[string]*agg)
	var order []string
	var totalCount, totalPrompt, totalCompletion, totalQuota int

	for _, l := range logs {
		stream := "否"
		if l.IsStream {
			stream = "是"
		}
		detail = append(detail, []string{
			time.Unix(l.CreatedAt, 0).Format("2006-01-02 15:04:05"),
			logTypeLabelZh[l.Type],
			l.ModelName,
			l.Group,
			l.TokenName,
			strconv.Itoa(l.PromptTokens),
			strconv.Itoa(l.CompletionTokens),
			strconv.Itoa(l.UseTime),
			stream,
			quotaToYuan(l.Quota),
			l.RequestId,
		})

		key := l.ModelName
		if key == "" {
			key = "(无模型)"
		}
		a := summary[key]
		if a == nil {
			a = &agg{}
			summary[key] = a
			order = append(order, key)
		}
		a.count++
		a.prompt += l.PromptTokens
		a.completion += l.CompletionTokens
		a.quota += l.Quota

		totalCount++
		totalPrompt += l.PromptTokens
		totalCompletion += l.CompletionTokens
		totalQuota += l.Quota
	}

	summaryRows := [][]string{{
		"模型", "调用次数", "输入Tokens合计", "输出Tokens合计", "消费金额(元)合计",
	}}
	for _, k := range order {
		a := summary[k]
		summaryRows = append(summaryRows, []string{
			k,
			strconv.Itoa(a.count),
			strconv.Itoa(a.prompt),
			strconv.Itoa(a.completion),
			quotaToYuan(a.quota),
		})
	}
	summaryRows = append(summaryRows, []string{
		"总计",
		strconv.Itoa(totalCount),
		strconv.Itoa(totalPrompt),
		strconv.Itoa(totalCompletion),
		quotaToYuan(totalQuota),
	})
	if truncated {
		summaryRows = append(summaryRows, []string{
			fmt.Sprintf("注：导出已达单次上限 %d 条，仅含时间最早的部分，请缩小时间范围分批导出", len(logs)),
			"", "", "", "",
		})
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	if err := writeCSVWithBOM(zw, "使用明细.csv", detail); err != nil {
		return nil, err
	}
	if err := writeCSVWithBOM(zw, "消费汇总.csv", summaryRows); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
