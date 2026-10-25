package deepseek

import (
	"testing"

	"eflow.local/new-api/dto"
	"eflow.local/new-api/internal/testconfig"
	relaycommon "eflow.local/new-api/relay/common"
)

func TestApplyDeepSeekV4ResponsesThinkingSuffix(t *testing.T) {
	for _, test := range []struct {
		name   string
		model  string
		effort string
	}{
		{name: "disable thinking", model: "deepseek-v4-0324-none", effort: "none"},
		{name: "maximum thinking", model: "deepseek-v4-0324-max", effort: "max"},
	} {
		t.Run(test.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: test.model}}
			request := dto.OpenAIResponsesRequest{Model: test.model}

			applyDeepSeekV4ResponsesThinkingSuffix(info, &request)

			if request.Model != "deepseek-v4-0324" || request.Reasoning == nil || request.Reasoning.Effort != test.effort {
				t.Fatalf("unexpected request after suffix conversion: %+v", request)
			}
			if info.UpstreamModelName != "deepseek-v4-0324" || info.ReasoningEffort != test.effort {
				t.Fatalf("unexpected relay info after suffix conversion: %+v", info)
			}
		})
	}
}

func TestDeepSeekV4SuffixKeptForReservedModel(t *testing.T) {
	testconfig.Set(t, "global", map[string]string{"thinking_model_blacklist": `["deepseek-v4-0324-max"]`})

	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "deepseek-v4-0324-max"}}
	chat := &dto.GeneralOpenAIRequest{Model: "deepseek-v4-0324-max"}
	if err := applyDeepSeekV4OpenAIThinkingSuffix(info, chat); err != nil {
		t.Fatal(err)
	}
	responses := dto.OpenAIResponsesRequest{Model: "deepseek-v4-0324-max"}
	applyDeepSeekV4ResponsesThinkingSuffix(info, &responses)

	if chat.Model != "deepseek-v4-0324-max" || chat.ReasoningEffort != "" || responses.Model != "deepseek-v4-0324-max" || responses.Reasoning != nil {
		t.Fatalf("reserved model suffix must pass through: chat=%+v responses=%+v", chat, responses)
	}
	if info.UpstreamModelName != "deepseek-v4-0324-max" {
		t.Fatalf("upstream model name changed: %q", info.UpstreamModelName)
	}
}
