package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	"eflow.local/new-api/common"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	MaxUsageRefundItems         = 5000
	maxUsageRefundExcludeGroups = 50
)

var (
	ErrUsageRefundInvalid   = errors.New("invalid refund selection, reason, or confirmation")
	ErrUsageRefundLimit     = errors.New("refund selection exceeds 5000 records; narrow the selection")
	ErrUsageRefundForbidden = errors.New("refund operation is not permitted")
	ErrUsageRefundStale     = errors.New("refund preview changed or expired; create a new preview")
)

// Batches and receipts live in DB, even when consumption logs live in LOG_DB.
// SourceLogID is permanently unique, independent of batch identity or log retention.
type UsageRefundBatch struct {
	ID         string `json:"id" gorm:"type:varchar(36);primaryKey"`
	AdminID    int    `json:"admin_id" gorm:"index"`
	Reason     string `json:"reason" gorm:"type:varchar(500)"`
	Status     string `json:"status" gorm:"type:varchar(16)"`
	Quota      int    `json:"quota"`
	Count      int    `json:"count"`
	CreatedAt  int64  `json:"created_at"`
	ExecutedAt int64  `json:"executed_at"`
	Snapshot   string `json:"-" gorm:"type:longtext"`
}

type UsageRefundReceipt struct {
	SourceLogID int    `json:"source_log_id" gorm:"primaryKey;autoIncrement:false"`
	BatchID     string `json:"batch_id" gorm:"type:varchar(36);index"`
	UserID      int    `json:"user_id" gorm:"index"`
	TokenID     int    `json:"token_id"`
	Quota       int    `json:"quota"`
	TokenQuota  int    `json:"token_quota"`
	RequestID   string `json:"request_id" gorm:"type:varchar(64)"`
	CreatedAt   int64  `json:"created_at"`
}

type UsageRefundFilter struct {
	Start               int64  `json:"start_timestamp"`
	End                 int64  `json:"end_timestamp"`
	Group               string `json:"group"`
	Channel             int    `json:"channel"`
	UserID              int    `json:"user_id"`
	Username            string `json:"username"`
	ModelName           string `json:"model_name"`
	TokenName           string `json:"token_name"`
	RequestID           string `json:"request_id"`
	UpstreamRequestID   string `json:"upstream_request_id"`
	Type                int    `json:"type"`
	MaxCompletionTokens *int   `json:"max_completion_tokens,omitempty"`
	// ExcludeGroups drops whole groups from the scan, e.g. embedding/rerank
	// groups whose logs never carry output tokens and would fill the item cap.
	ExcludeGroups []string `json:"exclude_groups,omitempty"`
}

type UsageRefundSelection struct {
	LogIDs             []int              `json:"log_ids,omitempty"`
	RequestIDs         []string           `json:"request_ids,omitempty"`
	UpstreamRequestIDs []string           `json:"upstream_request_ids,omitempty"`
	Filter             *UsageRefundFilter `json:"filter,omitempty"`
	IncludeClientGone  bool               `json:"include_client_gone"`
	Reason             string             `json:"reason"`
	// Amounts overrides the returned quota per log (log_ids mode only);
	// unlisted logs return their full original quota.
	Amounts map[int]int `json:"amounts,omitempty"`
}

type UsageRefundItem struct {
	LogID         int    `json:"log_id"`
	RequestID     string `json:"request_id"`
	UserID        int    `json:"user_id"`
	Username      string `json:"username"`
	TokenID       int    `json:"token_id"`
	Quota         int    `json:"quota"`
	OriginalQuota int    `json:"original_quota,omitempty"`
	CreatedAt     int64  `json:"created_at"`
	ModelName     string `json:"model_name"`
	Channel       int    `json:"channel"`
	Status        string `json:"status"`
	Fingerprint   string `json:"fingerprint"`
	RefundBatchID string `json:"refund_batch_id,omitempty"`
}

type UsageRefundUserSummary struct {
	UserID   int    `json:"user_id"`
	Username string `json:"username"`
	Quota    int    `json:"quota"`
	Count    int    `json:"count"`
}

type UsageRefundView struct {
	UsageRefundBatch
	Items       []UsageRefundItem        `json:"items"`
	Users       []UsageRefundUserSummary `json:"users"`
	Receipts    []UsageRefundReceipt     `json:"receipts"`
	LogsPending int64                    `json:"logs_pending"`
}

