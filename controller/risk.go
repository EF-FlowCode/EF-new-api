package controller

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/requestip"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

type riskRuleUpsertRequest struct {
	Name                string                `json:"name"`
	Description         string                `json:"description"`
	Enabled             bool                  `json:"enabled"`
	Scope               string                `json:"scope"`
	Detector            string                `json:"detector"`
	MatchMode           string                `json:"match_mode"`
	Priority            int                   `json:"priority"`
	Action              string                `json:"action"`
	AutoBlock           bool                  `json:"auto_block"`
	AutoRecover         bool                  `json:"auto_recover"`
	RecoverMode         string                `json:"recover_mode"`
	RecoverAfterSeconds int                   `json:"recover_after_seconds"`
	ResponseStatusCode  int                   `json:"response_status_code"`
	ResponseMessage     string                `json:"response_message"`
	ScoreWeight         int                   `json:"score_weight"`
	Conditions          []types.RiskCondition `json:"conditions"`
	Groups              []string              `json:"groups"`
}

func GetRiskCenterOverview(c *gin.Context) {
	overview, err := service.GetRiskOverview()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, overview)
}

func GetRiskCenterConfig(c *gin.Context) {
	common.ApiSuccess(c, service.GetRiskControlConfig())
}

func UpdateRiskCenterConfig(c *gin.Context) {
	var req operation_setting.RiskControlSetting
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "无效的参数",
		})
		return
	}
	operation_setting.NormalizeRiskControlSetting(&req)
	configMap, err := config.ConfigToMap(&req)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	for key, value := range configMap {
		if err = model.UpdateOption("risk_control."+key, value); err != nil {
			common.ApiError(c, err)
			return
		}
	}
	common.ApiSuccess(c, service.GetRiskControlConfig())
}

func DetectRiskIP(c *gin.Context) {
	common.ApiSuccess(c, requestip.DiagnoseRequest(c))
}

func GetRiskRules(c *gin.Context) {
	rules, err := service.ListRiskRules()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, rules)
}

func CreateRiskRule(c *gin.Context) {
	rule, err := bindRiskRuleRequest(c, 0)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	rule.CreatedBy = c.GetInt("id")
	rule.UpdatedBy = c.GetInt("id")
	if err = service.CreateRiskRule(rule); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, rule)
}

func UpdateRiskRule(c *gin.Context) {
	ruleID, err := strconv.Atoi(c.Param("id"))
	if err != nil || ruleID <= 0 {
		common.ApiErrorMsg(c, "无效的规则 ID")
		return
	}
	rule, err := bindRiskRuleRequest(c, ruleID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	rule.UpdatedBy = c.GetInt("id")
	if err = service.UpdateRiskRule(rule); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, rule)
}

func DeleteRiskRule(c *gin.Context) {
	ruleID, err := strconv.Atoi(c.Param("id"))
	if err != nil || ruleID <= 0 {
		common.ApiErrorMsg(c, "无效的规则 ID")
		return
	}
	if err = service.DeleteRiskRule(ruleID); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"id": ruleID})
}

func GetRiskSubjects(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	query := model.RiskSubjectQuery{
		Scope:   c.Query("scope"),
		Status:  c.Query("status"),
		Keyword: c.Query("keyword"),
		Group:   c.Query("group"),
	}
	items, total, err := service.ListRiskSubjectSnapshots(query, pageInfo)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}

func GetRiskIncidents(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	query := model.RiskIncidentQuery{
		Scope:   c.Query("scope"),
		Action:  c.Query("action"),
		Keyword: c.Query("keyword"),
		Group:   c.Query("group"),
	}
	items, total, err := service.ListRiskIncidents(query, pageInfo)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}

// UnblockRiskSubject requires ?group=<name>; the engine stores blocks under
// the (scope, subjectID, group) triple, so the operator must say which group
// to clear. The group does not need to be inside EnabledGroups — admins may
// clean up legacy blocks left behind after a group leaves the whitelist.
func UnblockRiskSubject(c *gin.Context) {
	scope := c.Param("scope")
	subjectID, err := strconv.Atoi(c.Param("id"))
	if err != nil || subjectID <= 0 {
		common.ApiErrorMsg(c, "无效的主体 ID")
		return
	}
	group := strings.TrimSpace(c.Query("group"))
	if group == "" {
		common.ApiErrorMsg(c, "解封必须指定分组")
		return
	}
	operator := fmt.Sprintf("%s#%d", c.GetString("username"), c.GetInt("id"))
	if err = service.UnblockRiskSubject(scope, subjectID, group, operator); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"scope": scope,
		"id":    subjectID,
		"group": group,
	})
}

