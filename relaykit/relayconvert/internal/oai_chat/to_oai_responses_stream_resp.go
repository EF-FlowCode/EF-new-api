package oaichat

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"eflow.local/new-api/relaykit/dto"
	"eflow.local/new-api/relaykit/relayconvert/convmeta"
	kitutil "eflow.local/new-api/relaykit/relayconvert/kitutil"
)

type ChatToResponsesStreamEvent struct {
	Type    string
	Payload dto.ResponsesStreamResponse
}

type ChatToResponsesStreamState struct {
	ID      string
	Model   string
	Created int64
	Usage   *dto.Usage

	// EmitSequenceNumber enables the required sequence_number field for current
	// Responses API SSE consumers while preserving the legacy relaykit default.
	EmitSequenceNumber bool
	// DroppedToolDeltas counts tool-call deltas ignored because they arrived
	// for a call that was already completed; the host logs it.
	DroppedToolDeltas int
	// Tools marks the Chat function calls that encode Responses custom tools;
	// they are streamed back as custom_tool_call items.
	Tools *convmeta.ResponsesToolState

	status             string
	incompleteDetails  *dto.IncompleteDetails
	sentCreated        bool
	textOutputIndex    int
	textStarted        bool
	textDone           bool
	reasoningIndex     int
	reasoningStarted   bool
	reasoningDone      bool
	finalized          bool
	nextSequenceNumber int
	nextOutputIndex    int
	// toolsByIndex is keyed by tool slot. A slot equals the Chat tool index
	// unless that index is reused by another call (different id or name, as
	// sent by upstreams that omit index on parallel tool_calls); toolSlots maps
	// each Chat index to the slot of its latest call.
	toolsByIndex map[int]*chatToResponsesStreamTool
	toolSlots    map[int]int
	outputOrder  []chatToResponsesOutputRef
	text         strings.Builder
	reasoning    strings.Builder
	// A mid-stream finish_reason closes the message and reasoning items. Deltas that
	// arrive afterwards open a fresh item (next segment ordinal) instead of touching the
	// closed one; the closed items are snapshotted so the final response keeps them.
	textSegment      int
	reasoningSegment int
	closedMessages   []dto.ResponsesOutput
	closedReasonings []dto.ResponsesOutput
}

type chatToResponsesStreamTool struct {
	ChatIndex   int
	Slot        int
	OutputIndex int
	ItemID      string
	CallID      string
	Name        string
	Arguments   strings.Builder
	// Announced records that output_item.added was sent. Custom reports that
	// the call restores a Responses custom tool; it is fixed at announcement.
	Announced bool
	Custom    bool
	Done      bool
}

type chatToResponsesOutputRef struct {
	Kind      string
	Segment   int
	ToolIndex int
}

func NewChatToResponsesStreamState(id string, model string) *ChatToResponsesStreamState {
	return &ChatToResponsesStreamState{
		ID:              id,
		Model:           model,
		Created:         time.Now().Unix(),
		Usage:           &dto.Usage{},
		status:          "completed",
		textOutputIndex: -1,
		reasoningIndex:  -1,
		toolsByIndex:    make(map[int]*chatToResponsesStreamTool),
		toolSlots:       make(map[int]int),
	}
}

func (s *ChatToResponsesStreamState) StreamUsage() *dto.Usage {
	if s == nil {
		return nil
	}
	return s.Usage
}

func (s *ChatToResponsesStreamState) SetStreamUsage(usage *dto.Usage) {
	if s != nil && usage != nil {
		s.Usage = UsageFromChatUsage(usage)
	}
}

func ChatCompletionsStreamChunkToResponsesEvents(chunk *dto.ChatCompletionsStreamResponse, state *ChatToResponsesStreamState) ([]ChatToResponsesStreamEvent, error) {
	if chunk == nil || state == nil {
		return nil, nil
	}
	if state.ID == "" {
		state.ID = chunk.Id
	}
	if state.Model == "" {
		state.Model = chunk.Model
	}
	if state.Created == 0 {
		state.Created = chunk.Created
	}
	if chunk.Usage != nil {
		state.Usage = UsageFromChatUsage(chunk.Usage)
	}

	events := state.ensureCreated()
	for _, choice := range chunk.Choices {
		if choice.Delta.GetReasoningContent() != "" {
			events = append(events, state.appendReasoningDelta(choice.Delta.GetReasoningContent())...)
		}
		if choice.Delta.GetContentString() != "" {
			events = append(events, state.appendTextDelta(choice.Delta.GetContentString())...)
		}
		for _, toolCall := range choice.Delta.ToolCalls {
			events = append(events, state.appendToolCallDelta(toolCall)...)
		}
		if choice.FinishReason != nil && strings.TrimSpace(*choice.FinishReason) != "" {
			state.applyFinishReason(*choice.FinishReason)
			events = append(events, state.doneDeltaEvents()...)
		}
	}
	return events, nil
}

