package dto

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStripAnthropicBillingHeaderFromStringSystem(t *testing.T) {
	req := &ClaudeRequest{
		System: "x-anthropic-billing-header: cc_version=2.1.117.48f; cc_entrypoint=cli; cch=71fea;\nYou are helpful.",
	}

	require.True(t, req.StripAnthropicBillingHeaderFromSystem())
	require.Equal(t, "You are helpful.", req.System)
}

func TestStripAnthropicBillingHeaderFromSystemBlocks(t *testing.T) {
	keep := "Project instructions"
	header := "x-anthropic-billing-header: cc_version=2.1.117.48f; cc_entrypoint=cli; cch=71fea;"
	req := &ClaudeRequest{
		System: []ClaudeMediaMessage{
			{Type: ContentTypeText, Text: &header},
			{Type: ContentTypeText, Text: &keep},
		},
	}

	require.True(t, req.StripAnthropicBillingHeaderFromSystem())
	systems := req.ParseSystem()
	require.Len(t, systems, 1)
	require.Equal(t, keep, systems[0].GetText())
}

func TestStripAnthropicBillingHeaderPreservesNormalSystem(t *testing.T) {
	req := &ClaudeRequest{System: "You are helpful."}

	require.False(t, req.StripAnthropicBillingHeaderFromSystem())
	require.Equal(t, "You are helpful.", req.System)
}
