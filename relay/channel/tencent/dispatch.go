package tencent

import (
	"strings"

	"eflow.local/new-api/constant"
	"eflow.local/new-api/relay/channel"
	"eflow.local/new-api/relay/channel/openai"
	relaycommon "eflow.local/new-api/relay/common"
)

const tokenHubBaseURL = "https://tokenhub.tencentmaas.com"

// DispatchAdaptor keeps legacy TC3 keys working while routing TokenHub keys
// through Tencent's OpenAI-compatible endpoint.
type DispatchAdaptor struct {
	channel.Adaptor
}

func (a *DispatchAdaptor) Init(info *relaycommon.RelayInfo) {
	if strings.Contains(info.ApiKey, "|") {
		a.Adaptor = &Adaptor{}
	} else {
		a.Adaptor = &openai.Adaptor{}
		if info.ChannelBaseUrl == "" || info.ChannelBaseUrl == constant.GetChannelBaseURL(constant.ChannelTypeTencent) {
			info.ChannelBaseUrl = tokenHubBaseURL
		}
	}
	a.Adaptor.Init(info)
}

func (a *DispatchAdaptor) GetModelList() []string {
	return ModelList
}

func (a *DispatchAdaptor) GetChannelName() string {
	return ChannelName
}
