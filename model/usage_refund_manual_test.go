package model

import (
	"context"
	"errors"
	"strings"
	"testing"

	"eflow.local/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestUsageRefundPartialAmountsCreditOnlyTheChosenQuota(t *testing.T) {
	u, token, logs := setupUsageRefundTest(t)
	v, err := PreviewUsageRefund(context.Background(), 99, common.RoleRootUser, UsageRefundSelection{
		LogIDs:  []int{logs[0].Id, logs[1].Id},
		Amounts: map[int]int{logs[0].Id: 12},
		Reason:  "manual compensation",
	})
	require.NoError(t, err)
	require.Equal(t, 32, v.Quota)
	require.Equal(t, 2, v.Count)
	require.Equal(t, 12, v.Items[0].Quota)
	require.Equal(t, 30, v.Items[0].OriginalQuota)
	require.Equal(t, 20, v.Items[1].Quota)

	// The execute confirmation must match the partial total, not the original charge.
	_, err = ExecuteUsageRefund(context.Background(), v.ID, 99, common.RoleRootUser, 50, 2)
	require.ErrorIs(t, err, ErrUsageRefundInvalid)
	got, err := ExecuteUsageRefund(context.Background(), v.ID, 99, common.RoleRootUser, 32, 2)
	require.NoError(t, err)
	require.Equal(t, "executed", got.Status)
	receipts := map[int]UsageRefundReceipt{}
	for _, receipt := range got.Receipts {
		receipts[receipt.SourceLogID] = receipt
	}
	require.Equal(t, 12, receipts[logs[0].Id].Quota)
	require.Equal(t, 12, receipts[logs[0].Id].TokenQuota)
	require.Equal(t, 20, receipts[logs[1].Id].Quota)

	require.NoError(t, DB.First(&u, u.Id).Error)
	require.NoError(t, DB.First(&token, token.Id).Error)
	require.Equal(t, 132, u.Quota)
	require.Equal(t, 132, token.RemainQuota)
	require.Equal(t, 48, token.UsedQuota)

	var notice UsageRefundNotice
	require.NoError(t, DB.Where("batch_id = ?", v.ID).First(&notice).Error)
	var entry Log
	require.NoError(t, common.Unmarshal([]byte(notice.Payload), &entry))
	require.Equal(t, 32, entry.Quota)

	// A partially returned log is still refunded once; the rest cannot be claimed later.
	again, err := PreviewUsageRefund(context.Background(), 99, common.RoleRootUser, UsageRefundSelection{LogIDs: []int{logs[0].Id}, Reason: "again"})
	require.NoError(t, err)
	require.Zero(t, again.Count)
	require.Equal(t, "already_refunded", again.Items[0].Status)
}

func TestUsageRefundPartialAmountValidation(t *testing.T) {
	_, _, logs := setupUsageRefundTest(t)
	for _, s := range []UsageRefundSelection{
		{LogIDs: []int{logs[0].Id}, Amounts: map[int]int{logs[0].Id: 0}, Reason: "test"},
		{LogIDs: []int{logs[0].Id}, Amounts: map[int]int{logs[0].Id: -5}, Reason: "test"},
		{LogIDs: []int{logs[0].Id}, Amounts: map[int]int{logs[1].Id: 5}, Reason: "test"},
		{RequestIDs: []string{"request-a"}, Amounts: map[int]int{logs[0].Id: 5}, Reason: "test"},
		{Filter: &UsageRefundFilter{Start: 1, End: 200}, Amounts: map[int]int{logs[0].Id: 5}, Reason: "test"},
	} {
		require.True(t, errors.Is(validateUsageRefundSelection(s), ErrUsageRefundInvalid), "%+v", s)
	}
	_, err := PreviewUsageRefund(context.Background(), 99, common.RoleRootUser, UsageRefundSelection{
		LogIDs: []int{logs[0].Id}, Amounts: map[int]int{logs[0].Id: 31}, Reason: "too much",
	})
	require.ErrorIs(t, err, ErrUsageRefundInvalid)
	full, err := PreviewUsageRefund(context.Background(), 99, common.RoleRootUser, UsageRefundSelection{
		LogIDs: []int{logs[0].Id}, Amounts: map[int]int{logs[0].Id: 30}, Reason: "exact",
	})
	require.NoError(t, err)
	require.Equal(t, 30, full.Quota)
}

