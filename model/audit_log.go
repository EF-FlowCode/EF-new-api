package model

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"

	"eflow.local/new-api/common"
	"eflow.local/new-api/logger"
	"github.com/gin-gonic/gin"
)

const (
	AuditCategoryLogin       = "login"
	AuditCategorySecurity    = "security"
	AuditCategoryOperation   = "operation"
	AuditCategoryAccessToken = "access_token"
)

// AuditLog is retained independently of usage logs. TokenRef identifies a PAT
// generation without storing its bearer credential.
//
// eflow: 表结构与上游 QuantumNous/new-api model/audit_log.go 一致（去掉 ClickHouse 分支）。
// 审计记录永久保留：不得加入任何按时间、容量或条数清理 audit_logs 的逻辑，
// 历史日志删除接口也不得触及本表（见 SPEC 硬性规则 3）。
type AuditLog struct {
	Id         int        `json:"id"`
	EventId    string     `json:"event_id" gorm:"type:varchar(64);uniqueIndex"`
	UserId     int        `json:"user_id" gorm:"index:idx_audit_user_time,priority:1"`
	Username   string     `json:"username" gorm:"type:varchar(64);index"`
	ActorRole  int        `json:"actor_role"` // Immutable role of the actor when the event began, not the log owner.
	CreatedAt  int64      `json:"created_at" gorm:"type:bigint;index:idx_audit_user_time,priority:2;index:idx_audit_token_time,priority:2;index"`
	Category   string     `json:"category" gorm:"type:varchar(24);index"`
	Action     string     `json:"action" gorm:"type:varchar(128)"`
	TokenRef   string     `json:"token_ref" gorm:"type:varchar(64);index:idx_audit_token_time,priority:1"`
	AuthMethod string     `json:"auth_method" gorm:"type:varchar(24)"`
	Ip         string     `json:"ip" gorm:"type:varchar(64)"`
	UserAgent  string     `json:"user_agent" gorm:"type:varchar(512)"`
	Method     string     `json:"method" gorm:"type:varchar(16)"`
	Route      string     `json:"route" gorm:"type:varchar(255)"`
	Status     int        `json:"status"`
	Success    bool       `json:"success"`
	RequestId  string     `json:"request_id" gorm:"type:varchar(64);index"`
	Content    string     `json:"content" gorm:"type:text"`
	Other      AuditOther `json:"other" gorm:"type:json"`
}

type AuditLogFilter struct {
	SelfView        bool // Server-selected metadata projection, independent of the viewer's actual role.
	UserId          int
	Username        string
	Category        string
	TokenRef        string
	ExcludeTokenRef string
	RequestId       string
	StartTimestamp  int64
	EndTimestamp    int64
	Success         *bool
}

// auditVisibleActorRoles are the actor roles a non-root viewer may see. Events
// performed by root, or whose actor role could not be resolved (0), stay root-only.
var auditVisibleActorRoles = []int{
	common.RoleCommonUser,
	common.RoleCustomerServiceUser,
	common.RoleFinanceUser,
	common.RoleAdminUser,
}

func auditKnownActorRole(role int) bool {
	return role == common.RoleRootUser || common.IsValidateRole(role) && role >= common.RoleCommonUser
}

func AccessTokenFingerprint(token string) string {
	token = strings.TrimRight(token, " ")
	if token == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(token))
	return fmt.Sprintf("%x", digest)
}

// RecordAuditLog captures safe request metadata only; raw URLs, query strings,
// credentials and response/request bodies must never enter this table.
func RecordAuditLog(c *gin.Context, entry AuditLog) {
	ctx := context.Background()
	if c != nil && c.Request != nil {
		ctx = c.Request.Context()
		entry.RequestId = c.GetString(common.RequestIdKey)
		entry.Ip = c.ClientIP()
		entry.UserAgent = c.Request.UserAgent()
		entry.Method = c.Request.Method
		entry.Route = c.FullPath()
		if entry.Status == 0 {
			entry.Status = c.Writer.Status()
		}
		if entry.AuthMethod == "" {
			entry.AuthMethod = "session"
			if c.GetBool("use_access_token") {
				entry.AuthMethod = "access_token"
			}
		}
		if entry.TokenRef == "" {
			entry.TokenRef = c.GetString("access_token_ref")
		}
	}
	if entry.CreatedAt == 0 {
		entry.CreatedAt = common.GetTimestamp()
	}
	if entry.RequestId == "" {
		entry.RequestId = common.NewRequestId()
	}
	if entry.EventId == "" {
		entry.EventId = common.NewRequestId()
	}
	if !auditKnownActorRole(entry.ActorRole) {
		logger.LogError(ctx, fmt.Sprintf("audit actor role unavailable (request_id=%s, actor_role=%d)", entry.RequestId, entry.ActorRole))
		entry.ActorRole = 0 // Unknown actors remain visible to root only.
	}
	if entry.Username == "" && entry.UserId > 0 {
		entry.Username, _ = GetUsernameById(entry.UserId, false)
	}
	if ua := []rune(entry.UserAgent); len(ua) > 512 {
		entry.UserAgent = string(ua[:512])
	}
	if LOG_DB == nil {
		logger.LogError(ctx, fmt.Sprintf("audit log write failed (request_id=%s): log database unavailable", entry.RequestId))
		return
	}
	if err := LOG_DB.Table("audit_logs").Create(&entry).Error; err != nil {
		logger.LogError(ctx, fmt.Sprintf("audit log write failed (request_id=%s): %v", entry.RequestId, err))
	}
}