func (s *ChatToResponsesStreamState) ensureCreated() []ChatToResponsesStreamEvent {
	if s.sentCreated {
		return nil
	}
	s.sentCreated = true
	return []ChatToResponsesStreamEvent{s.event(responsesEventCreated, dto.ResponsesStreamResponse{
		Type:     responsesEventCreated,
		Response: s.createdResponse(),
	})}
}

func FinalizeChatCompletionsStreamToResponses(state *ChatToResponsesStreamState) []ChatToResponsesStreamEvent {
	if state == nil || state.finalized {
		return nil
	}
	events := state.doneDeltaEvents()
	state.finalized = true
	resp := state.finalResponse()
	eventType := responsesEventCompleted
	if state.status == "incomplete" {
		eventType = responsesEventIncomplete
	}
	events = append(events, state.event(eventType, dto.ResponsesStreamResponse{
		Type:     eventType,
		Response: resp,
	}))
	return events
}

// MarkIncomplete records that the upstream stream was cut short after output
// reached the client, so finalization reports response.incomplete instead of
// response.completed. An incomplete status derived from finish_reason wins.
func (s *ChatToResponsesStreamState) MarkIncomplete(reason string) {
	if s == nil || s.status == "incomplete" {
		return
	}
	s.status = "incomplete"
	s.incompleteDetails = &dto.IncompleteDetails{Reason: reason}
}

func (s *ChatToResponsesStreamState) UsageText() string {
	if s == nil {
		return ""
	}
	return s.text.String()
}

func (s *ChatToResponsesStreamState) appendTextDelta(delta string) []ChatToResponsesStreamEvent {
	events := s.closeReasoning(s.outputStatus())
	events = append(events, s.startText()...)
	s.text.WriteString(delta)
	events = append(events, s.event(responsesEventOutputTextDelta, dto.ResponsesStreamResponse{
		Type:         responsesEventOutputTextDelta,
		OutputIndex:  intPtr(s.textOutputIndex),
		ContentIndex: intPtr(0),
		Delta:        delta,
		ItemID:       s.messageID(),
	}))
	return events
}

func (s *ChatToResponsesStreamState) startText() []ChatToResponsesStreamEvent {
	if s.textDone {
		s.textSegment++
		s.textStarted = false
		s.textDone = false
		s.text.Reset()
	}
	if !s.textStarted {
		s.textStarted = true
		s.textOutputIndex = s.nextIndex(chatToResponsesOutputRef{Kind: "message", Segment: s.textSegment})
		return []ChatToResponsesStreamEvent{
			s.event(responsesEventOutputItemAdded, dto.ResponsesStreamResponse{
				Type:        responsesEventOutputItemAdded,
				OutputIndex: intPtr(s.textOutputIndex),
				Item: &dto.ResponsesOutput{
					Type:    responsesOutputTypeMessage,
					ID:      s.messageID(),
					Status:  "in_progress",
					Role:    "assistant",
					Content: []dto.ResponsesOutputContent{},
				},
			}),
			// SDK stream accumulators append this part to content and index
			// output_text deltas into it by content_index.
			s.event(responsesEventContentPartAdded, dto.ResponsesStreamResponse{
				Type:         responsesEventContentPartAdded,
				OutputIndex:  intPtr(s.textOutputIndex),
				ContentIndex: intPtr(0),
				ItemID:       s.messageID(),
				Part:         outputTextPart(""),
			}),
		}
	}
	return nil
}

