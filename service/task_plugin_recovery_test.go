package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/model"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
	management "eflow.local/new-api/service/taskplugin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func recoveryTask(t *testing.T, phase string) *model.Task {
	t.Helper()
	oldLimit := constant.TaskQueryLimit
	constant.TaskQueryLimit = 100
	t.Cleanup(func() { constant.TaskQueryLimit = oldLimit })
	task, _ := completionTask(t, "wan2.2-s2v")
	require.NoError(t, model.DB.AutoMigrate(&model.TaskPluginReview{}, &model.TaskPluginSubmission{}))
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", task.UserId).Update("role", common.RoleRootUser).Error)
	task.Status, task.Revision = model.TaskStatusSubmitUnknown, 1
	if phase == "prepared" {
		task.Status = model.TaskStatusSubmitting
	}
	task.PrivateData.UpstreamTaskID = ""
	require.NoError(t, model.DB.Save(task).Error)
	require.NoError(t, model.DB.Create(&model.TaskPluginSubmission{
		TaskID: task.ID, ScopeKey: fmt.Sprintf("%064d", task.ID), Fingerprint: fmt.Sprintf("%064d", 1),
		Phase: phase, Deadline: time.Now().Add(-time.Minute).Unix(), ParseInput: `{}`, Response: `invalid`,
	}).Error)
	return task
}

func TestPluginRecoveryReviewMySQL(t *testing.T) {
	for _, mode := range []string{"refund", "accept_race", "parse_failure", "unsent", "sending", "paused", "channel_paused", "duplicate"} {
		t.Run(mode, func(t *testing.T) {
			phase := "response"
			if mode == "unsent" {
				phase = "prepared"
			}
			if mode == "sending" {
				phase = "sending"
			}
			task := recoveryTask(t, phase)
			before := getUserQuota(t, task.UserId)
			if mode == "unsent" || mode == "sending" {
				recoverPluginSubmissions(t.Context())
				recoverPluginSubmissions(t.Context())
				if mode == "unsent" {
					require.EqualValues(t, model.TaskStatusFailure, reloadCompletion(t, task.ID).Status)
					require.Equal(t, before+task.Quota, getUserQuota(t, task.UserId))
					require.Error(t, model.ClaimPluginSubmission(t.Context(), task.ID))
					require.EqualValues(t, 1, countLogs(t))
				} else {
					require.ErrorIs(t, model.RejectUnsentPluginSubmission(t.Context(), task.ID), model.ErrPluginSubmissionConflict)
					require.Equal(t, before, getUserQuota(t, task.UserId))
				}
				return
			}
			if mode == "parse_failure" {
				sub, err := model.GetPluginSubmission(t.Context(), task.ID)
				require.NoError(t, err)
				recoverPluginSubmissions(t.Context())
				after, err := model.GetPluginSubmission(t.Context(), task.ID)
				require.NoError(t, err)
				require.Equal(t, sub.Deadline, after.Deadline, "parsing must not renew the manual review lock")
				require.Greater(t, after.RetryAfter, time.Now().Unix())
			}
			if mode == "paused" || mode == "channel_paused" {
				if mode == "paused" {
					require.NoError(t, model.DB.Model(&model.TaskPluginControl{}).Where("id = 1").Update("enabled", false).Error)
				} else {
					require.NoError(t, model.DB.Model(&model.TaskPluginProductionBinding{}).Where("channel_id = ?", task.ChannelId).Update("enabled", false).Error)
				}
				_, _, err := management.Default.StoredSubmitHandle(t.Context(), task.ID)
				require.ErrorIs(t, err, model.ErrTaskPluginDisabled)
				_, err = model.AcceptPluginSubmission(t.Context(), task.ID, "accepted", []byte(`{}`), nil)
				require.ErrorIs(t, err, model.ErrTaskPluginDisabled)
				require.EqualValues(t, model.TaskStatusSubmitUnknown, reloadCompletion(t, task.ID).Status)
				return
			}
			if mode == "duplicate" {
				other, err := model.ClonePluginTask(task)
				require.NoError(t, err)
				other.ID, other.TaskID = 0, "other-task"
				other.Status, other.PrivateData.UpstreamTaskID = model.TaskStatusSubmitted, "accepted"
				require.NoError(t, model.DB.Create(other).Error)
				_, err = model.AcceptPluginSubmission(t.Context(), task.ID, "accepted", []byte(`{}`), nil)
				require.ErrorIs(t, err, model.ErrPluginSubmissionConflict)
				return
			}
			req := PluginReviewRequest{Revision: task.Revision, Action: "refund", Reason: "provider checked", Evidence: "confirmed non-acceptance", Confirmed: true}
			review, err := PreviewPluginTaskReview(t.Context(), management.Default, task.UserId, task.ID, req)
			require.NoError(t, err)
			var wg sync.WaitGroup
			errs := make(chan error, 7)
			for range 6 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					_, err := ConfirmPluginTaskReview(t.Context(), task.UserId, review.OperationID)
					errs <- err
				}()
			}
			if mode == "accept_race" {
				wg.Add(1)
				go func() {
					defer wg.Done()
					_, err := model.AcceptPluginSubmission(t.Context(), task.ID, "accepted", []byte(`{}`), nil)
					errs <- err
				}()
			}
			wg.Wait()
			close(errs)
			for err := range errs {
				if mode == "accept_race" && errors.Is(err, model.ErrPluginSubmissionConflict) {
					continue
				}
				require.NoError(t, err)
			}
			saved := reloadCompletion(t, task.ID)
			if saved.Status == model.TaskStatusFailure {
				require.Zero(t, saved.Quota)
				require.Equal(t, before+task.Quota, getUserQuota(t, task.UserId))
				require.EqualValues(t, 1, countLogs(t))
			} else {
				require.EqualValues(t, model.TaskStatusSubmitted, saved.Status)
				require.Equal(t, before, getUserQuota(t, task.UserId))
				require.Zero(t, countLogs(t))
			}
			require.Equal(t, getUserQuota(t, task.UserId), getTokenRemainQuota(t, task.PrivateData.TokenId))
			// A worker holding the old revision cannot overwrite the review winner.
			task.Status = model.TaskStatusFailure
			won, err := task.UpdateWithStatus(model.TaskStatusSubmitUnknown)
			require.NoError(t, err)
			require.False(t, won)
		})
	}
}

