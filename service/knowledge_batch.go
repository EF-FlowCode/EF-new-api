package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/model"
	"eflow.local/new-api/pkg/knowledge"
)

type KnowledgeBatchResponse struct {
	ID               string  `json:"id"`
	Object           string  `json:"object"`
	Endpoint         string  `json:"endpoint"`
	InputFileID      string  `json:"input_file_id"`
	CompletionWindow string  `json:"completion_window"`
	Status           string  `json:"status"`
	CreatedAt        int64   `json:"created_at"`
	OutputFileID     *string `json:"output_file_id"`
	ErrorFileID      *string `json:"error_file_id"`
	RequestCounts    struct {
		Total     int `json:"total"`
		Completed int `json:"completed"`
		Failed    int `json:"failed"`
	} `json:"request_counts"`
	Metadata      map[string]string `json:"metadata,omitempty"`
	BillingStatus string            `json:"billing_status,omitempty"`
}

type knowledgeProviderError struct{ Status int }

func (e *knowledgeProviderError) Error() string {
	return fmt.Sprintf("batch provider HTTP %d", e.Status)
}
func definiteKnowledgeRejection(err error) bool {
	var e *knowledgeProviderError
	return errors.As(err, &e) && e.Status >= 400 && e.Status < 500 && e.Status != 408
}

func KnowledgeBatchTransportAvailable() bool {
	_, throughWorker := configuredOutboundHTTPAdapter()
	return !throughWorker // Existing Worker body contract accepts JSON, not multipart.
}

func AliKnowledgeBatchBase(raw string) (string, error) {
	return knowledge.AliBatchBase(raw)
}

func knowledgeBatchRequest(ctx context.Context, t *model.Task, method, path, contentType string, body []byte) (*http.Response, error) {
	state := t.PrivateData.KnowledgeBatch
	base, err := AliKnowledgeBatchBase(state.BaseURL)
	if err != nil {
		return nil, err
	}
	adapter, _ := configuredOutboundHTTPAdapter()
	if strings.HasPrefix(contentType, "multipart/") && !KnowledgeBatchTransportAvailable() {
		return nil, fmt.Errorf("configured Worker does not support Batch multipart uploads")
	}
	return doOutboundHTTPRequest(ctx, adapter, outboundRequest{URL: base + path, Method: method, Headers: map[string]string{"Authorization": "Bearer " + t.PrivateData.Key, "Content-Type": contentType}, Body: body}, downloadOutboundPolicy())
}

type knowledgeBatchClient struct {
	request func(context.Context, *model.Task, string, string, string, []byte) (*http.Response, error)
}

func (client knowledgeBatchClient) json(ctx context.Context, t *model.Task, method, path string, body any, out any) error {
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	r, err := client.request(ctx, t, method, path, "application/json", data)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	if r.StatusCode < 200 || r.StatusCode >= 300 {
		return &knowledgeProviderError{Status: r.StatusCode}
	}
	return json.NewDecoder(io.LimitReader(r.Body, 4*1024*1024)).Decode(out)
}

func (client knowledgeBatchClient) upload(ctx context.Context, t *model.Task, input *model.KnowledgeFile) error {
	// Always send the canonical form (files stored before strict validation may
	// still hold the raw upload), so the provider runs exactly what was priced.
	manifest, err := knowledge.ValidateBatch(input.Contents)
	if err != nil {
		return err
	}
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	f, err := w.CreateFormFile("file", "requests.jsonl")
	if err != nil {
		return err
	}
	if _, err = f.Write(manifest.Canonical); err != nil {
		return err
	}
	if err = w.WriteField("purpose", "batch"); err != nil {
		return err
	}
	if err = w.Close(); err != nil {
		return err
	}
	r, err := client.request(ctx, t, http.MethodPost, "/files", w.FormDataContentType(), body.Bytes())
	if err != nil {
		return err
	}
	defer r.Body.Close()
	if r.StatusCode < 200 || r.StatusCode >= 300 {
		return &knowledgeProviderError{Status: r.StatusCode}
	}
	var remote struct {
		ID string `json:"id"`
	}
	if err = json.NewDecoder(io.LimitReader(r.Body, 1024*1024)).Decode(&remote); err != nil {
		return err
	}
	if remote.ID == "" {
		return fmt.Errorf("batch upload returned no file id")
	}
	t.PrivateData.KnowledgeBatch.UpstreamFileID = remote.ID
	return model.SaveKnowledgeBatch(t)
}

