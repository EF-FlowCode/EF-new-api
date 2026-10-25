package controller

import (
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestValidateChannelRequiresBaseURLForVLLMAndSGLang(t *testing.T) {
	for _, channelType := range []int{constant.ChannelTypeVLLM, constant.ChannelTypeSGLang} {
		name := constant.GetChannelTypeName(channelType)
		t.Run(name, func(t *testing.T) {
			require.ErrorContains(t, validateChannel(&model.Channel{Type: channelType, Key: "k"}, true), name+" 渠道必须填写 API 地址")
			require.ErrorContains(t, validateChannel(&model.Channel{Type: channelType, Key: "k", BaseURL: common.GetPointer(" ")}, true), "API 地址")
			// Updates that omit base_url keep the stored address.
			require.NoError(t, validateChannel(&model.Channel{Type: channelType}, false))
			require.ErrorContains(t, validateChannel(&model.Channel{Type: channelType, BaseURL: common.GetPointer("")}, false), "API 地址")
			require.NoError(t, validateChannel(&model.Channel{Type: channelType, Key: "k", BaseURL: common.GetPointer("http://127.0.0.1:8000")}, true))
		})
	}
}