func (s *ChatToResponsesStreamState) appendReasoningDelta(delta string) []ChatToResponsesStreamEvent {
	events := make([]ChatToResponsesStreamEvent, 0, 3)
	if s.reasoningDone {
		s.reasoningSegment++
		s.reasoningStarted = false
		s.reasoningDone = false
		s.reasoning.Reset()
	}
	if !s.reasoningStarted {
		s.reasoningStarted = true
		s.reasoningIndex = s.nextIndex(chatToResponsesOutputRef{Kind: "reasoning", Segment: s.reasoningSegment})
		events = append(events, s.event(responsesEventOutputItemAdded, dto.ResponsesStreamResponse{
			Type:        responsesEventOutputItemAdded,
			OutputIndex: intPtr(s.reasoningIndex),
			Item: &dto.ResponsesOutput{
				Type:    responsesOutputTypeReasoning,
				ID:      s.reasoningID(),
				Status:  "in_progress",
				Summary: []dto.ResponsesReasoningSummaryPart{},
			},
		}))
		events = append(events, s.event(responsesEventReasoningSummaryPartAdded, dto.ResponsesStreamResponse{
			Type:         responsesEventReasoningSummaryPartAdded,
			OutputIndex:  intPtr(s.reasoningIndex),
			SummaryIndex: intPtr(0),
			ItemID:       s.reasoningID(),
			Part:         &dto.ResponsesReasoningSummaryPart{Type: "summary_text"},
		}))
	}
	s.reasoning.WriteString(delta)
	events = append(events, s.event(responsesEventReasoningSummaryDelta, dto.ResponsesStreamResponse{
		Type:         responsesEventReasoningSummaryDelta,
		OutputIndex:  intPtr(s.reasoningIndex),
		SummaryIndex: intPtr(0),
		Delta:        delta,
		ItemID:       s.reasoningID(),
	}))
	return events
}

func (s *ChatToResponsesStreamState) appendToolCallDelta(toolCall dto.ToolCallResponse) []ChatToResponsesStreamEvent {
	chatIndex := 0
	if toolCall.Index != nil {
		chatIndex = *toolCall.Index
	}
	incomingID := strings.TrimSpace(toolCall.ID)
	incomingName := strings.TrimSpace(toolCall.Function.Name)
	tool := s.currentTool(chatIndex)
	if tool == nil || tool.startsNewCall(incomingID, incomingName) {
		tool = s.newTool(chatIndex, incomingID)
	}
	if tool.Done {
		// A late delta for a call that was already closed (e.g. after
		// finish_reason) cannot change emitted output; drop it rather than
		// aborting the whole stream.
		s.DroppedToolDeltas++
		return nil
	}
	events := make([]ChatToResponsesStreamEvent, 0, 2)
	if incomingID != "" {
		tool.CallID = incomingID
		if !tool.Announced {
			tool.ItemID = incomingID
		}
	}
	// A conflicting name on the same id cannot rename an announced item; the
	// first name wins.
	if incomingName != "" && tool.Name == "" {
		tool.Name = incomingName
	}
	delta := toolCall.Function.Arguments
	tool.Arguments.WriteString(delta)
	// A nameless call is invalid Responses output, so hold it back until a
	// valid name arrives. Announcement also determines whether it is custom.
	if !tool.Announced && tool.Name != "" {
		events = append(events, s.closeReasoning(s.outputStatus())...)
		events = append(events, s.announceTool(tool))
		delta = tool.Arguments.String()
	}
	// Custom tool input is unwrapped from the complete arguments at the end.
	if tool.Announced && !tool.Custom && delta != "" {
		events = append(events, s.closeReasoning(s.outputStatus())...)
		events = append(events, s.event(responsesEventFunctionArgsDelta, dto.ResponsesStreamResponse{
			Type:        responsesEventFunctionArgsDelta,
			OutputIndex: intPtr(tool.OutputIndex),
			ItemID:      tool.ItemID,
			Delta:       delta,
		}))
	}
	return events
}

func (s *ChatToResponsesStreamState) currentTool(chatIndex int) *chatToResponsesStreamTool {
	slot, ok := s.toolSlots[chatIndex]
	if !ok {
		return nil
	}
	return s.toolsByIndex[slot]
}

// newTool opens a call for chatIndex in a fresh slot, keeping the Chat index
// as the slot when it is still free.
func (s *ChatToResponsesStreamState) newTool(chatIndex int, id string) *chatToResponsesStreamTool {
	slot := chatIndex
	if _, used := s.toolsByIndex[slot]; used {
		for existing := range s.toolsByIndex {
			if existing >= slot {
				slot = existing + 1
			}
		}
	}
	tool := &chatToResponsesStreamTool{ChatIndex: chatIndex, Slot: slot, CallID: id, ItemID: id}
	if tool.ItemID == "" {
		tool.ItemID = fmt.Sprintf("%s_call_%d", s.ID, slot)
	}
	s.toolsByIndex[slot] = tool
	s.toolSlots[chatIndex] = slot
	return tool
}