func publicBatch(t *model.Task, remote KnowledgeBatchResponse) KnowledgeBatchResponse {
	remote.ID = t.TaskID
	remote.Object = "batch"
	remote.Endpoint = "/v1/embeddings"
	remote.InputFileID = t.PrivateData.KnowledgeBatch.InputFileID
	remote.CompletionWindow = "24h"
	remote.CreatedAt = t.SubmitTime
	remote.Metadata = nil
	if remote.BillingStatus == "" {
		remote.BillingStatus = "reserved"
	}
	return remote
}

// Reconcile an uncertain submission by its persisted identity. Never blindly
// resubmit after a timeout/crash: the provider may already be charging the job.
func (client knowledgeBatchClient) find(ctx context.Context, t *model.Task) (string, error) {
	after := ""
	for range 100 {
		var list struct {
			Data    []KnowledgeBatchResponse `json:"data"`
			HasMore bool                     `json:"has_more"`
			LastID  string                   `json:"last_id"`
		}
		path := "/batches?limit=100"
		if after != "" {
			path += "&after=" + url.QueryEscape(after)
		}
		if err := client.json(ctx, t, "GET", path, nil, &list); err != nil {
			return "", err
		}
		for _, b := range list.Data {
			if b.Metadata["new_api_task_id"] == t.TaskID {
				return b.ID, nil
			}
		}
		if !list.HasMore {
			return "", nil
		}
		if list.LastID == "" || list.LastID == after {
			return "", fmt.Errorf("invalid batch pagination")
		}
		after = list.LastID
	}
	return "", fmt.Errorf("batch reconciliation exceeded page limit")
}

func PollKnowledgeBatch(ctx context.Context, t *model.Task) error {
	return (knowledgeBatchClient{request: knowledgeBatchRequest}).poll(ctx, t)
}

