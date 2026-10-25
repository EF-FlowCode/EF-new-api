package controller

import (
	"encoding/json"
	"io"
	"math"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/middleware"
	"eflow.local/new-api/model"
	"eflow.local/new-api/pkg/knowledge"
	relaycommon "eflow.local/new-api/relay/common"
	"eflow.local/new-api/relay/helper"
	"eflow.local/new-api/service"
	"eflow.local/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

func knowledgeError(c *gin.Context, status int, message string) {
	c.AbortWithStatusJSON(status, gin.H{"error": gin.H{"message": message, "type": "knowledge_batch_error"}})
}
func knowledgeUser(c *gin.Context) int { return common.GetContextKeyInt(c, constant.ContextKeyUserId) }

func UploadKnowledgeFile(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, knowledge.MaxInputBytes+1024*1024)
	if err := c.Request.ParseMultipartForm(knowledge.MaxInputBytes + 1024*1024); err != nil {
		knowledgeError(c, 400, "invalid batch file upload (maximum 8 MiB)")
		return
	}
	if c.Request.MultipartForm != nil {
		defer c.Request.MultipartForm.RemoveAll()
	}
	if c.PostForm("purpose") != "batch" {
		knowledgeError(c, 400, "only purpose=batch is supported")
		return
	}
	header, err := c.FormFile("file")
	if err != nil {
		knowledgeError(c, 400, "file is required")
		return
	}
	r, err := header.Open()
	if err != nil {
		knowledgeError(c, 400, "cannot read upload")
		return
	}
	defer r.Close()
	data, err := io.ReadAll(io.LimitReader(r, knowledge.MaxInputBytes+1))
	if err != nil {
		knowledgeError(c, 400, "cannot read upload")
		return
	}
	m, err := knowledge.ValidateBatch(data)
	if err != nil {
		knowledgeError(c, 400, err.Error())
		return
	}
	name := filepath.Base(header.Filename)
	if len(name) > 255 {
		knowledgeError(c, 400, "filename too long")
		return
	}
	f := model.KnowledgeFile{ID: "file_" + model.GenerateTaskID(), UserID: knowledgeUser(c), Object: "file", Purpose: "batch", Filename: name, Bytes: len(m.Canonical), CreatedAt: time.Now().Unix(), Model: m.Model, Contents: m.Canonical}
	if err = model.CreateKnowledgeFile(&f); err != nil {
		knowledgeError(c, 400, "cannot store batch file (maximum 100 retained input files per user)")
		return
	}
	c.JSON(200, f)
}

func ListKnowledgeFiles(c *gin.Context) {
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "100"))
	if err != nil || limit < 1 || limit > 100 {
		knowledgeError(c, 400, "limit must be between 1 and 100")
		return
	}
	var files []model.KnowledgeFile
	query := model.DB.Omit("contents").Where("user_id = ?", knowledgeUser(c))
	if purpose := c.Query("purpose"); purpose != "" {
		query = query.Where("purpose = ?", purpose)
	}
	if after := c.Query("after"); after != "" {
		f, err := model.GetKnowledgeFile(knowledgeUser(c), after)
		if err != nil {
			knowledgeError(c, 400, "invalid cursor")
			return
		}
		query = query.Where("created_at < ? OR (created_at = ? AND id < ?)", f.CreatedAt, f.CreatedAt, f.ID)
	}
	if err := query.Order("created_at DESC, id DESC").Limit(limit + 1).Find(&files).Error; err != nil {
		knowledgeError(c, 500, "cannot list files")
		return
	}
	more := len(files) > limit
	if more {
		files = files[:limit]
	}
	for i := range files {
		files[i].Object = "file"
	}
	first, last := "", ""
	if len(files) > 0 {
		first, last = files[0].ID, files[len(files)-1].ID
	}
	c.JSON(200, gin.H{"object": "list", "data": files, "has_more": more, "first_id": first, "last_id": last})
}

func RetrieveKnowledgeFile(c *gin.Context) {
	f, err := model.GetKnowledgeFile(knowledgeUser(c), c.Param("id"))
	if err != nil {
		knowledgeError(c, 404, "file not found")
		return
	}
	c.JSON(200, f)
}

func DeleteKnowledgeFile(c *gin.Context) {
	if err := model.DeleteKnowledgeFile(knowledgeUser(c), c.Param("id")); err != nil {
		knowledgeError(c, 409, "file unavailable or referenced by an active batch")
		return
	}
	c.JSON(200, gin.H{"id": c.Param("id"), "object": "file", "deleted": true})
}

func DownloadKnowledgeFile(c *gin.Context) {
	f, err := model.GetKnowledgeFile(knowledgeUser(c), c.Param("id"))
	if err != nil {
		knowledgeError(c, 404, "file not found")
		return
	}
	if f.UpstreamID == "" {
		c.Data(200, "application/jsonl", f.Contents)
		return
	}
	t, exists, err := model.GetByTaskId(knowledgeUser(c), f.TaskID)
	if err != nil || !exists || t.Platform != model.KnowledgeBatchPlatform {
		knowledgeError(c, 404, "file task not found")
		return
	}
	r, err := service.KnowledgeBatchFileContent(c.Request.Context(), t, f.UpstreamID)
	if err != nil {
		knowledgeError(c, 502, "cannot retrieve batch result")
		return
	}
	defer r.Close()
	c.Header("Content-Type", "application/jsonl")
	c.Status(200)
	_, _ = io.Copy(c.Writer, r)
}

