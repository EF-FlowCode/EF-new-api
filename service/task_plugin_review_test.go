package service

import (
	"errors"
	"sync"
	"testing"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/model"
	management "eflow.local/new-api/service/taskplugin"
	"eflow.local/new-api/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestPluginReviewSettlementMySQL(t *testing.T) {
	for _, mode := range []string{"concurrent", "stale", "wallet_failure", "audit_failure", "expired", "not_root", "immutable"} {
		t.Run(mode, func(t *testing.T) {
			task, _ := completionTask(t, "wan2.2-s2v")
			require.NoError(t, model.DB.AutoMigrate(&model.TaskPluginReview{}, &model.TaskPluginSubmission{}))
			require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", task.UserId).Updates(map[string]any{"role": common.RoleRootUser, "status": common.UserStatusEnabled}).Error)
			task.Status = model.TaskStatusBillingPending
			task.PrivateData.PluginContext.BillingPendingReason = "missing_completion_usage"
			require.NoError(t, model.DB.Save(task).Error)
			seconds := 2.0
			req := PluginReviewRequest{Revision: task.Revision, Action: "settle", Seconds: &seconds, Resolution: "480P", Reason: "provider invoice", Evidence: "confirmed duration in provider console", Confirmed: true}
			review, err := PreviewPluginTaskReview(t.Context(), management.Default, task.UserId, task.ID, req)
			require.NoError(t, err)
			persisted := reloadCompletion(t, task.ID)
			require.Nil(t, persisted.PrivateData.PluginContext.CompletionUsage, "preview must not mutate durable facts")
			var userBefore model.User
			var tokenBefore model.Token
			require.NoError(t, model.DB.First(&userBefore, task.UserId).Error)
			require.NoError(t, model.DB.First(&tokenBefore, task.PrivateData.TokenId).Error)
			switch mode {
			case "stale":
				require.NoError(t, model.DB.Model(task).UpdateColumn("revision", gorm.Expr("revision + 1")).Error)
			case "wallet_failure":
				require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register("fail_wallet", func(tx *gorm.DB) {
					if tx.Statement.Table == "users" {
						tx.AddError(errors.New("injected wallet failure"))
					}
				}))
			case "audit_failure":
				require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register("fail_audit", func(tx *gorm.DB) {
					if tx.Statement.Table == "task_plugin_reviews" {
						tx.AddError(errors.New("injected audit failure"))
					}
				}))
			case "expired":
				require.NoError(t, model.DB.Model(&review).UpdateColumn("expires", time.Now().Add(-time.Minute).Unix()).Error)
			case "not_root":
				require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", task.UserId).UpdateColumn("role", common.RoleAdminUser).Error)
			case "immutable":
				pc := task.PrivateData.PluginContext
				pc.CompletionReady = true
				pc.CompletionUsage = map[string]any{"seconds": float64(3), "resolution": "480P"}
				require.NoError(t, model.DB.Save(task).Error)
			}
			if mode == "concurrent" {
				var wg sync.WaitGroup
				errs := make(chan error, 8)
				for range 8 {
					wg.Add(1)
					go func() {
						defer wg.Done()
						_, err := ConfirmPluginTaskReview(t.Context(), task.UserId, review.OperationID)
						errs <- err
					}()
				}
				wg.Wait()
				close(errs)
				for err := range errs {
					require.NoError(t, err)
				}
				saved := reloadCompletion(t, task.ID)
				require.EqualValues(t, model.TaskStatusSuccess, saved.Status)
				require.Equal(t, 200, saved.Quota)
				var user model.User
				var token model.Token
				require.NoError(t, model.DB.First(&user, userBefore.Id).Error)
				require.NoError(t, model.DB.First(&token, tokenBefore.Id).Error)
				require.Equal(t, userBefore.Quota+task.Quota-200, user.Quota)
				require.Equal(t, tokenBefore.RemainQuota+task.Quota-200, token.RemainQuota)
			} else {
				_, err := ConfirmPluginTaskReview(t.Context(), task.UserId, review.OperationID)
				require.Error(t, err)
				var user model.User
				var token model.Token
				require.NoError(t, model.DB.First(&user, userBefore.Id).Error)
				require.NoError(t, model.DB.First(&token, tokenBefore.Id).Error)
				require.Equal(t, userBefore.Quota, user.Quota)
				require.Equal(t, tokenBefore.RemainQuota, token.RemainQuota)
				require.EqualValues(t, model.TaskStatusBillingPending, reloadCompletion(t, task.ID).Status)
			}
		})
	}
}

