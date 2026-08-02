package service

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
)

const (
	RiskSubjectTypeToken = "token"
	RiskSubjectTypeUser  = "user"

	RiskDecisionAllow   = "allow"
	RiskDecisionObserve = "observe"
	RiskDecisionBlock   = "block"

	RiskActionObserve = "observe"
	RiskActionBlock   = "block"
	RiskActionRecover = "recover"
	RiskActionManual  = "manual_unblock"

	RiskStatusNormal  = "normal"
	RiskStatusObserve = "observe"
	RiskStatusBlocked = "blocked"

	RiskEventTypeStart  = "start"
	RiskEventTypeFinish = "finish"
)

type RiskEvent struct {
	Type           string `json:"type"`
	OccurAt        int64  `json:"occur_at"`
	RequestID      string `json:"request_id"`
	RequestPath    string `json:"request_path"`
	UserID         int    `json:"user_id"`
	Username       string `json:"username"`
	TokenID        int    `json:"token_id"`
	TokenName      string `json:"token_name"`
	TokenMaskedKey string `json:"token_masked_key"`
	// Group is the risk group dimension snapshot, captured at BeforeRelay.
	// Always equals info.RiskGroup; never re-read from UsingGroup downstream
	// because auto cross-group retry can rewrite UsingGroup mid-request.
	Group         string `json:"group"`
	ClientIPHash  string `json:"client_ip_hash"`
	UserAgentHash string `json:"user_agent_hash"`
	StatusCode    int    `json:"status_code"`
}

// compiledRiskRule is the in-memory form of a model.RiskRule, with conditions
// and groups parsed once at reload time. Rules with an empty Groups set are
// dropped during reload — see DEV_GUIDE §5 red line "未配置分组 = 不启用".
type compiledRiskRule struct {
	Raw        *model.RiskRule
	Conditions []types.RiskCondition
	Groups     map[string]struct{}
}

func normalizeRiskMessage(message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return operation_setting.GetRiskControlSetting().DefaultResponseMessage
	}
	return message
}

func encodeRiskJSON(data any) string {
	bytes, err := common.Marshal(data)
	if err != nil {
		return ""
	}
	return string(bytes)
}
