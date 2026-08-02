package billing_setting

import (
	"fmt"

	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/samber/lo"
)

const (
	BillingModeRatio      = "ratio"
	BillingModeTieredExpr = "tiered_expr"
	// eflow: 仅作模型广场展示标记——结算仍走 ratio（ModelRatio）。
	// 用于阿里 ASR 这类「按音频秒数」计费的模型：内部把秒数当 token、
	// ModelRatio 编码 元/1M秒，前端识别此 mode 后把价格行渲染成「¥X/秒」。
	BillingModeAudioSeconds = "audio_seconds"
	BillingModeField        = "billing_mode"
	BillingExprField        = "billing_expr"
)

// BillingSetting is managed by config.GlobalConfig.Register.
// DB keys: billing_setting.billing_mode, billing_setting.billing_expr
type BillingSetting struct {
	BillingMode map[string]string `json:"billing_mode"`
	BillingExpr map[string]string `json:"billing_expr"`
}

var billingSetting = BillingSetting{
	BillingMode: make(map[string]string),
	BillingExpr: make(map[string]string),
}

func init() {
	config.GlobalConfig.Register("billing_setting", &billingSetting)
}

// ---------------------------------------------------------------------------
// Read accessors (hot path, must be fast)
// ---------------------------------------------------------------------------

func GetBillingMode(model string) string {
	mode := BillingModeRatio
	config.GlobalConfig.Read("billing_setting", func(cfg interface{}) {
		if setting, ok := cfg.(*BillingSetting); ok {
			if configured, exists := setting.BillingMode[model]; exists {
				mode = configured
			}
		}
	})
	return mode
}

func GetBillingExpr(model string) (string, bool) {
	var expr string
	var exists bool
	config.GlobalConfig.Read("billing_setting", func(cfg interface{}) {
		if setting, ok := cfg.(*BillingSetting); ok {
			expr, exists = setting.BillingExpr[model]
		}
	})
	return expr, exists
}

func GetBillingModeCopy() map[string]string {
	result := map[string]string{}
	config.GlobalConfig.Read("billing_setting", func(cfg interface{}) {
		if setting, ok := cfg.(*BillingSetting); ok {
			result = lo.Assign(setting.BillingMode)
		}
	})
	return result
}

func GetBillingExprCopy() map[string]string {
	result := map[string]string{}
	config.GlobalConfig.Read("billing_setting", func(cfg interface{}) {
		if setting, ok := cfg.(*BillingSetting); ok {
			result = lo.Assign(setting.BillingExpr)
		}
	})
	return result
}

func GetPricingSyncData(base map[string]any) map[string]any {
	extra := make(map[string]any, 2)
	if modes := GetBillingModeCopy(); len(modes) > 0 {
		extra[BillingModeField] = modes
	}
	if exprs := GetBillingExprCopy(); len(exprs) > 0 {
		extra[BillingExprField] = exprs
	}
	return lo.Assign(base, extra)
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
	if billingexpr.UsedVars(exprStr)["param"] {
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