func TestPluginTokenPriceReviewMySQL(t *testing.T) {
	task, _ := setupAllPluginPoll(t, "doubao", "")
	require.NoError(t, model.DB.AutoMigrate(&model.TaskPluginReview{}, &model.TaskPluginSubmission{}))
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", task.UserId).Update("role", common.RoleRootUser).Error)
	snapshot, err := NewTaskPluginTokenBilling(types.PriceData{ModelRatio: 0.5, TaskModelRatioConfigured: true, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 0.5}}, false)
	require.NoError(t, err)
	tokens := 1000
	task.PrivateData.PluginContext = &model.TaskPluginContext{TokenBilling: snapshot, CompletionTokens: &tokens, BillingPendingReason: "settlement_pending", BillingRetryAfter: time.Now().Unix()}
	task.Status = model.TaskStatusBillingPending
	require.NoError(t, model.DB.Save(task).Error)
	detail, err := PluginTaskReviewDetail(t.Context(), task.ID)
	require.NoError(t, err)
	require.NotNil(t, detail.TokenBilling)
	require.Equal(t, tokens, *detail.CompletionTokens)
	require.False(t, detail.EditSeconds)
	require.False(t, detail.EditResolution)
	req := PluginReviewRequest{Revision: task.Revision, Action: "settle", Reason: "verify", Evidence: "stored submission and completion", Confirmed: true}
	for _, field := range []string{"price", "seconds", "resolution"} {
		bad := req
		switch field {
		case "price":
			bad.Billing = &model.TaskPluginBilling{Version: 1, Mode: "fixed", BaseQuota: 1}
		case "seconds":
			n := 1.0
			bad.Seconds = &n
		case "resolution":
			bad.Resolution = "720P"
		}
		_, err = PreviewPluginTaskReview(t.Context(), management.Default, task.UserId, task.ID, bad)
		require.Error(t, err)
	}
	review, err := PreviewPluginTaskReview(t.Context(), management.Default, task.UserId, task.ID, req)
	require.NoError(t, err)
	require.Equal(t, 250, review.AfterQuota)
	for range 2 {
		_, err = ConfirmPluginTaskReview(t.Context(), task.UserId, review.OperationID)
		require.NoError(t, err)
	}
	require.Equal(t, 1150, getUserQuota(t, 40))
	require.Equal(t, 1150, getTokenRemainQuota(t, 40))
	require.Equal(t, snapshot, reloadCompletion(t, task.ID).PrivateData.PluginContext.TokenBilling)
}

// An implausible or overflowing token report must not strand the reservation:
// it fails with an explicit reason and review can settle or refund it once.
func TestPluginTokenOverflowReviewMySQL(t *testing.T) {
	for _, action := range []string{"manual_quota", "refund"} {
		t.Run(action, func(t *testing.T) {
			task, _ := setupAllPluginPoll(t, "doubao", "")
			require.NoError(t, model.DB.AutoMigrate(&model.TaskPluginReview{}, &model.TaskPluginSubmission{}))
			require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", task.UserId).Update("role", common.RoleRootUser).Error)
			snapshot, err := NewTaskPluginTokenBilling(types.PriceData{ModelRatio: 1000, TaskModelRatioConfigured: true, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}}, false)
			require.NoError(t, err)
			for tokens, reason := range map[int]string{maxTaskPluginCompletionTokens + 1: "completion_tokens_exceed_limit", 50_000_000: "completion_quota_overflow", 1000: ""} {
				probe := *task
				probe.PrivateData.PluginContext = &model.TaskPluginContext{TokenBilling: snapshot, CompletionTokens: &tokens}
				_, got := pluginCompletionQuota(&probe)
				require.Equal(t, reason, got, "tokens=%d", tokens)
			}
			tokens := maxTaskPluginCompletionTokens + 1
			task.PrivateData.PluginContext = &model.TaskPluginContext{TokenBilling: snapshot, CompletionTokens: &tokens, BillingPendingReason: "completion_tokens_exceed_limit", BillingRetryAfter: time.Now().Unix()}
			task.Status = model.TaskStatusBillingPending
			require.NoError(t, model.DB.Save(task).Error)
			require.Error(t, settlePluginCompletion(t.Context(), task))
			detail, err := PluginTaskReviewDetail(t.Context(), task.ID)
			require.NoError(t, err)
			require.True(t, detail.ManualQuota)
			req := PluginReviewRequest{Revision: task.Revision, Action: "settle", Reason: "provider invoice", Evidence: "provider console usage", Confirmed: true}
			_, err = PreviewPluginTaskReview(t.Context(), management.Default, task.UserId, task.ID, req)
			require.Error(t, err, "recorded facts still cannot produce a quota")
			for _, bad := range []int{-1, task.Quota + 1} {
				q := bad
				req.Quota = &q
				_, err = PreviewPluginTaskReview(t.Context(), management.Default, task.UserId, task.ID, req)
				require.Error(t, err)
			}
			want := 0
			if action == "manual_quota" {
				q := 100
				req.Quota = &q
				want = 100
			} else {
				req.Action, req.Quota = "refund", nil
			}
			review, err := PreviewPluginTaskReview(t.Context(), management.Default, task.UserId, task.ID, req)
			require.NoError(t, err)
			require.Equal(t, want, review.AfterQuota)
			for range 2 {
				_, err = ConfirmPluginTaskReview(t.Context(), task.UserId, review.OperationID)
				require.NoError(t, err)
			}
			saved := reloadCompletion(t, task.ID)
			require.Equal(t, want, saved.Quota)
			if action == "refund" {
				require.EqualValues(t, model.TaskStatusFailure, saved.Status)
			} else {
				require.EqualValues(t, model.TaskStatusSuccess, saved.Status)
			}
			require.Equal(t, 1000+task.Quota-want, getUserQuota(t, 40))
			require.Equal(t, 1000+task.Quota-want, getTokenRemainQuota(t, 40))
			// The reservation is released exactly once: neither another review
			// nor the automatic settlement path can move the task again.
			req.Revision = saved.Revision
			_, err = PreviewPluginTaskReview(t.Context(), management.Default, task.UserId, task.ID, req)
			require.Error(t, err)
			require.Error(t, settlePluginCompletion(t.Context(), saved))
			require.Equal(t, 1000+task.Quota-want, getUserQuota(t, 40))
		})
	}
}
