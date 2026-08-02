package dto

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestImageRequestBillingRatiosUseRequestedCount(t *testing.T) {
	three := uint(3)
	request := ImageRequest{N: &three}
	require.Equal(t, 3.0, request.GetTokenCountMeta().BillingRatios["n"])

	zero := uint(0)
	request.N = &zero
	require.Equal(t, 1.0, request.GetTokenCountMeta().BillingRatios["n"])

	request.N = nil
	require.Equal(t, 1.0, request.GetTokenCountMeta().BillingRatios["n"])
}