func validateUsageRefundSelection(s UsageRefundSelection) error {
	modes := 0
	for _, n := range []int{len(s.LogIDs), len(s.RequestIDs), len(s.UpstreamRequestIDs)} {
		if n > MaxUsageRefundItems {
			return ErrUsageRefundLimit
		}
		if n > 0 {
			modes++
		}
	}
	if s.Filter != nil {
		modes++
		f := s.Filter
		if f.Start <= 0 || f.End < f.Start || f.End-f.Start > 31*86400 || f.Channel < 0 || f.UserID < 0 || (f.Type != 0 && f.Type != LogTypeConsume) || (f.MaxCompletionTokens != nil && *f.MaxCompletionTokens < 0) || len(f.ExcludeGroups) > maxUsageRefundExcludeGroups {
			return ErrUsageRefundInvalid
		}
		for _, group := range f.ExcludeGroups {
			if strings.TrimSpace(group) == "" || len(group) > 64 {
				return ErrUsageRefundInvalid
			}
		}
	}
	if modes != 1 || len(strings.TrimSpace(s.Reason)) == 0 || len(s.Reason) > 500 {
		return ErrUsageRefundInvalid
	}
	selectedLogs := make(map[int]bool, len(s.LogIDs))
	for _, id := range s.LogIDs {
		if id <= 0 {
			return ErrUsageRefundInvalid
		}
		selectedLogs[id] = true
	}
	for id, amount := range s.Amounts {
		if !selectedLogs[id] || amount < 1 {
			return ErrUsageRefundInvalid
		}
	}
	for _, ids := range [][]string{s.RequestIDs, s.UpstreamRequestIDs} {
		for _, id := range ids {
			if strings.TrimSpace(id) == "" || len(id) > 128 {
				return ErrUsageRefundInvalid
			}
		}
	}
	return nil
}

func selectUsageRefundLogs(ctx context.Context, s UsageRefundSelection) ([]Log, error) {
	ctx, cancel := context.WithTimeout(ctx, logQueryTimeout)
	defer cancel()
	q := LOG_DB.WithContext(ctx).Model(&Log{})
	switch {
	case len(s.LogIDs) > 0:
		q = q.Where("id IN ?", s.LogIDs)
	case len(s.RequestIDs) > 0:
		q = q.Where("request_id IN ?", s.RequestIDs)
	case len(s.UpstreamRequestIDs) > 0:
		q = q.Where("upstream_request_id IN ?", s.UpstreamRequestIDs)
	case s.Filter != nil:
		f := s.Filter
		q = q.Where("type = ? AND quota > 0 AND created_at >= ? AND created_at <= ?", LogTypeConsume, f.Start, f.End)
		if f.Group != "" {
			q = q.Where("`group` = ?", f.Group)
		}
		if len(f.ExcludeGroups) > 0 {
			q = q.Where("`group` NOT IN ?", f.ExcludeGroups)
		}
		if f.Channel != 0 {
			q = q.Where("channel_id = ?", f.Channel)
		}
		if f.UserID != 0 {
			q = q.Where("user_id = ?", f.UserID)
		}
		// Same rule as the usage log page: an exact username targets only that
		// user, so "bob" never also refunds "bobby".
		username := f.Username
		if f.UserID == 0 {
			if uid, ok := resolveLogUsername(username); ok {
				q = q.Where("user_id = ?", uid)
				username = ""
			}
		}
		if username != "" {
			q = applyLogContainsFilter(q, "username", username)
		}
		q = applyLogModelFilter(q, "model_name", f.ModelName)
		q = applyLogTokenFilter(q, "token_name", f.TokenName)
		if f.RequestID != "" {
			q = q.Where("request_id = ?", f.RequestID)
		}
		if f.UpstreamRequestID != "" {
			q = q.Where("upstream_request_id = ?", f.UpstreamRequestID)
		}
		if f.MaxCompletionTokens != nil {
			q = q.Where("completion_tokens <= ?", *f.MaxCompletionTokens)
		}
	}
	var logs []Log
	// ORDER BY id with LIMIT can make MySQL scan millions of old primary-key
	// rows. A valid batch must fit in full, so sort only after checking the cap.
	err := q.Limit(MaxUsageRefundItems + 1).Find(&logs).Error
	if err == nil && len(logs) > MaxUsageRefundItems {
		err = ErrUsageRefundLimit
	}
	if err == nil {
		sort.Slice(logs, func(i, j int) bool { return logs[i].Id < logs[j].Id })
	}
	return logs, err
}

