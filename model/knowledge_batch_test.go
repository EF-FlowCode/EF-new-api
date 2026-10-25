package model

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"github.com/stretchr/testify/require"
)

func TestKnowledgeBatchChannelSelection(t *testing.T) {
	base := "https://dashscope.aliyuncs.com"
	channel := &Channel{Type: constant.ChannelTypeAli, BaseURL: &base}
	require.True(t, ChannelSupportsRequestPath(channel, "/v1/batches", "text-embedding-v4"))
	require.False(t, ChannelSupportsRequestPath(channel, "/v1/batches", "qwen3-rerank"))
	base = "https://dashscope-intl.aliyuncs.com"
	require.False(t, ChannelSupportsRequestPath(channel, "/v1/batches", "text-embedding-v4"))
	base = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	channel.Type = constant.ChannelTypeOpenAI
	require.True(t, ChannelSupportsRequestPath(channel, "/v1/batches", "text-embedding-v4"))
	mapping := `{"text-embedding-v4":"text-embedding-v3"}`
	channel.ModelMapping = &mapping
	require.False(t, ChannelSupportsRequestPath(channel, "/v1/batches", "text-embedding-v4"))
}

func TestKnowledgeBatchReservationAndSettlementAtomicity(t *testing.T) {
	db := useModelIntegrationTestDB(t, &User{}, &Token{}, &Task{}, &KnowledgeFile{})
	u := User{Username: "kb", Quota: 1000}
	require.NoError(t, db.Create(&u).Error)
	token := Token{UserId: u.Id, Key: "kb", RemainQuota: 100, Status: common.TokenStatusEnabled}
	require.NoError(t, db.Create(&token).Error)
	f := KnowledgeFile{ID: "file_kb", UserID: u.Id, Purpose: "batch", Contents: []byte(`{"custom_id":"1","method":"POST","url":"/v1/embeddings","body":{"model":"text-embedding-v4","input":"text"}}`)}
	require.NoError(t, CreateKnowledgeFile(&f))
	task := &Task{TaskID: "batch_kb", UserId: u.Id, Platform: KnowledgeBatchPlatform, Status: TaskStatusNotStart, Quota: 200, PrivateData: TaskPrivateData{TokenId: token.Id, KnowledgeBatch: &KnowledgeBatchState{InputFileID: f.ID}}}
	require.Error(t, CreateKnowledgeBatch(task))
	require.NoError(t, db.First(&u, u.Id).Error)
	require.Equal(t, 1000, u.Quota)
	task.Quota = 80
	require.NoError(t, CreateKnowledgeBatch(task))
	require.NoError(t, db.First(&u, u.Id).Error)
	require.Equal(t, 920, u.Quota)
	require.NoError(t, db.First(&token, token.Id).Error)
	require.Equal(t, 20, token.RemainQuota)
	require.Error(t, DeleteKnowledgeFile(u.Id, f.ID))
	_, err := GetKnowledgeFile(u.Id+1, f.ID)
	require.Error(t, err)
	won, err := ClaimKnowledgeBatch(task.ID)
	require.NoError(t, err)
	require.True(t, won)
	require.NoError(t, db.First(task, task.ID).Error)
	task.PrivateData.KnowledgeBatch.OutputFileID = "remote_output"
	task.Status = TaskStatusSuccess
	task.Quota = 20
	task.FinishTime = time.Now().Unix()
	adjustment := BillingQuotaAdjustment{UserID: u.Id, TokenID: token.Id, Delta: -60}
	won, err = UpdateTaskWithBillingAtomic(task, TaskStatusNotStart, adjustment)
	require.NoError(t, err)
	require.True(t, won)
	won, err = UpdateTaskWithBillingAtomic(task, TaskStatusNotStart, adjustment)
	require.NoError(t, err)
	require.False(t, won)
	require.NoError(t, db.First(&u, u.Id).Error)
	require.Equal(t, 980, u.Quota)
	require.NoError(t, db.First(&token, token.Id).Error)
	require.Equal(t, 80, token.RemainQuota)
	_, err = GetKnowledgeFile(u.Id, "batch_kb_output")
	require.NoError(t, err)
}

