package model

import (
	"context"
	"errors"
	"sync"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/internal/testdb"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupUsageRefundTest(t *testing.T) (User, Token, []Log) {
	t.Helper()
	db := useModelIntegrationTestDB(t, &User{}, &Token{}, &Log{}, &UsageRefundBatch{}, &UsageRefundReceipt{}, &UsageRefundNotice{}, &UsageRefundLogDelivery{})
	u := User{Username: "refund-user", Role: common.RoleCommonUser, Quota: 100}
	require.NoError(t, db.Create(&u).Error)
	token := Token{UserId: u.Id, Key: "refund-token", RemainQuota: 100, UsedQuota: 80}
	require.NoError(t, db.Create(&token).Error)
	logs := []Log{
		{UserId: u.Id, Username: u.Username, Type: LogTypeConsume, Quota: 30, TokenId: token.Id, RequestId: "request-a", CreatedAt: 100, Other: `{"stream_status":{"end_reason":"eof"}}`},
		{UserId: u.Id, Username: u.Username, Type: LogTypeConsume, Quota: 20, TokenId: token.Id, RequestId: "request-b", CreatedAt: 101},
		{UserId: u.Id, Username: u.Username, Type: LogTypeConsume, Quota: 5, TokenId: token.Id, RequestId: "request-c", CreatedAt: 102, Other: `{"stream_status":{"end_reason":"client_gone"}}`},
		{UserId: u.Id, Username: u.Username, Type: LogTypeConsume, Quota: 0, RequestId: "request-zero", CreatedAt: 103},
		{UserId: u.Id, Username: u.Username, Type: LogTypeConsume, Quota: 7, RequestId: "request-task", CreatedAt: 104, Other: `{"is_task":true}`},
	}
	require.NoError(t, db.Create(&logs).Error)
	return u, token, logs
}

func previewUsageRefundTest(t *testing.T, ids ...int) *UsageRefundView {
	t.Helper()
	v, err := PreviewUsageRefund(context.Background(), 99, common.RoleRootUser, UsageRefundSelection{LogIDs: ids, Reason: "test compensation"})
	require.NoError(t, err)
	return v
}

func TestUsageRefundSelectionAndIdempotency(t *testing.T) {
	u, token, logs := setupUsageRefundTest(t)
	v := previewUsageRefundTest(t, logs[0].Id, logs[1].Id, logs[2].Id, logs[3].Id, logs[4].Id, 999, logs[0].Id)
	require.Equal(t, 50, v.Quota)
	require.Equal(t, 2, v.Count)
	require.Len(t, v.Items, 6)
	require.Equal(t, "client_gone", v.Items[2].Status)
	require.Equal(t, "not_charged", v.Items[3].Status)
	require.Equal(t, "separate_billing", v.Items[4].Status)
	require.Equal(t, "missing", v.Items[5].Status)
	_, err := ExecuteUsageRefund(context.Background(), v.ID, 99, common.RoleRootUser, 51, 2)
	require.ErrorIs(t, err, ErrUsageRefundInvalid)
	for range 2 {
		got, err := ExecuteUsageRefund(context.Background(), v.ID, 99, common.RoleRootUser, 50, 2)
		require.NoError(t, err)
		require.Equal(t, "executed", got.Status)
		require.Len(t, got.Receipts, 2)
	}
	require.NoError(t, DB.First(&u, u.Id).Error)
	require.NoError(t, DB.First(&token, token.Id).Error)
	require.Equal(t, 150, u.Quota)
	require.Equal(t, 150, token.RemainQuota)
	require.Equal(t, 30, token.UsedQuota)
	v = previewUsageRefundTest(t, logs[0].Id)
	require.Zero(t, v.Count)
	require.Equal(t, "already_refunded", v.Items[0].Status)
}

