package openai

import (
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestReconcileOaiImageCountProtectsClientAbortBilling(t *testing.T) {
	newInfo := func(requested float64) *relaycommon.RelayInfo {
		return &relaycommon.RelayInfo{PriceData: types.PriceData{
			UsePrice:    true,
			OtherRatios: map[string]float64{"n": requested},
		}}
	}

	info := newInfo(3)
	reconcileOaiImageCount(info, 1, false)
	require.Equal(t, 3.0, info.PriceData.OtherRatios["n"])

	info = newInfo(1)
	reconcileOaiImageCount(info, 2, false)
	require.Equal(t, 2.0, info.PriceData.OtherRatios["n"])

	info = newInfo(3)
	reconcileOaiImageCount(info, 2, true)
	require.Equal(t, 2.0, info.PriceData.OtherRatios["n"])
}