func TestUsageRefundCandidatesListOneUsersCharges(t *testing.T) {
	u, _, logs := setupUsageRefundTest(t)
	other := User{Username: "refund-other", Role: common.RoleCommonUser, AffCode: "refund-other"}
	require.NoError(t, DB.Create(&other).Error)
	require.NoError(t, LOG_DB.Create(&Log{UserId: other.Id, Type: LogTypeConsume, Quota: 9, CreatedAt: 100}).Error)
	v := previewUsageRefundTest(t, logs[1].Id)
	_, err := ExecuteUsageRefund(context.Background(), v.ID, 99, common.RoleRootUser, v.Quota, v.Count)
	require.NoError(t, err)

	got, err := ListUsageRefundCandidates(context.Background(), 99, common.RoleRootUser, UsageRefundCandidateQuery{Username: u.Username, Start: 90, End: 110})
	require.NoError(t, err)
	require.Equal(t, u.Id, got.UserID)
	status := map[int]UsageRefundCandidate{}
	for _, item := range got.Items {
		status[item.LogID] = item
	}
	// Zero-charge logs are not candidates; other users' logs never appear.
	require.Len(t, got.Items, 4)
	require.Equal(t, "eligible", status[logs[0].Id].Status)
	require.Equal(t, "already_refunded", status[logs[1].Id].Status)
	require.Equal(t, v.ID, status[logs[1].Id].RefundBatchID)
	require.Equal(t, 20, status[logs[1].Id].RefundedQuota)
	require.Equal(t, "client_gone", status[logs[2].Id].Status)
	require.Equal(t, "separate_billing", status[logs[4].Id].Status)

	byID, err := ListUsageRefundCandidates(context.Background(), 99, common.RoleRootUser, UsageRefundCandidateQuery{UserID: u.Id, Start: 90, End: 110, IncludeClientGone: true})
	require.NoError(t, err)
	require.Len(t, byID.Items, 4)
	for _, item := range byID.Items {
		if item.LogID == logs[2].Id {
			require.Equal(t, "eligible", item.Status)
		}
	}

	for _, q := range []UsageRefundCandidateQuery{
		{Start: 90, End: 110},
		{UserID: u.Id, Username: u.Username, Start: 90, End: 110},
		{UserID: u.Id, Start: 110, End: 90},
		{UserID: u.Id, Start: 1, End: 32 * 86400},
		{Username: "nobody", Start: 90, End: 110},
	} {
		_, err := ListUsageRefundCandidates(context.Background(), 99, common.RoleRootUser, q)
		require.ErrorIs(t, err, ErrUsageRefundInvalid, "%+v", q)
	}
	_, err = ListUsageRefundCandidates(context.Background(), 99, common.RoleAdminUser, UsageRefundCandidateQuery{UserID: u.Id, Start: 90, End: 110})
	require.ErrorIs(t, err, ErrUsageRefundForbidden)
}

func TestUsageRefundCandidatesByPastedRequestIDs(t *testing.T) {
	u, _, logs := setupUsageRefundTest(t)
	got, err := ListUsageRefundCandidates(context.Background(), 99, common.RoleRootUser, UsageRefundCandidateQuery{
		UserID: u.Id, Start: 90, End: 110,
		RequestID:  "request-a",
		RequestIDs: []string{" request-b ", "request-a", "", "request-zero", "request-unknown"},
	})
	require.NoError(t, err)
	ids := []int{}
	for _, item := range got.Items {
		ids = append(ids, item.LogID)
	}
	require.ElementsMatch(t, []int{logs[0].Id, logs[1].Id}, ids)
	// Zero-charge and unknown IDs are reported back instead of silently dropped.
	require.Equal(t, []string{"request-zero", "request-unknown"}, got.MissingRequestIDs)

	tooLong := strings.Repeat("x", 129)
	_, err = ListUsageRefundCandidates(context.Background(), 99, common.RoleRootUser, UsageRefundCandidateQuery{UserID: u.Id, Start: 90, End: 110, RequestIDs: []string{tooLong}})
	require.ErrorIs(t, err, ErrUsageRefundInvalid)
}