// startsNewCall reports whether a delta begins another call on the same Chat
// index: a different id, or (for id-less deltas) a different function name.
// Upstreams that omit index on parallel tool_calls (seen on Gemini's
// OpenAI-compatible endpoint) send every call as index 0. After completion
// any new id or name also opens a new call.
func (t *chatToResponsesStreamTool) startsNewCall(id string, name string) bool {
	if id != "" {
		return id != t.CallID && (t.CallID != "" || t.Done)
	}
	return name != "" && ((t.Name != "" && name != t.Name) || (t.Done && t.Name == ""))
}

// announceTool also allocates the output index, so a held tool never ends up
// behind items that were added to the stream before it.
func (s *ChatToResponsesStreamState) announceTool(tool *chatToResponsesStreamTool) ChatToResponsesStreamEvent {
	tool.Announced = true
	tool.OutputIndex = s.nextIndex(chatToResponsesOutputRef{Kind: "tool", ToolIndex: tool.Slot})
	tool.Custom = s.Tools.IsCustomTool(tool.Name)
	item := &dto.ResponsesOutput{
		Type:      responsesOutputTypeFunctionCall,
		ID:        tool.ItemID,
		Status:    "in_progress",
		CallId:    tool.callID(),
		Name:      tool.Name,
		Arguments: []byte(`""`),
	}
	if tool.Custom {
		item.Type = responsesOutputTypeCustomToolCall
		item.Arguments = nil
		item.Input = []byte(`""`)
	}
	return s.event(responsesEventOutputItemAdded, dto.ResponsesStreamResponse{
		Type:        responsesEventOutputItemAdded,
		OutputIndex: intPtr(tool.OutputIndex),
		ItemID:      tool.ItemID,
		Item:        item,
	})
}

// closeReasoning finishes the open reasoning item. Reasoning precedes the
// answer, so it is closed as soon as text or a tool call starts (as the
// official API does) rather than staying in_progress until finish_reason.
func (s *ChatToResponsesStreamState) closeReasoning(status string) []ChatToResponsesStreamEvent {
	if !s.reasoningStarted || s.reasoningDone {
		return nil
	}
	events := make([]ChatToResponsesStreamEvent, 0, 3)
	s.reasoningDone = true
	reasoningDone := dto.ResponsesStreamResponse{
		Type:         responsesEventReasoningSummaryDone,
		OutputIndex:  intPtr(s.reasoningIndex),
		SummaryIndex: intPtr(0),
		ItemID:       s.reasoningID(),
		Part: &dto.ResponsesReasoningSummaryPart{
			Type: "summary_text",
			Text: s.reasoning.String(),
		},
	}
	if s.EmitSequenceNumber {
		reasoningDone.Text = kitutil.GetPointer(s.reasoning.String())
		reasoningDone.Part = nil
	}
	events = append(events, s.event(responsesEventReasoningSummaryDone, reasoningDone))
	events = append(events, s.event(responsesEventReasoningSummaryPartDone, dto.ResponsesStreamResponse{
		Type:         responsesEventReasoningSummaryPartDone,
		OutputIndex:  intPtr(s.reasoningIndex),
		SummaryIndex: intPtr(0),
		ItemID:       s.reasoningID(),
		Part: &dto.ResponsesReasoningSummaryPart{
			Type: "summary_text",
			Text: s.reasoning.String(),
		},
	}))
	closedReasoning := s.reasoningOutput(status)
	events = append(events, s.event(responsesEventOutputItemDone, dto.ResponsesStreamResponse{
		Type:        responsesEventOutputItemDone,
		OutputIndex: intPtr(s.reasoningIndex),
		Item:        closedReasoning,
	}))
	s.closedReasonings = append(s.closedReasonings, *closedReasoning)
	return events
}

