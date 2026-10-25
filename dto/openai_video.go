package dto

import relaydto "eflow.local/new-api/relaykit/dto"

const (
	VideoStatusUnknown    = relaydto.VideoStatusUnknown
	VideoStatusQueued     = relaydto.VideoStatusQueued
	VideoStatusInProgress = relaydto.VideoStatusInProgress
	VideoStatusCompleted  = relaydto.VideoStatusCompleted
	VideoStatusFailed     = relaydto.VideoStatusFailed
)

type OpenAIVideo = relaydto.OpenAIVideo
type OpenAIVideoError = relaydto.OpenAIVideoError

func NewOpenAIVideo() *OpenAIVideo {
	return relaydto.NewOpenAIVideo()
}
