package deepseek

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
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