func TestUsageRefundConcurrentBatches(t *testing.T) {
	u, _, logs := setupUsageRefundTest(t)
	a := previewUsageRefundTest(t, logs[0].Id, logs[1].Id)
	b := previewUsageRefundTest(t, logs[1].Id, logs[0].Id)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, v := range []*UsageRefundView{a, b} {
		wg.Add(1)
		go func(v *UsageRefundView) {
			defer wg.Done()
			_, err := ExecuteUsageRefund(context.Background(), v.ID, 99, common.RoleRootUser, v.Quota, v.Count)
			errs <- err
		}(v)
	}
	wg.Wait()
	close(errs)
	success := 0
	for err := range errs {
		if err == nil {
			success++
		} else {
			require.ErrorIs(t, err, ErrUsageRefundStale)
		}
	}
	require.Equal(t, 1, success)
	require.NoError(t, DB.First(&u, u.Id).Error)
	require.Equal(t, 150, u.Quota)
}

func TestUsageRefundRollbackAndStale(t *testing.T) {
	u, _, logs := setupUsageRefundTest(t)
	v := previewUsageRefundTest(t, logs[0].Id)
	require.NoError(t, DB.Exec("CREATE TRIGGER reject_refund BEFORE INSERT ON usage_refund_receipts FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'test rollback'").Error)
	_, err := ExecuteUsageRefund(context.Background(), v.ID, 99, common.RoleRootUser, v.Quota, v.Count)
	require.Error(t, err)
	require.NoError(t, DB.First(&u, u.Id).Error)
	require.Equal(t, 100, u.Quota)
	require.NoError(t, DB.Exec("DROP TRIGGER reject_refund").Error)
	require.NoError(t, LOG_DB.Model(&Log{}).Where("id = ?", logs[0].Id).Update("quota", 31).Error)
	_, err = ExecuteUsageRefund(context.Background(), v.ID, 99, common.RoleRootUser, v.Quota, v.Count)
	require.ErrorIs(t, err, ErrUsageRefundStale)
}

func TestUsageRefundPermissionsAndDeletedToken(t *testing.T) {
	u, token, logs := setupUsageRefundTest(t)
	_, err := PreviewUsageRefund(context.Background(), 99, common.RoleCommonUser, UsageRefundSelection{LogIDs: []int{logs[0].Id}, Reason: "test"})
	require.ErrorIs(t, err, ErrUsageRefundForbidden)
	v := previewUsageRefundTest(t, logs[0].Id)
	_, err = ExecuteUsageRefund(context.Background(), v.ID, 99, common.RoleAdminUser, v.Quota, v.Count)
	require.ErrorIs(t, err, ErrUsageRefundForbidden)
	_, err = GetUsageRefund(context.Background(), v.ID, 98)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	require.NoError(t, DB.Delete(&token).Error)
	got, err := ExecuteUsageRefund(context.Background(), v.ID, 99, common.RoleRootUser, v.Quota, v.Count)
	require.NoError(t, err)
	require.Zero(t, got.Receipts[0].TokenQuota)
	require.NoError(t, DB.Model(&u).Update("role", common.RoleRootUser).Error)
	v, err = PreviewUsageRefund(context.Background(), 98, common.RoleAdminUser, UsageRefundSelection{LogIDs: []int{logs[1].Id}, Reason: "test"})
	require.ErrorIs(t, err, ErrUsageRefundForbidden)
}