func (s *ChatToResponsesStreamState) doneDeltaEvents() []ChatToResponsesStreamEvent {
	status := s.outputStatus()
	events := s.closeReasoning(status)
	if s.textStarted && !s.textDone {
		s.textDone = true
		textDone := dto.ResponsesStreamResponse{
			Type:         "response.output_text.done",
			OutputIndex:  intPtr(s.textOutputIndex),
			ContentIndex: intPtr(0),
			ItemID:       s.messageID(),
		}
		if s.EmitSequenceNumber {
			textDone.Text = kitutil.GetPointer(s.text.String())
		}
		events = append(events, s.event("response.output_text.done", textDone))
		events = append(events, s.event(responsesEventContentPartDone, dto.ResponsesStreamResponse{
			Type:         responsesEventContentPartDone,
			OutputIndex:  intPtr(s.textOutputIndex),
			ContentIndex: intPtr(0),
			ItemID:       s.messageID(),
			Part:         outputTextPart(s.text.String()),
		}))
		closedMessage := s.messageOutput(status)
		events = append(events, s.event(responsesEventOutputItemDone, dto.ResponsesStreamResponse{
			Type:        responsesEventOutputItemDone,
			OutputIndex: intPtr(s.textOutputIndex),
			Item:        closedMessage,
		}))
		s.closedMessages = append(s.closedMessages, *closedMessage)
	}
	for _, tool := range s.sortedTools() {
		if tool.Done {
			continue
		}
		tool.Done = true
		if !tool.Announced {
			if tool.Name == "" {
				continue
			}
			events = append(events, s.announceTool(tool))
			if !tool.Custom && tool.Arguments.Len() > 0 {
				events = append(events, s.event(responsesEventFunctionArgsDelta, dto.ResponsesStreamResponse{
					Type:        responsesEventFunctionArgsDelta,
					OutputIndex: intPtr(tool.OutputIndex),
					ItemID:      tool.ItemID,
					Delta:       tool.Arguments.String(),
				}))
			}
		}
		if tool.Custom {
			input := customToolInputFromArguments(tool.Arguments.String())
			if input != "" {
				events = append(events, s.event(responsesEventCustomToolInputDelta, dto.ResponsesStreamResponse{
					Type:        responsesEventCustomToolInputDelta,
					OutputIndex: intPtr(tool.OutputIndex),
					ItemID:      tool.ItemID,
					Delta:       input,
				}))
			}
			events = append(events, s.event(responsesEventCustomToolInputDone, dto.ResponsesStreamResponse{
				Type:        responsesEventCustomToolInputDone,
				OutputIndex: intPtr(tool.OutputIndex),
				ItemID:      tool.ItemID,
				Input:       kitutil.GetPointer(input),
			}))
		} else {
			argumentsDone := dto.ResponsesStreamResponse{
				Type:        responsesEventFunctionArgsDone,
				OutputIndex: intPtr(tool.OutputIndex),
				ItemID:      tool.ItemID,
			}
			if s.EmitSequenceNumber {
				argumentsDone.Arguments = kitutil.GetPointer(tool.Arguments.String())
				argumentsDone.Name = tool.Name
			}
			events = append(events, s.event(responsesEventFunctionArgsDone, argumentsDone))
		}
		events = append(events, s.event(responsesEventOutputItemDone, dto.ResponsesStreamResponse{
			Type:        responsesEventOutputItemDone,
			OutputIndex: intPtr(tool.OutputIndex),
			Item:        s.toolOutput(tool, status),
		}))
	}

	return events
}

func (s *ChatToResponsesStreamState) applyFinishReason(finishReason string) {
	if status, details := ResponsesStatusFromChatFinishReason(finishReason); status != "" {
		s.status = status
		s.incompleteDetails = details
	}
}

func (s *ChatToResponsesStreamState) finalResponse() *dto.OpenAIResponsesResponse {
	output := make([]dto.ResponsesOutput, 0, len(s.outputOrder))
	status := s.outputStatus()
	for _, ref := range s.outputOrder {
		switch ref.Kind {
		case "message":
			if ref.Segment < len(s.closedMessages) {
				closed := s.closedMessages[ref.Segment]
				closed.Status = status
				output = append(output, closed)
				continue
			}
			output = append(output, *s.messageOutput(status))
		case "reasoning":
			if ref.Segment < len(s.closedReasonings) {
				closed := s.closedReasonings[ref.Segment]
				closed.Status = status
				output = append(output, closed)
				continue
			}
			output = append(output, *s.reasoningOutput(status))
		case "tool":
			if tool := s.toolsByIndex[ref.ToolIndex]; tool != nil {
				output = append(output, *s.toolOutput(tool, status))
			}

		}
	}
	return &dto.OpenAIResponsesResponse{
		ID:                s.ID,
		Object:            "response",
		CreatedAt:         int(s.Created),
		Status:            []byte(fmt.Sprintf("%q", s.status)),
		IncompleteDetails: s.incompleteDetails,
		Model:             s.Model,
		Output:            output,
		Usage:             s.Usage,
	}
}