func usageRefundFingerprint(log Log) string {
	// Ignore read-only channel enrichment; bind every stored source field.
	log.ChannelName = ""
	log.UsageRefund = nil
	data, _ := json.Marshal(log)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func canRefundUsageRole(adminRole, _ int) bool {
	return adminRole == common.RoleRootUser
}

func usageRefundEligibility(log Log, includeClientGone bool) string {
	if log.Type != LogTypeConsume || log.Quota <= 0 {
		return "not_charged"
	}
	var meta struct {
		IsTask       bool   `json:"is_task"`
		TaskID       string `json:"task_id"`
		ViolationFee bool   `json:"violation_fee"`
		StreamStatus struct {
			EndReason string `json:"end_reason"`
		} `json:"stream_status"`
	}
	if log.Other != "" && json.Unmarshal([]byte(log.Other), &meta) != nil {
		return "invalid_metadata"
	}
	if meta.IsTask || meta.TaskID != "" || meta.ViolationFee {
		return "separate_billing"
	}
	if !includeClientGone && meta.StreamStatus.EndReason == "client_gone" {
		return "client_gone"
	}
	return "eligible"
}

func PreviewUsageRefund(ctx context.Context, adminID, adminRole int, selection UsageRefundSelection) (*UsageRefundView, error) {
	if adminID <= 0 || adminRole != common.RoleRootUser {
		return nil, ErrUsageRefundForbidden
	}
	if err := validateUsageRefundSelection(selection); err != nil {
		return nil, err
	}
	logs, err := selectUsageRefundLogs(ctx, selection)
	if err != nil {
		return nil, err
	}
	ids, userIDs := make([]int, 0, len(logs)), make([]int, 0, len(logs))
	for _, log := range logs {
		ids = append(ids, log.Id)
		userIDs = append(userIDs, log.UserId)
	}
	var receipts []UsageRefundReceipt
	var users []User
	if len(ids) > 0 {
		if err = DB.WithContext(ctx).Where("source_log_id IN ?", ids).Find(&receipts).Error; err != nil {
			return nil, err
		}
		if err = DB.WithContext(ctx).Select("id", "username", "role").Where("id IN ?", userIDs).Find(&users).Error; err != nil {
			return nil, err
		}
	}
	refunded := make(map[int]string)
	for _, receipt := range receipts {
		refunded[receipt.SourceLogID] = receipt.BatchID
	}
	userMap := make(map[int]User)
	for _, user := range users {
		userMap[user.Id] = user
	}
	items := make([]UsageRefundItem, 0, len(logs))
	foundLogs, foundRequests, foundUpstream := map[int]bool{}, map[string]bool{}, map[string]bool{}
	for _, log := range logs {
		foundLogs[log.Id], foundRequests[log.RequestId], foundUpstream[log.UpstreamRequestId] = true, true, true
		status := usageRefundEligibility(log, selection.IncludeClientGone)
		if refunded[log.Id] != "" {
			status = "already_refunded"
		}
		user, exists := userMap[log.UserId]
		if !exists {
			status = "user_unavailable"
		} else if !canRefundUsageRole(adminRole, user.Role) {
			status = "permission_denied"
		}
		// The snapshot fixes the amount to return; the fingerprint still binds
		// the whole source log, so a changed original quota makes it stale.
		quota := log.Quota
		if amount, ok := selection.Amounts[log.Id]; ok && status == "eligible" {
			if amount > log.Quota {
				return nil, fmt.Errorf("%w: log %d refund amount exceeds its charge", ErrUsageRefundInvalid, log.Id)
			}
			quota = amount
		}
		items = append(items, UsageRefundItem{LogID: log.Id, RequestID: log.RequestId, UserID: log.UserId, Username: log.Username, TokenID: log.TokenId, Quota: quota, OriginalQuota: log.Quota, CreatedAt: log.CreatedAt, ModelName: log.ModelName, Channel: log.ChannelId, Status: status, Fingerprint: usageRefundFingerprint(log), RefundBatchID: refunded[log.Id]})
	}
	for _, id := range selection.LogIDs {
		if !foundLogs[id] {
			items = append(items, UsageRefundItem{LogID: id, Status: "missing"})
			foundLogs[id] = true
		}
	}
	for _, pair := range []struct {
		ids   []string
		found map[string]bool
	}{{selection.RequestIDs, foundRequests}, {selection.UpstreamRequestIDs, foundUpstream}} {
		for _, id := range pair.ids {
			if !pair.found[id] {
				items = append(items, UsageRefundItem{RequestID: id, Status: "missing"})
				pair.found[id] = true
			}
		}
	}
	view := &UsageRefundView{Items: items, Receipts: []UsageRefundReceipt{}}
	view.UsageRefundBatch = UsageRefundBatch{ID: uuid.NewString(), AdminID: adminID, Reason: strings.TrimSpace(selection.Reason), Status: "preview", CreatedAt: common.GetTimestamp()}
	if err = view.summarize(); err != nil {
		return nil, err
	}
	snapshot, err := json.Marshal(items)
	if err != nil {
		return nil, err
	}
	view.Snapshot = string(snapshot)
	if err = DB.WithContext(ctx).Create(&view.UsageRefundBatch).Error; err != nil {
		return nil, err
	}
	return view, nil
}

func (v *UsageRefundView) summarize() error {
	v.Quota, v.Count = 0, 0
	byUser := map[int]*UsageRefundUserSummary{}
	for _, item := range v.Items {
		if item.Status != "eligible" {
			continue
		}
		if item.Quota <= 0 || v.Quota > math.MaxInt-item.Quota {
			return ErrUsageRefundInvalid
		}
		v.Quota += item.Quota
		v.Count++
		if byUser[item.UserID] == nil {
			byUser[item.UserID] = &UsageRefundUserSummary{UserID: item.UserID, Username: item.Username}
		}
		byUser[item.UserID].Quota += item.Quota
		byUser[item.UserID].Count++
	}
	v.Users = make([]UsageRefundUserSummary, 0, len(byUser))
	for _, user := range byUser {
		v.Users = append(v.Users, *user)
	}
	sort.Slice(v.Users, func(i, j int) bool { return v.Users[i].UserID < v.Users[j].UserID })
	return nil
}

func GetUsageRefund(ctx context.Context, id string, adminID int) (*UsageRefundView, error) {
	v := &UsageRefundView{Receipts: []UsageRefundReceipt{}}
	if err := DB.WithContext(ctx).Where("id = ? AND admin_id = ?", id, adminID).First(&v.UsageRefundBatch).Error; err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(v.Snapshot), &v.Items); err != nil {
		return nil, err
	}
	if err := v.summarize(); err != nil {
		return nil, err
	}
	if err := DB.WithContext(ctx).Where("batch_id = ?", id).Order("source_log_id").Find(&v.Receipts).Error; err != nil {
		return nil, err
	}
	if err := DB.WithContext(ctx).Model(&UsageRefundNotice{}).Where("batch_id = ? AND delivered = ?", id, false).Count(&v.LogsPending).Error; err != nil {
		return nil, err
	}
	return v, nil
}