func TestUsageRefundMarksSourceLogsAndSkipsNextPreview(t *testing.T) {
	u, token, logs := setupUsageRefundTest(t)
	LOG_DB = testdb.OpenMySQL8(t, &Log{}, &UsageRefundLogDelivery{})
	require.NoError(t, LOG_DB.Create(&logs).Error)
	originalFingerprint := usageRefundFingerprint(logs[0])
	v := previewUsageRefundTest(t, logs[0].Id)
	_, err := ExecuteUsageRefund(context.Background(), v.ID, 99, common.RoleRootUser, v.Quota, v.Count)
	require.NoError(t, err)
	adminLogs, _, err := GetAllLogs(LogTypeConsume, 90, 110, "", "", "", 0, 100, 0, "", "", "", u.Id, NewLogSortOptions("", ""))
	require.NoError(t, err)
	marked := 0
	for _, log := range adminLogs {
		if log.Id == logs[0].Id {
			require.NotNil(t, log.UsageRefund)
			require.Equal(t, v.ID, log.UsageRefund.BatchID)
			require.Equal(t, 30, log.UsageRefund.Quota)
			require.Equal(t, originalFingerprint, usageRefundFingerprint(*log))
			marked++
		} else {
			require.Nil(t, log.UsageRefund)
		}
	}
	require.Equal(t, 1, marked)
	userLogs, _, err := GetUserLogs(u.Id, LogTypeConsume, 90, 110, "", "", 0, 100, "", logs[0].RequestId, "", NewLogSortOptions("", ""))
	require.NoError(t, err)
	require.Len(t, userLogs, 1)
	require.Equal(t, v.ID, userLogs[0].UsageRefund.BatchID)
	tokenLogs, err := GetLogByTokenId(token.Id)
	require.NoError(t, err)
	require.NotEmpty(t, tokenLogs)
	next := previewUsageRefundTest(t, logs[0].Id, logs[1].Id)
	require.Equal(t, 1, next.Count)
	require.Equal(t, 20, next.Quota)
	require.Equal(t, "already_refunded", next.Items[0].Status)
	require.Equal(t, v.ID, next.Items[0].RefundBatchID)
}

