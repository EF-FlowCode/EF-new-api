package billing_setting

import (
	"eflow.local/new-api/common"
	"encoding/json"
	"github.com/stretchr/testify/require"
	"math"
	"testing"
)

func TestTaskPriceRulePreservesUnitArithmetic(t *testing.T) {
	for _, price := range []float64{0, 0.0000019, 0.000003, 0.0000012, 0.019999999999999997, 3.9} {
		r := TaskPriceRule{Version: 1, Kind: "unit_price", UnitPrice: &price, UpstreamModel: "happyhorse-1.0-t2v", Dimensions: "provider_v1", Settlement: "submit"}
		require.NoError(t, r.ValidateFor("happyhorse", r.UpstreamModel))
		for _, q := range []float64{500000, 1000000, 123456.7} {
			for _, group := range []float64{0, 0.1, 0.3, 1, 2, 17.25} {
				want, err := common.QuotaFromFloatStrict(price * q * group)
				require.NoError(t, err)
				got, err := r.BaseQuota(q, group)
				require.NoError(t, err)
				require.Equal(t, want, got)
			}
		}
	}
}

func TestTaskPriceRuleJSONAndRejections(t *testing.T) {
	var r TaskPriceRule
	require.NoError(t, json.Unmarshal([]byte(`"v2:0.01"`), &r))
	raw, err := json.Marshal(r)
	require.NoError(t, err)
	require.JSONEq(t, `{"version":1,"kind":"amount","expression":"v2:0.01"}`, string(raw))
	for _, raw := range []string{`null`, `{}`, `{"version":2,"kind":"amount","expression":"v2:1"}`, `{"version":1,"kind":"amount","expression":"v2:1","unknown":1}`, `{"version":1,"kind":"unit_price","unit_price":0,"upstream_model":"m","dimensions":"provider_v1","settlement":"tokens"}`, `{"version":1,"kind":"amount","expression":"v2:1","unit_price":0}`} {
		require.Error(t, json.Unmarshal([]byte(raw), &r), raw)
	}
	zero := 0.0
	r = TaskPriceRule{Version: 1, Kind: "unit_price", UnitPrice: &zero, UpstreamModel: "wan3.0-video", Dimensions: "provider_v1", Settlement: "wan_duration"}
	require.NoError(t, r.ValidateFor("alibaba", "wan3.0-video"))
	require.Error(t, r.ValidateFor("alibaba", "wan2.2-s2v"))
	require.Error(t, r.ValidateFor("sora", "wan3.0-video"))
	for _, v := range []float64{-1, math.Inf(1), math.NaN()} {
		r.UnitPrice = &v
		require.Error(t, r.Validate())
	}
}

func TestTaskPriceRulePreservesTokenReservation(t *testing.T) {
	configured := true
	for _, ratio := range []float64{0, 0.1234567890123456, 0.5, 37.5} {
		settlement := "tokens"
		if ratio == 0 {
			settlement = "submit"
		}
		r := TaskPriceRule{Version: 1, Kind: "token_completion", ModelRatio: &ratio, RatioConfigured: &configured, UpstreamModel: "seedance", Dimensions: "provider_v1", Settlement: settlement}
		require.NoError(t, r.ValidateFor("doubao", "seedance"))
		raw, err := json.Marshal(r)
		require.NoError(t, err)
		var copy TaskPriceRule
		require.NoError(t, json.Unmarshal(raw, &copy))
		for _, group := range []float64{0, 0.3, 1, 2} {
			for _, q := range []float64{500000, 123456.7} {
				want, err := common.QuotaFromFloatStrict(ratio / 2 * q * group)
				require.NoError(t, err)
				got, err := copy.BaseQuota(q, group)
				require.NoError(t, err)
				require.Equal(t, want, got)
			}
		}
	}
}
