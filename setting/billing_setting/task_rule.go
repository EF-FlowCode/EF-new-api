package billing_setting

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"

	"eflow.local/new-api/common"
)

// TaskPriceRule is a versioned calculation contract, not a second price store.
// Amount expressions always return pre-group USD. Legacy unit/token rules keep
// their original operands and truncation points instead of rounding a total.
type TaskPriceRule struct {
	Version         int      `json:"version"`
	Kind            string   `json:"kind"`
	Expression      string   `json:"expression,omitempty"`
	UnitPrice       *float64 `json:"unit_price,omitempty"`
	ModelRatio      *float64 `json:"model_ratio,omitempty"`
	RatioConfigured *bool    `json:"ratio_configured,omitempty"`
	UpstreamModel   string   `json:"upstream_model,omitempty"`
	Dimensions      string   `json:"dimensions,omitempty"` // none or provider_v1
	Settlement      string   `json:"settlement,omitempty"` // submit, wan_duration, tokens
}

// Old imports remain accepted, but all writes emit the structured form.
func (r *TaskPriceRule) UnmarshalJSON(raw []byte) error {
	var expression string
	if json.Unmarshal(raw, &expression) == nil && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		*r = TaskPriceRule{Version: 1, Kind: "amount", Expression: expression}
		return r.Validate()
	}
	type plain TaskPriceRule
	var candidate plain
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&candidate); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("trailing task rule data")
	}
	*r = TaskPriceRule(candidate)
	return r.Validate()
}

func (r TaskPriceRule) Validate() error {
	if r.Version != 1 {
		return fmt.Errorf("unsupported task price rule version %d", r.Version)
	}
	if r.Kind == "amount" {
		if r.UnitPrice != nil || r.ModelRatio != nil || r.RatioConfigured != nil || r.UpstreamModel != "" || r.Dimensions != "" || r.Settlement != "" {
			return fmt.Errorf("amount rule contains legacy calculation fields")
		}
		if !strings.HasPrefix(r.Expression, "v2:") || strings.TrimSpace(r.Expression[3:]) == "" {
			return fmt.Errorf("task amount rule requires a nonempty v2 expression")
		}
		return SmokeTestExpr(r.Expression)
	}
	if r.Expression != "" || r.UpstreamModel == "" || (r.Dimensions != "none" && r.Dimensions != "provider_v1") {
		return fmt.Errorf("incomplete task unit contract")
	}
	var value *float64
	switch r.Kind {
	case "unit_price":
		value = r.UnitPrice
		if r.ModelRatio != nil || r.RatioConfigured != nil || (r.Settlement != "submit" && r.Settlement != "wan_duration") {
			return fmt.Errorf("invalid task unit settlement")
		}
	case "token_completion":
		value = r.ModelRatio
		if r.UnitPrice != nil || r.RatioConfigured == nil || (r.Settlement != "submit" && r.Settlement != "tokens") {
			return fmt.Errorf("invalid task token settlement")
		}
	default:
		return fmt.Errorf("unknown task price rule kind %q", r.Kind)
	}
	if value == nil || *value < 0 || math.IsNaN(*value) || math.IsInf(*value, 0) {
		return fmt.Errorf("task price must be a finite nonnegative number")
	}
	return nil
}

func TaskUsesWANCompletion(plugin, model string) bool {
	return plugin == "alibaba" && (model == "wan2.2-s2v" || model == "wan3.0-video" || model == "wan3.0-video-prime")
}

// ValidateFor locks a legacy conversion to the provider/model that proved it.
// A later rebind to another tail must be priced explicitly, never guessed.
func (r TaskPriceRule) ValidateFor(plugin, model string) error {
	if err := r.Validate(); err != nil {
		return err
	}
	if r.Kind == "amount" {
		return nil
	}
	if r.UpstreamModel != model {
		return fmt.Errorf("task price targets %s, not %s", r.UpstreamModel, model)
	}
	switch plugin {
	case "alibaba", "happyhorse", "google", "vertex-ai", "sora", "doubao", "hailuo", "jimeng", "kling", "vidu", "sunoapi":
	default:
		return fmt.Errorf("unsupported task price provider %q", plugin)
	}
	wan := TaskUsesWANCompletion(plugin, model)
	if r.Settlement == "wan_duration" && (!wan || r.Dimensions != "provider_v1") {
		return fmt.Errorf("invalid WAN task contract")
	}
	if wan && r.Dimensions == "provider_v1" && r.Settlement != "wan_duration" {
		return fmt.Errorf("WAN duration price requires completion settlement")
	}
	if r.Kind == "token_completion" {
		wantTokens := r.Dimensions == "provider_v1" && *r.RatioConfigured && *r.ModelRatio != 0
		if (r.Settlement == "tokens") != wantTokens {
			return fmt.Errorf("inconsistent token settlement")
		}
	}
	return nil
}

// BaseQuota preserves the legacy multiplication order before provider ratios.
func (r TaskPriceRule) BaseQuota(quotaPerUnit, group float64) (int, error) {
	if err := r.Validate(); err != nil {
		return 0, err
	}
	if quotaPerUnit <= 0 || group < 0 || math.IsNaN(quotaPerUnit) || math.IsInf(quotaPerUnit, 0) || math.IsNaN(group) || math.IsInf(group, 0) {
		return 0, fmt.Errorf("invalid task quota or group multiplier")
	}
	switch r.Kind {
	case "unit_price":
		return common.QuotaFromFloatStrict(*r.UnitPrice * quotaPerUnit * group)
	case "token_completion":
		return common.QuotaFromFloatStrict(*r.ModelRatio / 2 * quotaPerUnit * group)
	default:
		return 0, fmt.Errorf("amount rules require expression evaluation")
	}
}
