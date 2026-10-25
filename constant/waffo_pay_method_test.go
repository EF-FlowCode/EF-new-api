package constant

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultWaffoPayMethodsReturnsFreshSlice(t *testing.T) {
	first := DefaultWaffoPayMethods()
	require.NotEmpty(t, first)
	first[0].Name = "mutated"

	second := DefaultWaffoPayMethods()
	require.Equal(t, "Card", second[0].Name)
}