func (client knowledgeBatchClient) poll(ctx context.Context, t *model.Task) error {
	won, err := model.ClaimKnowledgeBatch(t.ID)
	if err != nil || !won {
		return err
	}
	// Reload after acquiring ownership, so cancellation and another worker's
	// saved upstream identity cannot be overwritten by a stale task snapshot.
	if err = model.DB.First(t, t.ID).Error; err != nil {
		return err
	}
	defer model.ReleaseKnowledgeBatch(t)
	s := t.PrivateData.KnowledgeBatch
	if s == nil {
		return fmt.Errorf("missing batch state")
	}
	if s.SubmissionError != "" {
		return finishKnowledgeBatch(ctx, t, KnowledgeBatchResponse{Status: "failed"}, 0)
	}
	input, err := model.GetKnowledgeFile(t.UserId, s.InputFileID)
	if err != nil {
		return err
	}
	if t.PrivateData.UpstreamTaskID == "" {
		if !s.SubmissionStarted && t.SubmitTime > 0 && time.Now().Unix()-t.SubmitTime > 86400 {
			return finishKnowledgeBatch(ctx, t, KnowledgeBatchResponse{Status: "expired"}, 0)
		}
		if s.CancelRequested && !s.SubmissionStarted {
			return finishKnowledgeBatch(ctx, t, KnowledgeBatchResponse{Status: "cancelled"}, 0)
		}
		if s.SubmissionStarted {
			id, err := client.find(ctx, t)
			if err != nil {
				return err
			}
			if id == "" {
				var pending KnowledgeBatchResponse
				if err := t.GetData(&pending); err != nil {
					return err
				}
				pending.BillingStatus = "submission_unresolved"
				t.SetData(pending)
				if err := model.SaveKnowledgeBatch(t); err != nil {
					return err
				}
				return fmt.Errorf("batch submission acceptance unresolved; reservation retained")
			}
			t.PrivateData.UpstreamTaskID = id
			if err = model.SaveKnowledgeBatch(t); err != nil {
				return err
			}
		} else {
			if s.UpstreamFileID == "" {
				if err = client.upload(ctx, t, input); err != nil {
					if definiteKnowledgeRejection(err) {
						return rejectKnowledgeBatch(ctx, t, err)
					}
					return err
				}
			}
			s.SubmissionStarted = true
			if err = model.SaveKnowledgeBatch(t); err != nil {
				return err
			}
			var created KnowledgeBatchResponse
			body := map[string]any{"input_file_id": s.UpstreamFileID, "endpoint": "/v1/embeddings", "completion_window": "24h", "metadata": map[string]string{"new_api_task_id": t.TaskID}}
			if err = client.json(ctx, t, "POST", "/batches", body, &created); err != nil {
				if definiteKnowledgeRejection(err) {
					return rejectKnowledgeBatch(ctx, t, err)
				}
				return err
			}
			if created.ID == "" {
				return fmt.Errorf("batch provider returned no task id")
			}
			t.PrivateData.UpstreamTaskID = created.ID
			if err = model.SaveKnowledgeBatch(t); err != nil {
				return err
			}
		}
	}
	id := url.PathEscape(t.PrivateData.UpstreamTaskID)
	var remote KnowledgeBatchResponse
	if err = client.json(ctx, t, "GET", "/batches/"+id, nil, &remote); err != nil {
		return err
	}
	if remote.ID != t.PrivateData.UpstreamTaskID || !validKnowledgeBatchStatus(remote.Status) {
		return fmt.Errorf("invalid upstream batch identity/status")
	}
	terminal := remote.Status == "completed" || remote.Status == "failed" || remote.Status == "expired" || remote.Status == "cancelled"
	if s.CancelRequested && !terminal && remote.Status != "cancelling" {
		if err = client.json(ctx, t, "POST", "/batches/"+id+"/cancel", map[string]any{}, &remote); err != nil {
			return err
		}
	}
	if !terminal {
		remote.OutputFileID = nil
		remote.ErrorFileID = nil
		t.SetData(publicBatch(t, remote))
		return model.SaveKnowledgeBatch(t)
	}
	manifest, err := knowledge.ValidateBatch(input.Contents)
	if err != nil {
		return err
	}
	tokens, completed := 0, 0
	if remote.OutputFileID != nil {
		r, err := client.fileContent(ctx, t, *remote.OutputFileID)
		if err != nil {
			return err
		}
		tokens, completed, err = knowledge.ReadBatchUsage(r, manifest.IDs)
		r.Close()
		if err != nil {
			return err
		}
	}
	// Providers may reject a file before counting its rows.
	validationFailure := remote.Status == "failed" && remote.RequestCounts.Total == 0 && remote.RequestCounts.Completed == 0 && remote.OutputFileID == nil
	if completed != remote.RequestCounts.Completed || (!validationFailure && remote.RequestCounts.Total != manifest.Count) {
		return fmt.Errorf("batch result count mismatch; settlement deferred")
	}
	return finishKnowledgeBatch(ctx, t, remote, tokens)
}

func KnowledgeBatchFileContent(ctx context.Context, t *model.Task, id string) (io.ReadCloser, error) {
	return (knowledgeBatchClient{request: knowledgeBatchRequest}).fileContent(ctx, t, id)
}

func (client knowledgeBatchClient) fileContent(ctx context.Context, t *model.Task, id string) (io.ReadCloser, error) {
	r, err := client.request(ctx, t, "GET", "/files/"+url.PathEscape(id)+"/content", "application/json", nil)
	if err != nil {
		return nil, err
	}
	if r.StatusCode != http.StatusOK {
		r.Body.Close()
		return nil, fmt.Errorf("batch result HTTP %d", r.StatusCode)
	}
	return r.Body, nil
}

