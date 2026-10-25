package dto

import (
	"testing"

	"eflow.local/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestGeneralOpenAIRequestPreservesVLLMThinkingTokenBudget(t *testing.T) {
	var request GeneralOpenAIRequest
	require.NoError(t, common.Unmarshal([]byte(`{"model":"test","thinking_token_budget":-1}`), &request))

	body, err := common.Marshal(request)
	require.NoError(t, err)
	require.JSONEq(t, `{"model":"test","thinking_token_budget":-1}`, string(body))
}
