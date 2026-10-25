package billing_setting

import (
	"fmt"
	"strings"

	"eflow.local/new-api/pkg/billingexpr"
	"eflow.local/new-api/pkg/knowledge"
	"eflow.local/new-api/setting/config"
	"eflow.local/new-api/setting/ratio_setting"
	"github.com/samber/lo"
)

const (
	BillingModeRatio      = "ratio"
	BillingModeFlatPrice  = "flat_price"
	BillingModeTieredExpr = "tiered_expr"
	// eflow: 仅作模型广场展示标记——结算仍走 ratio（ModelRatio）。
	// 用于阿里 ASR 这类「按音频秒数」计费的模型：内部把秒数当 token、
	// ModelRatio 编码 元/1M秒，前端识别此 mode 后把价格行渲染成「¥X/秒」。
	BillingModeAudioSeconds = "audio_seconds"
	BillingModeField        = "billing_mode"
	BillingExprField        = "billing_expr"
)

func IsExpressionBillingMode(mode string) bool {
	return mode == BillingModeFlatPrice || mode == BillingModeTieredExpr
}

// IsKnownBillingMode reports whether a stored billing_mode is one the runtime
// understands; anything else would silently fall back to ratio billing.
func IsKnownBillingMode(mode string) bool {
	switch mode {
	case BillingModeRatio, BillingModeFlatPrice, BillingModeTieredExpr, BillingModeAudioSeconds, BillingModeDimensional:
		return true
	}
	return false
}

// NormalizeExpressionBillingMode classifies expression modes by their actual
// structure. This keeps legacy New API data compatible while ensuring dynamic
// expressions are never presented as flat prices.
func NormalizeExpressionBillingMode(mode, expression string) string {
	if !IsExpressionBillingMode(mode) || strings.TrimSpace(expression) == "" {
		return mode
	}
	if _, ok := billingexpr.ParseFlatTokenBillingExpr(expression); ok {
		return BillingModeFlatPrice
	}
	return BillingModeTieredExpr
}

// BillingSetting is managed by config.GlobalConfig.Register.
// Runtime projections: billing_mode, billing_expr and task_expressions.
// Persistent authority: ModelPricing.
type BillingSetting struct {
	TaskPriorityVersion int                                 `json:"task_priority_version"`
	BillingMode         map[string]string                   `json:"billing_mode"`
	BillingExpr         map[string]string                   `json:"billing_expr"`
	TaskExpressions     map[string]map[string]TaskPriceRule `json:"task_expressions"`
}

var billingSetting = BillingSetting{
	BillingMode:     make(map[string]string),
	BillingExpr:     make(map[string]string),
	TaskExpressions: make(map[string]map[string]TaskPriceRule),
}

func init() {
	config.GlobalConfig.Register("billing_setting", &billingSetting)
}

// ---------------------------------------------------------------------------
// Read accessors (hot path, must be fast)
// ---------------------------------------------------------------------------

// GetBillingRule resolves the mode and expression from one immutable config
// version so a concurrent publication cannot mix fields from two versions.
func GetBillingRule(model string) (mode, expression string, hasExpression bool) {
	mode = BillingModeRatio
	setting := billingSettingSnapshot()
	if configured, exists := setting.BillingMode[model]; exists {
		mode = configured
	}
	expression, hasExpression = setting.BillingExpr[model]
	// A configured per-call price wins over the synthesized knowledge rule,
	// matching the price-before-ratio order of ModelPriceHelper.
	if !hasExpression {
		if capability, ok := knowledge.Lookup(model); ok && capability.Kind != "" && !hasModelPrice(model) {
			if inputRatio, configured := ratio_setting.GetModelRatioCopy()[ratio_setting.FormatMatchingModelName(model)]; configured {
				input := inputRatio * 2
				image := input
				if imageRatio, hasImage := ratio_setting.GetImageRatio(model); hasImage {
					image *= imageRatio
				}
				expression = (knowledge.Prices{Input: &input, Image: &image}).Expression()
				hasExpression = true
				mode = BillingModeTieredExpr
			}
		}
	}
	return mode, expression, hasExpression
}

func GetBillingMode(model string) string {
	mode, _, _ := GetBillingRule(model)
	return mode
}

func GetBillingExpr(model string) (string, bool) {
	_, expression, exists := GetBillingRule(model)
	return expression, exists
}

func GetBillingModeCopy() map[string]string {
	return lo.Assign(billingSettingSnapshot().BillingMode)
}

func GetBillingExprCopy() map[string]string {
	return lo.Assign(billingSettingSnapshot().BillingExpr)
}

func GetPricingSyncData(base map[string]any) map[string]any {
	extra := make(map[string]any, 2)
	setting := billingSettingSnapshot()
	exprs := lo.Assign(setting.BillingExpr)
	modes := lo.Assign(setting.BillingMode)
	for name := range ratio_setting.GetModelRatioCopy() {
		if _, known := knowledge.Lookup(name); !known {
			continue
		}
		if _, exists := exprs[name]; exists {
			continue
		}
		mode, expression, hasExpression := GetBillingRule(name)
		if hasExpression {
			modes[name] = mode
			exprs[name] = expression
		}
	}
	if len(modes) > 0 {
		for model, mode := range modes {
			if expression, ok := exprs[model]; ok {
				modes[model] = NormalizeExpressionBillingMode(mode, expression)
			}
		}
		extra[BillingModeField] = modes
	}
	if len(exprs) > 0 {
		extra[BillingExprField] = exprs
	}
	return lo.Assign(base, extra)
}

func billingSettingSnapshot() BillingSetting {
	return config.MustSnapshot[BillingSetting]("billing_setting")
}

// ---------------------------------------------------------------------------
// Smoke test (called externally for validation before save)
// ---------------------------------------------------------------------------

func SmokeTestExpr(exprStr string) error {
	return smokeTestExpr(exprStr)
}

func smokeTestExpr(exprStr string) error {
	if _, err := billingexpr.CompileFromCache(exprStr); err != nil {
		return err
	}
	// A fixed sample body cannot exercise arbitrary param() paths. Runtime
	// evaluation remains strict and rejects missing or wrongly typed values.
	keys, err := billingexpr.UsedUsageKeys(exprStr)
	if err != nil {
		return err
	}
	// Live usage schema is validated on changed saves and against the exact
	// selected binding at execution. Reads/startup must survive paused plugins.
	if len(keys) > 0 || billingexpr.UsedVars(exprStr)["param"] {
		return nil
	}

	vectors := []billingexpr.TokenParams{
		{P: 0, C: 0, Len: 0},
		{P: 1000, C: 1000, Len: 1000},
		{P: 100000, C: 100000, Len: 100000},
		{P: 1000000, C: 1000000, Len: 1000000},
	}
	requests := []billingexpr.RequestInput{
		{},
		{
			Headers: map[string]string{
				"anthropic-beta": "fast-mode-2026-02-01",
			},
			Body: []byte(`{"service_tier":"fast","stream_options":{"include_usage":true},"messages":[1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21]}`),
		},
	}

	for _, v := range vectors {
		for _, request := range requests {
			result, _, err := billingexpr.RunExprWithRequest(exprStr, v, request)
			if err != nil {
				return fmt.Errorf("vector {p=%g, c=%g}: run failed: %w", v.P, v.C, err)
			}
			if result < 0 {
				return fmt.Errorf("vector {p=%g, c=%g}: result %f < 0", v.P, v.C, result)
			}
		}
	}
	return nil
}

func hasModelPrice(model string) bool {
	_, priced := ratio_setting.GetModelPrice(model, false)
	return priced
}