// GetRiskGroups returns the per-group risk control matrix consumed by the
// admin "分组启用矩阵" widget. See DEV_GUIDE §12.1 for the schema.
//
//	{ schema_version: 1, global_mode: ..., items: [...] }
//
// `auto` is filtered out — auto resolves to a real group during distribute and
// is never a valid risk control target on its own.
func GetRiskGroups(c *gin.Context) {
	cfg := operation_setting.GetRiskControlSetting()
	whitelist := make(map[string]struct{}, len(cfg.EnabledGroups))
	for _, g := range cfg.EnabledGroups {
		whitelist[g] = struct{}{}
	}

	rules, err := model.ListRiskRules()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	totalByGroup := make(map[string]int)
	enabledByGroup := make(map[string]int)
	for _, rule := range rules {
		if rule == nil {
			continue
		}
		for _, g := range rule.ParsedGroups() {
			totalByGroup[g]++
			if rule.Enabled {
				enabledByGroup[g]++
			}
		}
	}

	groupRatios := ratio_setting.GetGroupRatioCopy()
	names := make([]string, 0, len(groupRatios))
	for name := range groupRatios {
		if name == operation_setting.RiskControlAutoGroup || name == "" {
			continue
		}
		names = append(names, name)
	}
	// stable order: enabled first, then alphabetic
	sortRiskGroups(names, whitelist)

	items := make([]map[string]any, 0, len(names))
	for _, name := range names {
		_, enabled := whitelist[name]
		mode := cfg.GroupModes[name]
		items = append(items, map[string]any{
			"name":                    name,
			"enabled":                 enabled,
			"mode":                    mode,
			"effective_mode":          operation_setting.EffectiveRiskModeForGroup(cfg, name),
			"rule_count_total":        totalByGroup[name],
			"rule_count_enabled":      enabledByGroup[name],
			"active_subject_count":    countOrZero(model.CountRiskSubjectSnapshotsByStatusAndGroup(service.RiskStatusObserve, name)),
			"blocked_subject_count":   countOrZero(model.CountRiskSubjectSnapshotsByStatusAndGroup(service.RiskStatusBlocked, name)),
			"high_risk_subject_count": countOrZero(model.CountHighRiskSubjectSnapshotsByGroup(60, name)),
		})
	}

	common.ApiSuccess(c, gin.H{
		"schema_version": 1,
		"global_mode":    cfg.Mode,
		"items":          items,
	})
}

// sortRiskGroups sorts in-place: enabled groups first, then by lexicographic
// order. Stable enough for the matrix UI; the absolute ordering is not
// observable to clients besides display.
func sortRiskGroups(names []string, whitelist map[string]struct{}) {
	for i := 1; i < len(names); i++ {
		for j := i; j > 0; j-- {
			_, ai := whitelist[names[j]]
			_, bi := whitelist[names[j-1]]
			if ai && !bi {
				names[j], names[j-1] = names[j-1], names[j]
				continue
			}
			if ai == bi && names[j] < names[j-1] {
				names[j], names[j-1] = names[j-1], names[j]
				continue
			}
			break
		}
	}
}

func countOrZero(n int64, err error) int64 {
	if err != nil {
		return 0
	}
	return n
}

func bindRiskRuleRequest(c *gin.Context, ruleID int) (*model.RiskRule, error) {
	var req riskRuleUpsertRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		return nil, err
	}
	conditionsBytes, err := common.Marshal(req.Conditions)
	if err != nil {
		return nil, err
	}
	groups := dedupeStrings(req.Groups)
	groupsBytes, err := common.Marshal(groups)
	if err != nil {
		return nil, err
	}
	return &model.RiskRule{
		Id:                  ruleID,
		Name:                req.Name,
		Description:         req.Description,
		Enabled:             req.Enabled,
		Scope:               req.Scope,
		Detector:            req.Detector,
		MatchMode:           req.MatchMode,
		Priority:            req.Priority,
		Action:              req.Action,
		AutoBlock:           req.AutoBlock,
		AutoRecover:         req.AutoRecover,
		RecoverMode:         req.RecoverMode,
		RecoverAfterSeconds: req.RecoverAfterSeconds,
		ResponseStatusCode:  req.ResponseStatusCode,
		ResponseMessage:     req.ResponseMessage,
		ScoreWeight:         req.ScoreWeight,
		Conditions:          string(conditionsBytes),
		Groups:              string(groupsBytes),
	}, nil
}

func dedupeStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
