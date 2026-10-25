package controller

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"eflow.local/new-api/common"
	"eflow.local/new-api/model"
	"eflow.local/new-api/service"
	"eflow.local/new-api/setting/config"
	"eflow.local/new-api/setting/operation_setting"
	"eflow.local/new-api/types"
	"github.com/gin-gonic/gin"
)

// GetModerationConfig returns the live ModerationSetting with API keys masked
// so the browser only sees suffixes — admins can still tell which keys are
// configured but cannot exfiltrate full secrets through the management UI.
func GetModerationConfig(c *gin.Context) {
	cfg := operation_setting.GetModerationSetting()
	masked := *cfg
	masked.APIKeys = make([]string, 0, len(cfg.APIKeys))
	for _, k := range cfg.APIKeys {
		masked.APIKeys = append(masked.APIKeys, operation_setting.MaskModerationKey(k))
	}
	common.ApiSuccess(c, gin.H{
		"config":    masked,
		"key_count": len(cfg.APIKeys),
	})
}

type updateModerationConfigRequest struct {
	operation_setting.ModerationSetting
	// PreserveExistingKeys: when true (default), masked entries (containing
	// only `*` plus a suffix) are kept from the existing config rather than
	// overwritten. This lets the admin save the form without re-typing all
	// keys every time.
	PreserveExistingKeys *bool `json:"preserve_existing_keys"`
}

// UpdateModerationConfig persists a new ModerationSetting in one Option
// transaction and reloads the engine in-process. Masked entries (stars +
// 4 chars) must uniquely match a current key, otherwise the save is
// rejected so a stale form can never silently drop or replace real keys.
func UpdateModerationConfig(c *gin.Context) {
	var req updateModerationConfigRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "无效的参数",
		})
		return
	}
	current := operation_setting.GetModerationSetting()
	preserve := true
	if req.PreserveExistingKeys != nil {
		preserve = *req.PreserveExistingKeys
	}
	if preserve {
		keys, err := mergePreservedModerationKeys(current.APIKeys, req.APIKeys)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		req.APIKeys = keys
	}
	operation_setting.NormalizeModerationSetting(&req.ModerationSetting)
	configMap, err := config.ConfigToMap(&req.ModerationSetting)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	values := make(map[string]string, len(configMap))
	for key, value := range configMap {
		values["moderation."+key] = value
	}
	if err = model.UpdateOptionsBulkValidated(values, func(locked map[string]string) error {
		if !preserve {
			return nil
		}
		var keys []string
		raw := locked["moderation.api_keys"]
		if raw != "" {
			if err := json.Unmarshal([]byte(raw), &keys); err != nil {
				return err
			}
		}
		if !slices.Equal(keys, current.APIKeys) {
			return errors.New("审核密钥已更新，请刷新后重试")
		}
		return nil
	}); err != nil {
		common.ApiError(c, err)
		return
	}
	service.ReloadModerationConfig()
	GetModerationConfig(c)
}

// mergePreservedModerationKeys substitutes incoming "***abcd" placeholders
// with the matching entries from the existing key list. New keys (raw text)
// pass through unchanged.
func mergePreservedModerationKeys(existing, incoming []string) ([]string, error) {
	out := make([]string, 0, len(incoming))
	for _, k := range incoming {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		if isMaskedKey(k) {
			if real := findKeyBySuffix(existing, k); real != "" {
				out = append(out, real)
				continue
			}
			return nil, errors.New("审核密钥掩码已失效或不唯一，请刷新或输入完整密钥")
		}
		out = append(out, k)
	}
	return out, nil
}

func isMaskedKey(k string) bool {
	return strings.HasPrefix(k, "*")
}

func findKeyBySuffix(keys []string, masked string) string {
	match := ""
	for _, key := range keys {
		if operation_setting.MaskModerationKey(key) != masked {
			continue
		}
		if match != "" && match != key {
			return ""
		}
		match = key
	}
	return match
}

// SubmitModerationDebug enqueues a debug moderation job and returns the
// request_id for the frontend to poll. Group lets admins rehearse a specific
// group's rule set; empty group falls back to a "preview against every
// enabled rule" mode for early-stage exploration.
type submitModerationDebugRequest struct {
	Text   string   `json:"text"`
	Images []string `json:"images"`
	Group  string   `json:"group"`
}

func SubmitModerationDebug(c *gin.Context) {
	var req submitModerationDebugRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "无效的参数",
		})
		return
	}
	requestID, err := service.SubmitModerationDebug(req.Text, req.Images, req.Group)
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	common.ApiSuccess(c, gin.H{
		"request_id": requestID,
		"queued":     true,
		"group":      strings.TrimSpace(req.Group),
	})
}

