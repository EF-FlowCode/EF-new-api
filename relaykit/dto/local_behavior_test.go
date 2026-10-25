package dto

import (
	"testing"

	kitutil "eflow.local/new-api/relaykit/relayconvert/kitutil"
	"eflow.local/new-api/relaykit/types"
	"github.com/stretchr/testify/require"
)

func TestStripAnthropicBillingHeaderFromSystem(t *testing.T) {
	header := "x-anthropic-billing-header: cc_version=2.1; cch=71fea;"
	keep := "Project instructions"
	tests := []struct {
		name   string
		system any
		want   any
	}{
		{"string", header + "\nYou are helpful.", "You are helpful."},
		{"blocks", []ClaudeMediaMessage{{Type: ContentTypeText, Text: &header}, {Type: ContentTypeText, Text: &keep}}, []ClaudeMediaMessage{{Type: ContentTypeText, Text: &keep}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := &ClaudeRequest{System: test.system}
			require.True(t, req.StripAnthropicBillingHeaderFromSystem())
			require.Equal(t, test.want, req.System)
		})
	}
}

func TestLocalRelayFields(t *testing.T) {
	var details InputTokenDetails
	require.NoError(t, kitutil.Unmarshal([]byte(`{"video_tokens":3586}`), &details))
	require.Equal(t, 3586, details.VideoTokens)

	response := OpenAIResponsesResponse{Output: []ResponsesOutput{{Type: ResponsesOutputTypeImageGenerationCall, Quality: "high", Size: "1024x1024"}}}
	require.True(t, response.HasImageGenerationCall())
	require.Equal(t, "high", response.GetQuality())
	require.Equal(t, "1024x1024", response.GetSize())

	source := (&MediaContent{Type: ContentTypeFile, File: &MessageFile{FileName: "sample.wav", FileData: "ZGF0YQ=="}}).ToFileSource()
	base64Source, ok := source.(*types.Base64Source)
	require.True(t, ok)
	require.Equal(t, "audio/wav", base64Source.MimeType)
}