type knowledgeBatchCreate struct {
	InputFileID      string `json:"input_file_id"`
	Endpoint         string `json:"endpoint"`
	CompletionWindow string `json:"completion_window"`
}

// Resolve the model from the owned file before normal distributor/model-limit
// checks. The client cannot choose a cheap model unrelated to file contents.
func PrepareKnowledgeBatch(c *gin.Context) {
	if !service.KnowledgeBatchTransportAvailable() {
		knowledgeError(c, 400, "Batch File requires direct outbound HTTP; configured Worker does not support multipart uploads")
		return
	}
	var request knowledgeBatchCreate
	if err := common.UnmarshalBodyReusable(c, &request); err != nil || request.Endpoint != "/v1/embeddings" || request.CompletionWindow != "24h" {
		knowledgeError(c, 400, "embedding Batch File requires endpoint=/v1/embeddings and completion_window=24h")
		return
	}
	f, err := model.GetKnowledgeFile(knowledgeUser(c), request.InputFileID)
	if err != nil || f.Purpose != "batch" {
		knowledgeError(c, 404, "batch input file not found")
		return
	}
	c.Set("knowledge_batch_model", f.Model)
	c.Set("knowledge_batch_input", f)
	c.Next()
}

func CreateKnowledgeBatch(c *gin.Context) {
	value, ok := c.Get("knowledge_batch_input")
	if !ok {
		knowledgeError(c, 400, "missing batch input")
		return
	}
	f := value.(*model.KnowledgeFile)
	info := relaycommon.GenRelayInfoOpenAI(c, nil)
	info.InitChannelMeta(c)
	if info.ChannelType != constant.ChannelTypeAli && info.ChannelType != constant.ChannelTypeOpenAI {
		knowledgeError(c, 400, "selected channel does not support native Bailian embedding Batch File")
		return
	}
	base, err := service.AliKnowledgeBatchBase(info.ChannelBaseUrl)
	if err != nil {
		knowledgeError(c, 400, err.Error())
		return
	}
	// Files deliberately carry the same model as the selected channel; reject
	// remappings until the JSONL and capability can be validated together.
	channel, err := model.GetChannelById(info.ChannelId, true)
	if err != nil {
		knowledgeError(c, 500, "channel unavailable")
		return
	}
	var mapping map[string]string
	_ = json.Unmarshal([]byte(channel.GetModelMapping()), &mapping)
	if mapped := mapping[f.Model]; mapped != "" && mapped != f.Model {
		knowledgeError(c, 400, "Batch File model remapping is not supported; use the upstream model name")
		return
	}
	if !model.ChannelSupportsKnowledgeBatch(channel, f.Model) {
		knowledgeError(c, 400, "selected channel does not support Batch File")
		return
	}
	if rpmErr := middleware.CheckUserModelRPM(c, f.Model); rpmErr != nil {
		knowledgeError(c, rpmErr.StatusCode, rpmErr.Error())
		return
	}
	if riskErr := service.RiskControlBeforeRelay(c, info); riskErr != nil {
		service.RecordRiskBlockedAccess(c, info, service.GetBlockingDecisionFromAudit(info.RiskAudit))
		knowledgeError(c, riskErr.StatusCode, riskErr.Error())
		return
	}
	batchRatio, ok := ratio_setting.GetBatchInputRatio(f.Model)
	if !ok {
		knowledgeError(c, 400, "configure an explicit Batch input price before enabling Batch File")
		return
	}
	m, err := knowledge.ValidateBatch(f.Contents)
	if err != nil {
		knowledgeError(c, 400, err.Error())
		return
	}
	ratio := helper.HandleGroupRatio(c, info).GroupRatio
	unit := common.GetQuotaPerUnit()
	batchInputPrice := batchRatio * 2
	reserved, err := common.QuotaRoundStrict(math.Ceil(float64(m.TextBytes+m.Count*256) * batchInputPrice / 1e6 * unit * ratio))
	if err != nil {
		knowledgeError(c, 400, "batch reservation exceeds quota limits")
		return
	}
	// The distributor only publishes metadata. Bind one enabled upstream key
	// before reserving funds so the worker can authenticate later.
	if keyErr := middleware.SetupContextForSelectedChannel(c, channel, f.Model); keyErr != nil {
		knowledgeError(c, keyErr.StatusCode, "selected channel has no available Batch credential")
		return
	}
	reservation := captureSchedulerReservation(c, channel, f.Model)
	defer reservation.release() // Admission ends here, not after the asynchronous job.
	info.InitChannelMeta(c)
	if strings.TrimSpace(info.ApiKey) == "" {
		knowledgeError(c, 503, "selected channel has no available Batch credential")
		return
	}
	now := time.Now().Unix()
	t := &model.Task{TaskID: "batch_" + model.GenerateTaskID(), Platform: model.KnowledgeBatchPlatform, UserId: info.UserId, Group: info.UsingGroup, ChannelId: info.ChannelId, Quota: reserved, Action: "embedding_batch", Status: model.TaskStatusNotStart, CreatedAt: now, UpdatedAt: now, SubmitTime: now, Progress: "0%", Properties: model.Properties{OriginModelName: f.Model, UpstreamModelName: f.Model}}
	t.PrivateData = model.TaskPrivateData{Key: info.ApiKey, TokenId: info.TokenId, BillingContext: &model.TaskBillingContext{OriginModelName: f.Model, GroupRatio: ratio}, KnowledgeBatch: &model.KnowledgeBatchState{InputFileID: f.ID, BaseURL: base, InputPrice: batchInputPrice, QuotaPerUnit: unit, GroupRatio: ratio, RequestCount: m.Count}}
	response := service.KnowledgeBatchResponse{ID: t.TaskID, Object: "batch", Endpoint: "/v1/embeddings", InputFileID: f.ID, CompletionWindow: "24h", Status: "validating", CreatedAt: now, BillingStatus: "reserved"}
	response.RequestCounts.Total = m.Count
	t.SetData(response)
	if err = model.CreateKnowledgeBatch(t); err != nil {
		knowledgeError(c, 403, "cannot reserve batch quota; check wallet/token balance and active batch limit (10)")
		return
	}
	model.UpdateUserUsedQuotaAndRequestCount(t.UserId, reserved)
	model.UpdateChannelUsedQuota(t.ChannelId, reserved)
	model.RecordTaskBillingLog(model.RecordTaskBillingLogParams{UserId: t.UserId, LogType: model.LogTypeConsume, Content: "Batch File 预扣", ChannelId: t.ChannelId, ModelName: f.Model, Quota: reserved, TokenId: info.TokenId, Group: t.Group, Other: map[string]any{"task_id": t.TaskID, "billing_mode": "batch_file", "pre_consumed_quota": reserved}})
	c.JSON(200, response)
}