func TestKnowledgeBatchConcurrentActiveLimit(t *testing.T) {
	db := useModelIntegrationTestDB(t, &User{}, &Token{}, &Task{}, &KnowledgeFile{})
	u := User{Username: "kb_limit", Quota: 1000}
	require.NoError(t, db.Create(&u).Error)
	token := Token{UserId: u.Id, Key: "kb_limit", RemainQuota: 1000}
	require.NoError(t, db.Create(&token).Error)
	var wg sync.WaitGroup
	errors := make(chan error, 20)
	for i := range 20 {
		f := KnowledgeFile{ID: fmt.Sprintf("f_%d", i), UserID: u.Id, Purpose: "batch", Contents: []byte(`{"custom_id":"1","method":"POST","url":"/v1/embeddings","body":{"model":"text-embedding-v4","input":"text"}}`)}
		require.NoError(t, CreateKnowledgeFile(&f))
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			errors <- CreateKnowledgeBatch(&Task{TaskID: fmt.Sprintf("b_%d", i), UserId: u.Id, Platform: KnowledgeBatchPlatform, Status: TaskStatusNotStart, Quota: 1, PrivateData: TaskPrivateData{TokenId: token.Id, KnowledgeBatch: &KnowledgeBatchState{InputFileID: id}}})
		}(i, f.ID)
	}
	wg.Wait()
	close(errors)
	successful := 0
	for err := range errors {
		if err == nil {
			successful++
		}
	}
	require.Equal(t, 10, successful)
	require.NoError(t, db.First(&u, u.Id).Error)
	require.Equal(t, 990, u.Quota)
}

func TestKnowledgeBatchAccessAllowsExhaustionButNotExpiry(t *testing.T) {
	db := useModelIntegrationTestDB(t, &Token{})
	token := Token{UserId: 1, Key: "kb_exhausted", RemainQuota: 0, Status: common.TokenStatusExhausted, ExpiredTime: -1}
	require.NoError(t, db.Create(&token).Error)
	_, err := ValidateTaskAccessToken(token.Key)
	require.NoError(t, err)
	_, err = ValidateUserToken(token.Key)
	require.Error(t, err)
	require.NoError(t, db.Model(&token).Update("expired_time", time.Now().Unix()-1).Error)
	_, err = ValidateTaskAccessToken(token.Key)
	require.Error(t, err)
}

// Settlement has no wallet floor: a batch whose final cost exceeds the balance
// still completes and leaves the wallet negative instead of retrying forever.
func TestKnowledgeBatchSettlementOverdrawsWallet(t *testing.T) {
	db := useModelIntegrationTestDB(t, &User{}, &Token{}, &Task{}, &KnowledgeFile{})
	u := User{Username: "kb_settle", Quota: 100}
	require.NoError(t, db.Create(&u).Error)
	token := Token{UserId: u.Id, Key: "kb_settle", RemainQuota: 1000}
	require.NoError(t, db.Create(&token).Error)
	f := &KnowledgeFile{ID: "settle_input", UserID: u.Id, Purpose: "batch", Contents: []byte(`{"custom_id":"1","method":"POST","url":"/v1/embeddings","body":{"model":"text-embedding-v4","input":"text"}}`)}
	require.NoError(t, CreateKnowledgeFile(f))
	task := &Task{TaskID: "settle", UserId: u.Id, Platform: KnowledgeBatchPlatform, Status: TaskStatusNotStart, Quota: 80, PrivateData: TaskPrivateData{TokenId: token.Id, KnowledgeBatch: &KnowledgeBatchState{InputFileID: f.ID}}}
	require.NoError(t, CreateKnowledgeBatch(task))
	won, err := ClaimKnowledgeBatch(task.ID)
	require.NoError(t, err)
	require.True(t, won)
	require.NoError(t, db.First(task, task.ID).Error)
	task.Status = TaskStatusSuccess
	task.PrivateData.KnowledgeBatch.OutputFileID = "remote_result"
	task.Quota = 200
	adjustment := BillingQuotaAdjustment{UserID: u.Id, TokenID: token.Id, Delta: 120}
	won, err = UpdateTaskWithBillingAtomic(task, TaskStatusNotStart, adjustment)
	require.NoError(t, err)
	require.True(t, won)
	var persisted Task
	require.NoError(t, db.First(&persisted, task.ID).Error)
	require.EqualValues(t, TaskStatusSuccess, persisted.Status)
	require.Equal(t, 200, persisted.Quota)
	require.NoError(t, db.First(&u, u.Id).Error)
	require.Equal(t, -100, u.Quota)
	require.NoError(t, db.First(&token, token.Id).Error)
	require.Equal(t, 800, token.RemainQuota)
	_, err = GetKnowledgeFile(u.Id, "settle_output")
	require.NoError(t, err)
}