// RecordLoginLog writes a login event. content is the English fallback text;
// action+params let the frontend render a localized message.
func RecordLoginLog(userId, actorRole int, username string, content string, action string, params map[string]any, other AuditOther, request ...*gin.Context) {
	other.Op = &AuditOperation{Action: action, Params: params}
	var c *gin.Context
	if len(request) > 0 {
		c = request[0]
	}
	RecordAuditLog(c, AuditLog{UserId: userId, Username: username, ActorRole: actorRole, Category: AuditCategoryLogin, Action: action, Content: content, Other: other, Success: true})
}

// RecordOperationAuditLog writes an operation (adminInfo set) or security
// (adminInfo nil) event owned by logUserId.
func RecordOperationAuditLog(logUserId, actorRole int, content string, action string, params map[string]any, adminInfo *AuditAdminInfo, auditInfo *AuditRequestInfo, request ...*gin.Context) {
	username, _ := GetUsernameById(logUserId, false)
	other := AuditOther{
		Op:        &AuditOperation{Action: action, Params: params},
		AdminInfo: adminInfo,
		AuditInfo: auditInfo,
	}
	var c *gin.Context
	if len(request) > 0 {
		c = request[0]
	}
	category := AuditCategoryOperation
	if adminInfo == nil {
		category = AuditCategorySecurity
	}
	status, success := 200, true
	if auditInfo != nil {
		status = auditInfo.Status
		success = auditInfo.Success
	}
	RecordAuditLog(c, AuditLog{UserId: logUserId, Username: username, ActorRole: actorRole, Category: category, Action: action, Content: content, Status: status, Success: success, Other: other})
}

func GetAuditLogs(filter AuditLogFilter, start, limit, viewerRole int) ([]*AuditLog, int64, error) {
	query := LOG_DB.Model(&AuditLog{})
	if viewerRole < common.RoleRootUser {
		query = query.Where("actor_role IN ?", auditVisibleActorRoles)
	}
	if filter.UserId > 0 {
		query = query.Where("user_id = ?", filter.UserId)
	}
	if filter.Username != "" {
		query = query.Where("username = ?", filter.Username)
	}
	if filter.Category != "" {
		query = query.Where("category = ?", filter.Category)
	}
	if filter.TokenRef != "" {
		query = query.Where("token_ref = ?", filter.TokenRef)
	}
	if filter.ExcludeTokenRef != "" {
		query = query.Where("token_ref <> ?", filter.ExcludeTokenRef)
	}
	if filter.RequestId != "" {
		query = query.Where("request_id = ?", filter.RequestId)
	}
	if filter.StartTimestamp > 0 {
		query = query.Where("created_at >= ?", filter.StartTimestamp)
	}
	if filter.EndTimestamp > 0 {
		query = query.Where("created_at <= ?", filter.EndTimestamp)
	}
	if filter.Success != nil {
		query = query.Where("success = ?", *filter.Success)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	logs := make([]*AuditLog, 0)
	if err := query.Order("created_at DESC").Order("event_id DESC").Offset(start).Limit(limit).Find(&logs).Error; err != nil {
		return nil, 0, err
	}
	rootView := !filter.SelfView && viewerRole >= common.RoleRootUser
	adminView := !filter.SelfView && viewerRole >= common.RoleAdminUser
	for _, entry := range logs {
		if !rootView {
			entry.Other.RootInfo = nil
		}
		if !adminView {
			entry.Other.AdminInfo = nil
			entry.Other.AuditInfo = nil
		}
	}
	return logs, total, nil
}

func ValidAuditCategory(category string) bool {
	return category == "" || category == AuditCategoryLogin || category == AuditCategorySecurity || category == AuditCategoryOperation || category == AuditCategoryAccessToken
}

func ValidTokenFingerprint(value string) bool {
	if value == "" {
		return true
	}
	if len(value) != 64 {
		return false
	}
	return strings.Trim(value, "0123456789abcdef") == ""
}
