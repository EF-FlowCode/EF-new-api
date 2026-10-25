package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"eflow.local/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestKnowledgeBatchReconcilesIdentityWithoutResubmission(t *testing.T) {
	calls := 0
	client := knowledgeBatchClient{request: func(_ context.Context, _ *model.Task, method, path, _ string, _ []byte) (*http.Response, error) {
		require.Equal(t, "GET", method)
		calls++
		body := `{"data":[],"has_more":true,"last_id":"cursor"}`
		if calls == 2 {
			require.Contains(t, path, "after=cursor")
			body = `{"data":[{"id":"remote","metadata":{"new_api_task_id":"local"}}],"has_more":false}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	}}
	id, err := client.find(context.Background(), &model.Task{TaskID: "local"})
	require.NoError(t, err)
	require.Equal(t, "remote", id)
	require.Equal(t, 2, calls)
}

func TestKnowledgeBatchDefiniteRejection(t *testing.T) {
	for _, status := range []int{400, 401, 403, 404, 422, 429} {
		require.True(t, definiteKnowledgeRejection(&knowledgeProviderError{Status: status}))
	}
	for _, status := range []int{408, 500, 502, 503} {
		require.False(t, definiteKnowledgeRejection(&knowledgeProviderError{Status: status}))
	}
	require.False(t, definiteKnowledgeRejection(errors.New("timeout")))
}

func TestKnowledgeBatchPublicDataDoesNotExposeUpstreamIdentity(t *testing.T) {
	task := &model.Task{TaskID: "local", PrivateData: model.TaskPrivateData{KnowledgeBatch: &model.KnowledgeBatchState{InputFileID: "file_local"}}}
	response := publicBatch(task, KnowledgeBatchResponse{ID: "remote", InputFileID: "remote_file", Metadata: map[string]string{"private": "value"}})
	require.Equal(t, "local", response.ID)
	require.Equal(t, "file_local", response.InputFileID)
	require.Nil(t, response.Metadata)
}

func TestKnowledgeBatchPartialCancellationAndRestartSettlement(t *testing.T) {
	truncate(t)
	require.NoError(t, model.DB.AutoMigrate(&model.KnowledgeFile{}))
	seedUser(t, 1, 1000)
	seedToken(t, 1, 1, "kb_test", 1000)
	seedChannel(t, 1)
	input := []byte("{\"custom_id\":\"1\",\"method\":\"POST\",\"url\":\"/v1/embeddings\",\"body\":{\"model\":\"text-embedding-v4\",\"input\":\"one\"}}\n{\"custom_id\":\"2\",\"method\":\"POST\",\"url\":\"/v1/embeddings\",\"body\":{\"model\":\"text-embedding-v4\",\"input\":\"two\"}}")
	f := &model.KnowledgeFile{ID: "input", UserID: 1, Purpose: "batch", Contents: input}
	require.NoError(t, model.CreateKnowledgeFile(f))
	task := &model.Task{TaskID: "local", Platform: model.KnowledgeBatchPlatform, UserId: 1, ChannelId: 1, Quota: 100, Status: model.TaskStatusNotStart, SubmitTime: time.Now().Unix(), PrivateData: model.TaskPrivateData{TokenId: 1, KnowledgeBatch: &model.KnowledgeBatchState{InputFileID: f.ID, InputPrice: 1, QuotaPerUnit: 1e6, GroupRatio: 0.5, RequestCount: 2, SubmissionStarted: true, CancelRequested: true}}}
	require.NoError(t, model.CreateKnowledgeBatch(task))
	client := knowledgeBatchClient{request: func(_ context.Context, _ *model.Task, method, path, _ string, _ []byte) (*http.Response, error) {
		require.Equal(t, "GET", method) // A restart after uncertain submission must not POST again.
		body := ""
		switch path {
		case "/batches?limit=100":
			body = `{"data":[{"id":"remote","metadata":{"new_api_task_id":"local"}}]}`
		case "/batches/remote":
			body = `{"id":"remote","status":"cancelled","output_file_id":"out","request_counts":{"total":2,"completed":1,"failed":0}}`
		case "/files/out/content":
			body = `{"custom_id":"1","response":{"status_code":200,"body":{"usage":{"total_tokens":40},"data":[]}},"error":null}`
		default:
			t.Fatalf("unexpected path %s", path)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	}}
	require.NoError(t, client.poll(context.Background(), task))
	require.Equal(t, 980, getUserQuota(t, 1))
	require.Equal(t, 980, getTokenRemainQuota(t, 1))
	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	require.NoError(t, client.poll(context.Background(), &reloaded))
	require.Equal(t, 980, getUserQuota(t, 1))
	var response KnowledgeBatchResponse
	require.NoError(t, reloaded.GetData(&response))
	require.Equal(t, "cancelled", response.Status)
	require.Equal(t, "settled", response.BillingStatus)
	require.Equal(t, "local_output", *response.OutputFileID)
}

func TestKnowledgeBatchRejectedSubmissionRefundsWithoutReplay(t *testing.T) {
	truncate(t)
	require.NoError(t, model.DB.AutoMigrate(&model.KnowledgeFile{}))
	seedUser(t, 1, 1000)
	seedToken(t, 1, 1, "kb_rejected", 1000)
	seedChannel(t, 1)
	f := &model.KnowledgeFile{ID: "rejected_input", UserID: 1, Purpose: "batch", Contents: []byte(`{"custom_id":"1","method":"POST","url":"/v1/embeddings","body":{"model":"text-embedding-v4","input":"text"}}`)}
	require.NoError(t, model.CreateKnowledgeFile(f))
	task := &model.Task{TaskID: "rejected", Platform: model.KnowledgeBatchPlatform, UserId: 1, ChannelId: 1, Quota: 100, Status: model.TaskStatusNotStart, PrivateData: model.TaskPrivateData{TokenId: 1, KnowledgeBatch: &model.KnowledgeBatchState{InputFileID: f.ID, UpstreamFileID: "uploaded", InputPrice: 1, QuotaPerUnit: 1e6, GroupRatio: 1, RequestCount: 1}}}
	require.NoError(t, model.CreateKnowledgeBatch(task))
	calls := 0
	client := knowledgeBatchClient{request: func(_ context.Context, _ *model.Task, method, path, _ string, _ []byte) (*http.Response, error) {
		calls++
		require.Equal(t, "POST", method)
		require.Equal(t, "/batches", path)
		return &http.Response{StatusCode: 400, Body: io.NopCloser(strings.NewReader(`{"error":"bad file"}`))}, nil
	}}
	require.NoError(t, client.poll(context.Background(), task))
	require.Equal(t, 1000, getUserQuota(t, 1))
	require.NoError(t, model.DB.First(task, task.ID).Error)
	require.EqualValues(t, model.TaskStatusFailure, task.Status)
	require.NoError(t, client.poll(context.Background(), task))
	require.Equal(t, 1, calls)
	require.Equal(t, 1000, getTokenRemainQuota(t, 1))
}

func TestKnowledgeBatchUncertainSubmissionRetainsReservationAndRecovers(t *testing.T) {
	truncate(t)
	require.NoError(t, model.DB.AutoMigrate(&model.KnowledgeFile{}))
	seedUser(t, 1, 1000)
	seedToken(t, 1, 1, "kb_uncertain", 1000)
	seedChannel(t, 1)
	f := &model.KnowledgeFile{ID: "uncertain_input", UserID: 1, Purpose: "batch", Contents: []byte(`{"custom_id":"1","method":"POST","url":"/v1/embeddings","body":{"model":"text-embedding-v4","input":"text"}}`)}
	require.NoError(t, model.CreateKnowledgeFile(f))
	task := &model.Task{TaskID: "uncertain", Platform: model.KnowledgeBatchPlatform, UserId: 1, ChannelId: 1, Quota: 100, Status: model.TaskStatusNotStart, PrivateData: model.TaskPrivateData{TokenId: 1, KnowledgeBatch: &model.KnowledgeBatchState{InputFileID: f.ID, UpstreamFileID: "uploaded", InputPrice: 1, QuotaPerUnit: 1e6, GroupRatio: 1, RequestCount: 1}}}
	task.SetData(KnowledgeBatchResponse{ID: task.TaskID, Status: "validating", BillingStatus: "reserved"})
	require.NoError(t, model.CreateKnowledgeBatch(task))
	submissions, found := 0, false
	client := knowledgeBatchClient{request: func(_ context.Context, _ *model.Task, method, path, _ string, _ []byte) (*http.Response, error) {
		if method == "POST" {
			submissions++
			require.Equal(t, "/batches", path)
			return nil, context.DeadlineExceeded
		}
		body := `{"data":[],"has_more":false}`
		if found {
			if path == "/batches?limit=100" {
				body = `{"data":[{"id":"remote","metadata":{"new_api_task_id":"uncertain"}}]}`
			} else {
				require.Equal(t, "/batches/remote", path)
				body = `{"id":"remote","status":"failed","request_counts":{"total":0,"completed":0,"failed":0}}`
			}
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	}}
	require.ErrorIs(t, client.poll(context.Background(), task), context.DeadlineExceeded)
	for range 2 {
		require.NoError(t, model.DB.First(task, task.ID).Error)
		require.ErrorContains(t, client.poll(context.Background(), task), "acceptance unresolved")
		require.Equal(t, 900, getUserQuota(t, 1))
	}
	require.NoError(t, model.DB.First(task, task.ID).Error)
	var response KnowledgeBatchResponse
	require.NoError(t, task.GetData(&response))
	require.Equal(t, "submission_unresolved", response.BillingStatus)
	found = true
	require.NoError(t, client.poll(context.Background(), task))
	require.Equal(t, 1, submissions)
	require.Equal(t, 1000, getUserQuota(t, 1))
	require.Equal(t, 1000, getTokenRemainQuota(t, 1))
}
