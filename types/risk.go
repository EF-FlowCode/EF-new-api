package types

type RiskCondition struct {
	Metric string  `json:"metric"`
	Op     string  `json:"op"`
	Value  float64 `json:"value"`
}

type RiskMetrics struct {
	DistinctIP10M   int `json:"distinct_ip_10m"`
	DistinctIP1H    int `json:"distinct_ip_1h"`
	DistinctUA10M   int `json:"distinct_ua_10m"`
	TokensPerIP10M  int `json:"tokens_per_ip_10m"`
	RequestCount1M  int `json:"request_count_1m"`
	RequestCount10M int `json:"request_count_10m"`
	InflightNow     int `json:"inflight_now"`
	RuleHitCount24H int `json:"rule_hit_count_24h"`
	RiskScore       int `json:"risk_score"`
}

type RiskDecision struct {
	Scope     string `json:"scope"`
	SubjectID int    `json:"subject_id"`
	// Group is the request group this decision applies to. Always populated by
	// the engine; downstream serialization keeps it for audit and reverse-key
	// lookup of caches.
	Group              string      `json:"group"`
	Decision           string      `json:"decision"`
	Action             string      `json:"action"`
	Status             string      `json:"status"`
	RuleID             int         `json:"rule_id"`
	RuleName           string      `json:"rule_name"`
	Detector           string      `json:"detector"`
	Reason             string      `json:"reason"`
	MatchedRules       []string    `json:"matched_rules,omitempty"`
	StatusCode         int         `json:"status_code"`
	ResponseMessage    string      `json:"response_message"`
	AutoRecover        bool        `json:"auto_recover"`
	RecoverMode        string      `json:"recover_mode"`
	RecoverAfterSecond int         `json:"recover_after_seconds"`
	BlockUntil         int64       `json:"block_until"`
	RiskScore          int         `json:"risk_score"`
	Metrics            RiskMetrics `json:"metrics"`
}

type RiskAudit struct {
	TokenDecision *RiskDecision `json:"token_decision,omitempty"`
	UserDecision  *RiskDecision `json:"user_decision,omitempty"`
	FinalDecision string        `json:"final_decision"`
	FinalReason   string        `json:"final_reason,omitempty"`
}
