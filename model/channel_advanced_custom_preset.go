package model

import (
	"eflow.local/new-api/constant"
	"eflow.local/new-api/dto"
)

// advancedCustomPresetPaths are the OpenAI- and Anthropic-compatible routes
// served by vLLM and SGLang. Every route passes the body through unchanged
// and sends the channel key as a bearer token.
var advancedCustomPresetPaths = []string{
	"/v1/chat/completions",
	"/v1/completions",
	"/v1/responses",
	"/v1/embeddings",
	"/v1/messages",
	dto.AdvancedCustomModelListPath,
}

// AdvancedCustomPreset returns a fresh route preset for channel types that
// are backed by Advanced Custom routes, or nil for every other type. The
// preset replaces any stored advanced_custom value so the routes cannot drift
// from what the named channel type promises.
//
// SGLang's /v1/rerank needs a response converter that eflow does not have
// yet, so rerank is not part of either preset.
func AdvancedCustomPreset(channelType int) *dto.AdvancedCustomConfig {
	if channelType != constant.ChannelTypeVLLM && channelType != constant.ChannelTypeSGLang {
		return nil
	}
	config := &dto.AdvancedCustomConfig{}
	for _, path := range advancedCustomPresetPaths {
		config.Routes = append(config.Routes, dto.AdvancedCustomRoute{
			IncomingPath: path,
			UpstreamPath: path,
			Converter:    dto.AdvancedCustomConverterNone,
			Auth: &dto.AdvancedCustomRouteAuth{
				Type:  dto.AdvancedCustomAuthTypeHeader,
				Name:  "Authorization",
				Value: "Bearer {api_key}",
			},
		})
	}
	return config
}