func (s *ChatToResponsesStreamState) createdResponse() *dto.OpenAIResponsesResponse {
	return &dto.OpenAIResponsesResponse{
		ID:        s.ID,
		Object:    "response",
		CreatedAt: int(s.Created),
		Status:    []byte(`"in_progress"`),
		Model:     s.Model,
		Output:    []dto.ResponsesOutput{},
	}
}

func (s *ChatToResponsesStreamState) nextIndex(ref chatToResponsesOutputRef) int {
	index := s.nextOutputIndex
	s.nextOutputIndex++
	s.outputOrder = append(s.outputOrder, ref)
	return index
}

func (s *ChatToResponsesStreamState) sortedTools() []*chatToResponsesStreamTool {
	indexes := make([]int, 0, len(s.toolsByIndex))
	for index := range s.toolsByIndex {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	tools := make([]*chatToResponsesStreamTool, 0, len(indexes))
	for _, index := range indexes {
		tools = append(tools, s.toolsByIndex[index])
	}
	return tools
}

func (s *ChatToResponsesStreamState) outputStatus() string {
	if s.status == "incomplete" || s.status == "failed" {
		return "incomplete"
	}
	return "completed"
}

func (s *ChatToResponsesStreamState) messageID() string {
	return fmt.Sprintf("%s_msg_%d", s.ID, s.textSegment)
}

func (s *ChatToResponsesStreamState) reasoningID() string {
	return fmt.Sprintf("%s_reasoning_%d", s.ID, s.reasoningSegment)
}

func (s *ChatToResponsesStreamState) messageOutput(status string) *dto.ResponsesOutput {
	return &dto.ResponsesOutput{
		Type:   responsesOutputTypeMessage,
		ID:     s.messageID(),
		Status: status,
		Role:   "assistant",
		Content: []dto.ResponsesOutputContent{
			{
				Type:        "output_text",
				Text:        s.text.String(),
				Annotations: []any{},
			},
		},
	}
}

func (s *ChatToResponsesStreamState) reasoningOutput(status string) *dto.ResponsesOutput {
	return &dto.ResponsesOutput{
		Type:   responsesOutputTypeReasoning,
		ID:     s.reasoningID(),
		Status: status,
		Summary: []dto.ResponsesReasoningSummaryPart{
			{
				Type: "summary_text",
				Text: s.reasoning.String(),
			},
		},
	}
}

func (s *ChatToResponsesStreamState) toolOutput(tool *chatToResponsesStreamTool, status string) *dto.ResponsesOutput {
	if tool.Custom {
		return &dto.ResponsesOutput{
			Type:   responsesOutputTypeCustomToolCall,
			ID:     tool.ItemID,
			Status: status,
			CallId: tool.callID(),
			Name:   tool.Name,
			Input:  chatArgumentsRawMessage(customToolInputFromArguments(tool.Arguments.String())),
		}
	}
	return &dto.ResponsesOutput{
		Type:      responsesOutputTypeFunctionCall,
		ID:        tool.ItemID,
		Status:    status,
		CallId:    tool.callID(),
		Name:      tool.Name,
		Arguments: chatArgumentsRawMessage(tool.Arguments.String()),
	}
}

// outputTextPart is the output_text content part carried by content_part
// events; annotations is always present as the SDK models require it.
func outputTextPart(text string) *dto.ResponsesReasoningSummaryPart {
	return &dto.ResponsesReasoningSummaryPart{Type: "output_text", Text: text, Annotations: &[]any{}}
}

func (t *chatToResponsesStreamTool) callID() string {
	if t == nil {
		return ""
	}
	if t.CallID == "" {
		return t.ItemID
	}
	return t.CallID
}

func (s *ChatToResponsesStreamState) event(eventType string, payload dto.ResponsesStreamResponse) ChatToResponsesStreamEvent {
	if s.EmitSequenceNumber {
		sequenceNumber := s.nextSequenceNumber
		s.nextSequenceNumber++
		payload.SequenceNumber = &sequenceNumber
	}
	return responsesStreamEvent(eventType, payload)
}