func finishKnowledgeBatch(ctx context.Context, t *model.Task, remote KnowledgeBatchResponse, tokens int) error {
	s := t.PrivateData.KnowledgeBatch
	s.BilledInputTokens = tokens
	quota, err := common.QuotaRoundStrict(float64(tokens) * s.InputPrice / 1e6 * s.QuotaPerUnit * s.GroupRatio)
	if err != nil {
		return err
	}
	for _, entry := range []struct {
		ID     **string
		Suffix string
		Stored *string
	}{{&remote.OutputFileID, "output", &s.OutputFileID}, {&remote.ErrorFileID, "error", &s.ErrorFileID}} {
		if *entry.ID == nil {
			continue
		}
		*entry.Stored = **entry.ID
		localID := t.TaskID + "_" + entry.Suffix
		*entry.ID = &localID
	}
	previous := t.Status
	t.Status = model.TaskStatusSuccess
	if remote.Status != "completed" {
		t.Status = model.TaskStatusFailure
	}
	t.Progress = "100%"
	t.FinishTime = time.Now().Unix()
	remote.BillingStatus = "settled"
	if remote.RequestCounts.Total == 0 {
		remote.RequestCounts.Total = s.RequestCount
	}
	t.SetData(publicBatch(t, remote))
	won, err := FinalizeTaskWithBilling(ctx, t, previous, quota, "Batch File 按成功请求输入 Token 结算")
	if err == nil && won {
		model.RecordTaskBillingLog(model.RecordTaskBillingLogParams{UserId: t.UserId, LogType: model.LogTypeSystem, Content: fmt.Sprintf("Batch File: %d input tokens; final quota %d", tokens, quota), ModelName: t.Properties.OriginModelName, TokenId: t.PrivateData.TokenId, Group: t.Group, ChannelId: t.ChannelId, Other: map[string]any{"task_id": t.TaskID, "billing_mode": "batch_file", "input_tokens": tokens, "input_price": s.InputPrice, "group_ratio": s.GroupRatio, "actual_quota": quota}})
	}
	return err
}

func PollKnowledgeBatches(ctx context.Context) {
	var tasks []*model.Task
	if err := model.DB.Where("platform = ? AND status NOT IN ?", model.KnowledgeBatchPlatform, []model.TaskStatus{model.TaskStatusSuccess, model.TaskStatusFailure}).Order("updated_at ASC, id ASC").Limit(20).Find(&tasks).Error; err != nil {
		common.SysError("knowledge batch query failed")
		return
	}
	var wg sync.WaitGroup
	defer wg.Wait()
	slots := make(chan struct{}, 4)
	for _, t := range tasks {
		if ctx.Err() != nil {
			return
		}
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			return
		}
		wg.Add(1)
		go func(t *model.Task) {
			defer wg.Done()
			defer func() { <-slots }()
			jobCtx, cancel := context.WithTimeout(ctx, 240*time.Second)
			if err := PollKnowledgeBatch(jobCtx, t); err != nil {
				common.SysError(fmt.Sprintf("knowledge batch %s: %v", t.TaskID, err))
			}
			cancel()
		}(t)
	}
}

func validKnowledgeBatchStatus(status string) bool {
	switch status {
	case "validating", "in_progress", "finalizing", "completed", "failed", "expired", "cancelling", "cancelled":
		return true
	}
	return false
}

func rejectKnowledgeBatch(ctx context.Context, t *model.Task, err error) error {
	t.PrivateData.KnowledgeBatch.SubmissionError = err.Error()
	if err := model.SaveKnowledgeBatch(t); err != nil {
		return err
	}
	return finishKnowledgeBatch(ctx, t, KnowledgeBatchResponse{Status: "failed"}, 0)
}

func KnowledgeBatchPollingLoop(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 15 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			PollKnowledgeBatches(ctx)
		}
	}
}