func RetrieveKnowledgeBatch(c *gin.Context) {
	t, ok, err := model.GetByTaskId(knowledgeUser(c), c.Param("id"))
	if err != nil || !ok || t.Platform != model.KnowledgeBatchPlatform {
		knowledgeError(c, 404, "batch not found")
		return
	}
	c.Data(200, "application/json", t.Data)
}

func ListKnowledgeBatches(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if limit < 1 || limit > 100 {
		knowledgeError(c, 400, "limit must be between 1 and 100")
		return
	}
	query := model.DB.Where("user_id = ? AND platform = ?", knowledgeUser(c), model.KnowledgeBatchPlatform)
	if after := c.Query("after"); after != "" {
		t, ok, err := model.GetByTaskId(knowledgeUser(c), after)
		if err != nil || !ok || t.Platform != model.KnowledgeBatchPlatform {
			knowledgeError(c, 400, "invalid cursor")
			return
		}
		query = query.Where("id < ?", t.ID)
	}
	var tasks []model.Task
	if err := query.Order("id DESC").Limit(limit + 1).Find(&tasks).Error; err != nil {
		knowledgeError(c, 500, "cannot list batches")
		return
	}
	more := len(tasks) > limit
	if more {
		tasks = tasks[:limit]
	}
	data := make([]json.RawMessage, 0, len(tasks))
	first, last := "", ""
	for _, t := range tasks {
		data = append(data, t.Data)
		if first == "" {
			first = t.TaskID
		}
		last = t.TaskID
	}
	c.JSON(200, gin.H{"object": "list", "data": data, "has_more": more, "first_id": first, "last_id": last})
}

func CancelKnowledgeBatch(c *gin.Context) {
	t, ok, err := model.GetByTaskId(knowledgeUser(c), c.Param("id"))
	if err != nil || !ok || t.Platform != model.KnowledgeBatchPlatform {
		knowledgeError(c, 404, "batch not found")
		return
	}
	if t.Status == model.TaskStatusSuccess || t.Status == model.TaskStatusFailure {
		c.Data(200, "application/json", t.Data)
		return
	}
	won, err := model.ClaimKnowledgeBatch(t.ID)
	if err != nil || !won {
		knowledgeError(c, 409, "batch is updating; retry cancellation shortly")
		return
	}
	if err = model.DB.First(t, t.ID).Error; err != nil {
		knowledgeError(c, 500, "cannot load batch")
		return
	}
	defer model.ReleaseKnowledgeBatch(t)
	t.PrivateData.KnowledgeBatch.CancelRequested = true
	var response service.KnowledgeBatchResponse
	if err = t.GetData(&response); err != nil {
		knowledgeError(c, 500, "invalid stored batch")
		return
	}
	response.Status = "cancelling"
	t.SetData(response)
	if err = model.SaveKnowledgeBatch(t); err != nil {
		knowledgeError(c, 500, "cannot cancel batch")
		return
	}
	c.JSON(200, response)
}
