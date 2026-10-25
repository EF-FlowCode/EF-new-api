package dto

import relaydto "eflow.local/new-api/relaykit/dto"

const (
	RealtimeEventTypeError              = relaydto.RealtimeEventTypeError
	RealtimeEventTypeSessionUpdate      = relaydto.RealtimeEventTypeSessionUpdate
	RealtimeEventTypeConversationCreate = relaydto.RealtimeEventTypeConversationCreate
	RealtimeEventTypeResponseCreate     = relaydto.RealtimeEventTypeResponseCreate
	RealtimeEventInputAudioBufferAppend = relaydto.RealtimeEventInputAudioBufferAppend

	RealtimeEventTypeResponseDone                   = relaydto.RealtimeEventTypeResponseDone
	RealtimeEventTypeSessionUpdated                 = relaydto.RealtimeEventTypeSessionUpdated
	RealtimeEventTypeSessionCreated                 = relaydto.RealtimeEventTypeSessionCreated
	RealtimeEventResponseAudioDelta                 = relaydto.RealtimeEventResponseAudioDelta
	RealtimeEventResponseAudioTranscriptionDelta    = relaydto.RealtimeEventResponseAudioTranscriptionDelta
	RealtimeEventResponseFunctionCallArgumentsDelta = relaydto.RealtimeEventResponseFunctionCallArgumentsDelta
	RealtimeEventResponseFunctionCallArgumentsDone  = relaydto.RealtimeEventResponseFunctionCallArgumentsDone
	RealtimeEventConversationItemCreated            = relaydto.RealtimeEventConversationItemCreated
)

type RealtimeEvent = relaydto.RealtimeEvent
type RealtimeResponse = relaydto.RealtimeResponse
type RealtimeUsage = relaydto.RealtimeUsage
type RealtimeSession = relaydto.RealtimeSession
type InputAudioTranscription = relaydto.InputAudioTranscription
type RealTimeTool = relaydto.RealTimeTool
type RealtimeItem = relaydto.RealtimeItem
type RealtimeContent = relaydto.RealtimeContent