func TestUsageRefundSeparateLogDatabase(t *testing.T) {
	u, _, logs := setupUsageRefundTest(t)
	LOG_DB = testdb.OpenMySQL8(t, &Log{}, &UsageRefundLogDelivery{})
	require.NoError(t, LOG_DB.Create(&logs).Error)
	v := previewUsageRefundTest(t, logs[0].Id)
	_, err := ExecuteUsageRefund(context.Background(), v.ID, 99, common.RoleRootUser, v.Quota, v.Count)
	require.NoError(t, err)
	require.NoError(t, DB.First(&u, u.Id).Error)
	require.Equal(t, 130, u.Quota)
	var count int64
	require.NoError(t, DB.Model(&UsageRefundReceipt{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestUsageRefundValidation(t *testing.T) {
	for _, s := range []UsageRefundSelection{
		{}, {LogIDs: []int{1}, RequestIDs: []string{"a"}, Reason: "test"},
		{LogIDs: []int{-1}, Reason: "test"}, {Filter: &UsageRefundFilter{}, Reason: "test"},
		{Filter: &UsageRefundFilter{Start: 1, End: 32 * 86400}, Reason: "test"},
	} {
		require.True(t, errors.Is(validateUsageRefundSelection(s), ErrUsageRefundInvalid))
	}
}

func TestUsageRefundUserLogDeliveryRecoversWithoutDuplicateCredit(t *testing.T) {
	u, _, logs := setupUsageRefundTest(t)
	v := previewUsageRefundTest(t, logs[0].Id, logs[1].Id)
	require.NoError(t, LOG_DB.Exec("CREATE TRIGGER reject_refund_log BEFORE INSERT ON logs FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'log unavailable'").Error)
	got, err := ExecuteUsageRefund(context.Background(), v.ID, 99, common.RoleRootUser, v.Quota, v.Count)
	require.NoError(t, err)
	require.Equal(t, "executed", got.Status)
	require.EqualValues(t, 1, got.LogsPending)
	require.NoError(t, DB.First(&u, u.Id).Error)
	require.Equal(t, 150, u.Quota)
	require.NoError(t, LOG_DB.Exec("DROP TRIGGER reject_refund_log").Error)
	require.NoError(t, DeliverUsageRefundNotices(context.Background(), v.ID))
	// Simulate losing the primary DB acknowledgement after LOG_DB committed.
	require.NoError(t, DB.Model(&UsageRefundNotice{}).Where("batch_id = ?", v.ID).Update("delivered", false).Error)
	require.NoError(t, DeliverUsageRefundNotices(context.Background(), v.ID))
	var notices []Log
	require.NoError(t, LOG_DB.Where("type = ? AND user_id = ?", LogTypeRefund, u.Id).Find(&notices).Error)
	require.Len(t, notices, 1)
	require.Equal(t, 50, notices[0].Quota)
	require.Contains(t, notices[0].Content, "2 笔")
	require.Contains(t, notices[0].Other, v.ID)
	userLogs := []*Log{&notices[0]}
	formatUserLogs(userLogs, 0)
	require.NotContains(t, userLogs[0].Other, "admin_info")
	require.Contains(t, userLogs[0].Other, "test compensation")
	require.NoError(t, DB.First(&u, u.Id).Error)
	require.Equal(t, 150, u.Quota)
}

func TestUsageRefundFilterAndRequestSelection(t *testing.T) {
	_, _, logs := setupUsageRefundTest(t)
	max := 1
	v, err := PreviewUsageRefund(context.Background(), 99, common.RoleRootUser, UsageRefundSelection{Filter: &UsageRefundFilter{Start: 100, End: 104, MaxCompletionTokens: &max}, Reason: "filtered"})
	require.NoError(t, err)
	require.Equal(t, 2, v.Count)
	v, err = PreviewUsageRefund(context.Background(), 99, common.RoleRootUser, UsageRefundSelection{RequestIDs: []string{logs[0].RequestId, "missing-id", logs[0].RequestId}, Reason: "requests"})
	require.NoError(t, err)
	require.Equal(t, 1, v.Count)
	require.Len(t, v.Items, 2)
	v, err = PreviewUsageRefund(context.Background(), 99, common.RoleRootUser, UsageRefundSelection{LogIDs: []int{logs[2].Id}, IncludeClientGone: true, Reason: "approved exception"})
	require.NoError(t, err)
	require.Equal(t, 1, v.Count)
}

func TestUsageRefundFilteredScanCompletenessAndLimit(t *testing.T) {
	u, _, _ := setupUsageRefundTest(t)
	max := 1
	selection := UsageRefundSelection{Filter: &UsageRefundFilter{Start: 200, End: 300, Group: "1_vip_codex", MaxCompletionTokens: &max}, Reason: "scan"}
	// Deliberately use timestamps that disagree with ID order.
	logs := []Log{
		{Id: 30, UserId: u.Id, Type: LogTypeConsume, Group: "1_vip_codex", CreatedAt: 200, Quota: 10, CompletionTokens: 1},
		{Id: 20, UserId: u.Id, Type: LogTypeConsume, Group: "1_vip_codex", CreatedAt: 300, Quota: 20},
		{Id: 40, UserId: u.Id, Type: LogTypeConsume, Group: "1_vip_codex", CreatedAt: 250, Quota: 30, Other: `{"stream_status":{"end_reason":"client_gone"}}`},
		{Id: 50, UserId: u.Id, Type: LogTypeConsume, Group: "other", CreatedAt: 250, Quota: 10},
		{Id: 60, UserId: u.Id, Type: LogTypeConsume, Group: "1_vip_codex", CreatedAt: 199, Quota: 10},
		{Id: 70, UserId: u.Id, Type: LogTypeConsume, Group: "1_vip_codex", CreatedAt: 301, Quota: 10},
		{Id: 80, UserId: u.Id, Type: LogTypeConsume, Group: "1_vip_codex", CreatedAt: 250, Quota: 10, CompletionTokens: 2},
	}
	require.NoError(t, LOG_DB.Create(&logs).Error)
	v, err := PreviewUsageRefund(context.Background(), 99, common.RoleRootUser, selection)
	require.NoError(t, err)
	require.Len(t, v.Items, 3)
	require.Equal(t, []int{20, 30, 40}, []int{v.Items[0].LogID, v.Items[1].LogID, v.Items[2].LogID})
	require.Equal(t, 2, v.Count)
	require.Equal(t, 30, v.Quota)
	require.Equal(t, "client_gone", v.Items[2].Status)

	extra := make([]Log, MaxUsageRefundItems-3)
	for i := range extra {
		extra[i] = Log{UserId: u.Id, Type: LogTypeConsume, Group: "1_vip_codex", CreatedAt: 250, Quota: 1}
	}
	require.NoError(t, LOG_DB.CreateInBatches(extra, 200).Error)
	v, err = PreviewUsageRefund(context.Background(), 99, common.RoleRootUser, selection)
	require.NoError(t, err)
	require.Len(t, v.Items, MaxUsageRefundItems)
	require.NoError(t, LOG_DB.Create(&Log{UserId: u.Id, Type: LogTypeConsume, Group: "1_vip_codex", CreatedAt: 250, Quota: 1}).Error)
	v, err = PreviewUsageRefund(context.Background(), 99, common.RoleRootUser, selection)
	require.ErrorIs(t, err, ErrUsageRefundLimit)
	require.Nil(t, v)
	var batches, receipts int64
	require.NoError(t, DB.Model(&UsageRefundBatch{}).Count(&batches).Error)
	require.EqualValues(t, 2, batches)
	require.NoError(t, DB.Model(&UsageRefundReceipt{}).Count(&receipts).Error)
	require.Zero(t, receipts)
	require.NoError(t, DB.First(&u, u.Id).Error)
	require.Equal(t, 100, u.Quota)
}

func TestUsageRefundFilterExcludesGroups(t *testing.T) {
	u, _, _ := setupUsageRefundTest(t)
	logs := []Log{
		{UserId: u.Id, Type: LogTypeConsume, Group: "1_vip_codex", CreatedAt: 500, Quota: 10},
		{UserId: u.Id, Type: LogTypeConsume, Group: "0_Tools_Knowledge_Base", CreatedAt: 500, Quota: 20},
		{UserId: u.Id, Type: LogTypeConsume, Group: "1_vip_typesafe", CreatedAt: 500, Quota: 30},
	}
	require.NoError(t, LOG_DB.Create(&logs).Error)
	v, err := PreviewUsageRefund(context.Background(), 99, common.RoleRootUser, UsageRefundSelection{Filter: &UsageRefundFilter{Start: 500, End: 500, ExcludeGroups: []string{"0_Tools_Knowledge_Base", "1_vip_typesafe"}}, Reason: "exclude"})
	require.NoError(t, err)
	require.Len(t, v.Items, 1)
	require.Equal(t, logs[0].Id, v.Items[0].LogID)

	tooMany := make([]string, maxUsageRefundExcludeGroups+1)
	for i := range tooMany {
		tooMany[i] = "g"
	}
	for _, groups := range [][]string{{" "}, tooMany} {
		_, err = PreviewUsageRefund(context.Background(), 99, common.RoleRootUser, UsageRefundSelection{Filter: &UsageRefundFilter{Start: 500, End: 500, ExcludeGroups: groups}, Reason: "exclude"})
		require.ErrorIs(t, err, ErrUsageRefundInvalid)
	}
}

func TestUsageRefundExactUsernameDoesNotMatchSimilarUsers(t *testing.T) {
	u, token, _ := setupUsageRefundTest(t)
	other := User{Username: u.Username + "-2", Role: common.RoleCommonUser, Quota: 100, AffCode: "refund-other"}
	require.NoError(t, DB.Create(&other).Error)
	require.NoError(t, DB.Create(&Log{UserId: other.Id, Username: other.Username, Type: LogTypeConsume, Quota: 40, TokenId: token.Id, RequestId: "other-request", CreatedAt: 101}).Error)

	v, err := PreviewUsageRefund(context.Background(), 99, common.RoleRootUser, UsageRefundSelection{Filter: &UsageRefundFilter{Start: 100, End: 104, Username: u.Username}, Reason: "exact user"})
	require.NoError(t, err)
	require.Len(t, v.Users, 1)
	require.Equal(t, u.Id, v.Users[0].UserID)
}

func TestUsageRefundDeletedUserMakesPreviewStale(t *testing.T) {
	u, _, logs := setupUsageRefundTest(t)
	v := previewUsageRefundTest(t, logs[0].Id)
	require.NoError(t, DB.Delete(&User{}, u.Id).Error)
	_, err := ExecuteUsageRefund(context.Background(), v.ID, 99, common.RoleRootUser, v.Quota, v.Count)
	require.ErrorIs(t, err, ErrUsageRefundStale)
}