func ListUsageRefunds(ctx context.Context, adminID int) ([]UsageRefundBatch, error) {
	batches := []UsageRefundBatch{}
	err := DB.WithContext(ctx).Select("id", "admin_id", "reason", "status", "quota", "count", "created_at", "executed_at").Where("admin_id = ?", adminID).Order("created_at DESC, id DESC").Limit(50).Find(&batches).Error
	return batches, err
}

func ExecuteUsageRefund(ctx context.Context, id string, adminID, adminRole, expectedQuota, expectedCount int) (*UsageRefundView, error) {
	if adminID <= 0 || adminRole != common.RoleRootUser {
		return nil, ErrUsageRefundForbidden
	}
	v, err := GetUsageRefund(ctx, id, adminID)
	if err != nil {
		return nil, err
	}
	if expectedQuota != v.Quota || expectedCount != v.Count || v.Count == 0 {
		return nil, ErrUsageRefundInvalid
	}
	if v.Status == "executed" {
		if err := DeliverUsageRefundNotices(ctx, id); err != nil {
			common.SysError("usage refund notice retry: " + err.Error())
		}
		return GetUsageRefund(ctx, id, adminID)
	}
	if common.GetTimestamp()-v.CreatedAt > 86400 {
		return nil, ErrUsageRefundStale
	}
	ids := make([]int, 0, v.Count)
	for _, item := range v.Items {
		if item.Status == "eligible" {
			ids = append(ids, item.LogID)
		}
	}
	current, err := selectUsageRefundLogs(ctx, UsageRefundSelection{LogIDs: ids})
	if err != nil {
		return nil, err
	}
	fingerprints := make(map[int]string, len(current))
	for _, log := range current {
		fingerprints[log.Id] = usageRefundFingerprint(log)
	}
	for _, item := range v.Items {
		if item.Status == "eligible" && fingerprints[item.LogID] != item.Fingerprint {
			return nil, ErrUsageRefundStale
		}
	}
	tokenDeltas := map[int]int{}
	tokenKeys := map[int]string{}
	applied := false
	err = DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var batch UsageRefundBatch
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND admin_id = ?", id, adminID).First(&batch).Error; err != nil {
			return err
		}
		if batch.Status == "executed" {
			return nil
		}
		if batch.Status != "preview" {
			return ErrUsageRefundStale
		}
		// Stable user lock order serializes overlapping batches before the receipt check.
		for _, summary := range v.Users {
			var user User
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, summary.UserID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return fmt.Errorf("%w: user %d no longer exists", ErrUsageRefundStale, summary.UserID)
				}
				return err
			}
			if !canRefundUsageRole(adminRole, user.Role) {
				return ErrUsageRefundForbidden
			}
		}
		var tokens []Token
		requestedTokenIDs := make([]int, 0, v.Count)
		for _, item := range v.Items {
			if item.Status == "eligible" && item.TokenID > 0 {
				requestedTokenIDs = append(requestedTokenIDs, item.TokenID)
			}
		}
		if len(requestedTokenIDs) > 0 {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id IN ?", requestedTokenIDs).Order("id").Find(&tokens).Error; err != nil {
				return err
			}
		}
		tokensByID := make(map[int]Token, len(tokens))
		for _, token := range tokens {
			tokensByID[token.Id] = token
		}
		var existing []UsageRefundReceipt
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("source_log_id IN ?", ids).Find(&existing).Error; err != nil {
			return err
		}
		if len(existing) != 0 {
			return ErrUsageRefundStale
		}
		for _, summary := range v.Users {
			if err := adjustUserQuotaTx(tx, summary.UserID, -summary.Quota); err != nil {
				return err
			}
		}
		now := common.GetTimestamp()
		receipts := make([]UsageRefundReceipt, 0, v.Count)
		for _, item := range v.Items {
			if item.Status != "eligible" {
				continue
			}
			receipt := UsageRefundReceipt{SourceLogID: item.LogID, BatchID: id, UserID: item.UserID, TokenID: item.TokenID, Quota: item.Quota, RequestID: item.RequestID, CreatedAt: now}
			if token, exists := tokensByID[item.TokenID]; exists && token.UserId == item.UserID {
				tokenDeltas[token.Id] += item.Quota
				tokenKeys[token.Id] = token.Key
				receipt.TokenQuota = item.Quota
			}
			receipts = append(receipts, receipt)
		}
		tokenIDs := make([]int, 0, len(tokenDeltas))
		for tokenID := range tokenDeltas {
			tokenIDs = append(tokenIDs, tokenID)
		}
		sort.Ints(tokenIDs)
		for _, tokenID := range tokenIDs {
			if err := adjustTokenDeltaTx(tx, tokenID, -tokenDeltas[tokenID]); err != nil {
				return err
			}
		}
		if err := tx.CreateInBatches(&receipts, 200).Error; err != nil {
			return err
		}
		for _, summary := range v.Users {
			other, _ := json.Marshal(map[string]any{"usage_refund_batch_id": id, "refund_count": summary.Count, "reason": v.Reason, "admin_info": map[string]any{"admin_id": adminID}})
			entry := Log{UserId: summary.UserID, Username: summary.Username, CreatedAt: now, Type: LogTypeRefund, Quota: summary.Quota, Content: fmt.Sprintf("消费余额返还（%d 笔）：%s", summary.Count, v.Reason), Other: string(other)}
			payload, err := json.Marshal(entry)
			if err != nil {
				return err
			}
			if err := tx.Create(&UsageRefundNotice{BatchID: id, UserID: summary.UserID, Payload: string(payload)}).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&batch).Updates(map[string]any{"status": "executed", "executed_at": now}).Error; err != nil {
			return err
		}
		applied = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	if applied {
		for _, summary := range v.Users {
			updateBillingQuotaCaches(summary.UserID, 0, "", -summary.Quota, 0)
		}
		for tokenID, amount := range tokenDeltas {
			updateBillingQuotaCaches(0, tokenID, tokenKeys[tokenID], 0, -amount)
		}
	}
	if err := DeliverUsageRefundNotices(ctx, id); err != nil {
		common.SysError("usage refund notice pending, batch=" + id + ": " + err.Error())
	}
	return GetUsageRefund(ctx, id, adminID)
}
