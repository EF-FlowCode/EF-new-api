package controller

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
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
		"config":   masked,
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

// UpdateModerationConfig persists a new ModerationSetting and reloads the
// engine in-process. If the request payload contains masked entries (only
// stars + 4 chars), those are merged from the previous config to avoid
// destroying real keys when the admin saves a partial form.
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
		req.APIKeys = mergePreservedModerationKeys(current.APIKeys, req.APIKeys)
	}
	operation_setting.NormalizeModerationSetting(&req.ModerationSetting)
	configMap, err := config.ConfigToMap(&req.ModerationSetting)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	for key, value := range configMap {
		if err = model.UpdateOption("moderation."+key, value); err != nil {
			common.ApiError(c, err)
			return
		}
	}
	service.ReloadModerationConfig()
	GetModerationConfig(c)
}

// mergePreservedModerationKeys substitutes incoming "***abcd" placeholders
// with the matching entries from the existing key list. New keys (raw text)
// pass through unchanged.
func mergePreservedModerationKeys(existing, incoming []string) []string {
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
			// masked entry without a match — drop it to avoid storing junk
			continue
		}
		out = append(out, k)
	}
	return out
}

func isMaskedKey(k string) bool {
	if len(k) < 5 {
		return false
	}
	starsEnd := -1
	for i, r := range k {
		if r != '*' {
			starsEnd = i
			break
		}
	}
	return starsEnd > 0
}

func findKeyBySuffix(keys []string, masked string) string {
	suffix := masked
	for i, r := range masked {
		if r != '*' {
			suffix = masked[i:]
			break
		}
	}
	if len(suffix) < 4 {
		return ""
	}
	for _, k := range keys {
		if strings.HasSuffix(k, suffix) {
			return k
		}
	}
	return ""
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
