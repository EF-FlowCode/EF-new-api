package dto

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGeneralOpenAIRequestPreservesVLLMThinkingTokenBudget(t *testing.T) {
	var request GeneralOpenAIRequest
	require.NoError(t, json.Unmarshal([]byte(`{"model":"test","thinking_token_budget":128}`), &request))

	body, err := json.Marshal(request)
	require.NoError(t, err)
	require.JSONEq(t, `{"model":"test","thinking_token_budget":128}`, string(body))
}