// GetModerationDebugResult returns the cached result (if ready) or an
// explicit pending payload so the frontend can keep polling.
func GetModerationDebugResult(c *gin.Context) {
	requestID := strings.TrimSpace(c.Param("id"))
	if requestID == "" {
		common.ApiErrorMsg(c, "missing request_id")
		return
	}
	result, ok := service.GetModerationDebugResult(requestID)
	if !ok {
		common.ApiSuccess(c, gin.H{
			"request_id": requestID,
			"pending":    true,
		})
		return
	}
	common.ApiSuccess(c, gin.H{
		"request_id": requestID,
		"pending":    false,
		"result":     result,
	})
}

func GetModerationIncidentDetail(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "无效的 ID")
		return
	}
	row, err := model.GetModerationIncident(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, row)
}

func GetModerationIncidents(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	query := model.ModerationIncidentQuery{
		Group:   c.Query("group"),
		Source:  c.Query("source"),
		Keyword: c.Query("keyword"),
	}
	if v := strings.TrimSpace(c.Query("flagged")); v != "" {
		flagged := v == "true" || v == "1"
		query.Flagged = &flagged
	}
	if v := strings.TrimSpace(c.Query("user_id")); v != "" {
		if uid, err := strconv.Atoi(v); err == nil {
			query.UserID = uid
		}
	}
	items, total, err := model.ListModerationIncidents(query, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}

func GetModerationOverview(c *gin.Context) {
	overview, err := service.ModerationOverview()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, overview)
}

// GetModerationQueueStats powers the live "运行状态" card on the content
// moderation tab. Returns the in-memory queue depth, Redis-persisted
// queue depth, per-worker state, dropped-count, and incident batcher
// pending size.
func GetModerationQueueStats(c *gin.Context) {
	common.ApiSuccess(c, service.QueueStats())
}

// GetModerationCategories powers the rule editor dropdown so the UI doesn't
// have to maintain its own copy of the OpenAI category list.
func GetModerationCategories(c *gin.Context) {
	common.ApiSuccess(c, service.ListModerationCategories())
}

type moderationRuleUpsertRequest struct {
	Name        string                      `json:"name"`
	Description string                      `json:"description"`
	Enabled     bool                        `json:"enabled"`
	MatchMode   string                      `json:"match_mode"`
	Action      string                      `json:"action"`
	Priority    int                         `json:"priority"`
	ScoreWeight int                         `json:"score_weight"`
	Conditions  []types.ModerationCondition `json:"conditions"`
	Groups      []string                    `json:"groups"`
}

func GetModerationRules(c *gin.Context) {
	rules, err := model.ListModerationRules()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, rules)
}

func CreateModerationRule(c *gin.Context) {
	rule, err := bindModerationRuleRequest(c, 0)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	rule.CreatedBy = c.GetInt("id")
	rule.UpdatedBy = c.GetInt("id")
	if err = service.CreateModerationRule(rule); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, rule)
}

func UpdateModerationRule(c *gin.Context) {
	ruleID, err := strconv.Atoi(c.Param("id"))
	if err != nil || ruleID <= 0 {
		common.ApiErrorMsg(c, "无效的规则 ID")
		return
	}
	rule, err := bindModerationRuleRequest(c, ruleID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	rule.UpdatedBy = c.GetInt("id")
	if err = service.UpdateModerationRule(rule); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, rule)
}

func DeleteModerationRule(c *gin.Context) {
	ruleID, err := strconv.Atoi(c.Param("id"))
	if err != nil || ruleID <= 0 {
		common.ApiErrorMsg(c, "无效的规则 ID")
		return
	}
	if err = service.DeleteModerationRule(ruleID); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"id": ruleID})
}

func bindModerationRuleRequest(c *gin.Context, ruleID int) (*model.ModerationRule, error) {
	var req moderationRuleUpsertRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		return nil, err
	}
	condBytes, err := common.Marshal(req.Conditions)
	if err != nil {
		return nil, err
	}
	groups := dedupeStrings(req.Groups)
	groupsBytes, err := common.Marshal(groups)
	if err != nil {
		return nil, err
	}
	return &model.ModerationRule{
		Id:          ruleID,
		Name:        req.Name,
		Description: req.Description,
		Enabled:     req.Enabled,
		MatchMode:   req.MatchMode,
		Action:      req.Action,
		Priority:    req.Priority,
		ScoreWeight: req.ScoreWeight,
		Conditions:  string(condBytes),
		Groups:      string(groupsBytes),
	}, nil
}