func TestPluginRecoveryProbePinnedIdentityMySQL(t *testing.T) {
	task := recoveryTask(t, "sending")
	for _, mode := range []string{"valid", "unknown", "not_found"} {
		t.Run(mode, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "GET", r.Method)
				require.Equal(t, "/api/v1/tasks/candidate", r.URL.Path)
				require.Equal(t, "Bearer bound-key", r.Header.Get("Authorization"))
				if mode == "not_found" {
					w.WriteHeader(404)
					return
				}
				id, status := "candidate", "RUNNING"
				if mode == "unknown" {
					status = "UNRECOGNIZED"
				}
				_, _ = io.WriteString(w, fmt.Sprintf(`{"output":{"task_id":%q,"task_status":%q}}`, id, status))
			}))
			defer srv.Close()
			err := management.Default.ProbeRecovery(t.Context(), task.ID, "candidate", management.TaskQueryEnvironment{BaseURL: srv.URL}, host.Executor{Client: srv.Client(), ValidateURL: func(string) error { return nil }})
			if mode == "valid" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			require.Empty(t, reloadCompletion(t, task.ID).PrivateData.UpstreamTaskID)
		})
	}
}

func TestPluginRecoveryProbeRejectsReturnedIDMySQL(t *testing.T) {
	task, _ := setupAllPluginPoll(t, "sunoapi", "")
	for _, id := range []string{"candidate", "wrong-id"} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "Bearer bound-key", r.Header.Get("Authorization"))
			_, _ = io.WriteString(w, fmt.Sprintf(`{"code":"success","data":[{"task_id":%q,"status":"IN_PROGRESS"}]}`, id))
		}))
		err := management.Default.ProbeRecovery(t.Context(), task.ID, "candidate", management.TaskQueryEnvironment{BaseURL: srv.URL}, host.Executor{Client: srv.Client(), ValidateURL: func(string) error { return nil }})
		srv.Close()
		if id == "candidate" {
			require.NoError(t, err)
		} else {
			require.Error(t, err)
		}
	}
}

func TestPluginReviewHistoricalPriceAndInvalidFactsMySQL(t *testing.T) {
	task, _ := completionTask(t, "wan2.2-s2v")
	require.NoError(t, model.DB.AutoMigrate(&model.TaskPluginReview{}))
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", task.UserId).Update("role", common.RoleRootUser).Error)
	task.Status = model.TaskStatusBillingPending
	task.PrivateData.PluginContext.Billing = nil
	task.PrivateData.PluginContext.CompletionUsage = map[string]any{"seconds": float64(-1), "resolution": "480P"}
	require.NoError(t, model.DB.Save(task).Error)
	seconds := 3.0
	price := &model.TaskPluginBilling{Version: 1, Mode: "duration", BaseQuota: 100, ResolutionRatios: map[string]float64{"480P": 1}}
	review, err := PreviewPluginTaskReview(t.Context(), management.Default, task.UserId, task.ID, PluginReviewRequest{Revision: task.Revision, Action: "settle", Seconds: &seconds, Billing: price, Reason: "historical invoice", Evidence: "duration and unit price verified", Confirmed: true})
	require.NoError(t, err)
	require.Nil(t, reloadCompletion(t, task.ID).PrivateData.PluginContext.Billing)
	_, err = ConfirmPluginTaskReview(t.Context(), task.UserId, review.OperationID)
	require.NoError(t, err)
	saved := reloadCompletion(t, task.ID)
	require.Equal(t, 300, saved.Quota)
	raw, err := json.Marshal(saved.PrivateData.PluginContext)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"seconds":3`)
	_, err = PreviewPluginTaskReview(t.Context(), management.Default, task.UserId, task.ID, PluginReviewRequest{Revision: saved.Revision, Action: "refund", Reason: "refund", Evidence: "bad request", Confirmed: true})
	require.ErrorIs(t, err, model.ErrPluginSubmissionConflict)
}

func TestPluginBillingCheckpointRevisionRollbackMySQL(t *testing.T) {
	task, _ := completionTask(t, "wan2.2-s2v")
	before := task.Revision
	task.Status = model.TaskStatusBillingPending
	task.PrivateData.PluginContext.BillingRetryAfter = time.Now().Add(time.Minute).Unix()
	require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register("fail_checkpoint", func(tx *gorm.DB) {
		if tx.Statement.Table == "tasks" {
			tx.AddError(errors.New("injected"))
		}
	}))
	won, err := model.StorePluginBillingPending(t.Context(), task, model.TaskStatusInProgress)
	require.Error(t, err)
	require.False(t, won)
	require.Equal(t, before, task.Revision)
}
