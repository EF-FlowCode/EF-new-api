package controller

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
)

// Semantic decoding adapted from upstream eb48396d5. Lifecycle and billing stay host-owned.
// publicProtocolLimits bounds untrusted semantic output before it reaches the
// host-owned Responses state machine.
type publicProtocolLimits struct {
	MaxEventsPerTick      int
	MaxEventsBytes        int
	MaxEventBytes         int
	MaxEventDepth         int
	MaxStateBytes         int
	MaxStateDepth         int
	MaxOutputs            int
	MaxTotalOutputBytes   int
	MaxMessageBytes       int
	MaxMetadataValueBytes int
	MaxCodeBytes          int
}

func defaultPublicProtocolLimits() publicProtocolLimits {
	return publicProtocolLimits{
		MaxEventsPerTick:      16,
		MaxEventsBytes:        64 << 10,
		MaxEventBytes:         32 << 10,
		MaxEventDepth:         16,
		MaxStateBytes:         16 << 10,
		MaxStateDepth:         16,
		MaxOutputs:            64,
		MaxTotalOutputBytes:   1 << 20,
		MaxMessageBytes:       4 << 10,
		MaxMetadataValueBytes: 512,
		MaxCodeBytes:          128,
	}
}

func (l publicProtocolLimits) withDefaults() publicProtocolLimits {
	defaults := defaultPublicProtocolLimits()
	if l.MaxEventsPerTick <= 0 {
		l.MaxEventsPerTick = defaults.MaxEventsPerTick
	}
	if l.MaxEventsBytes <= 0 {
		l.MaxEventsBytes = defaults.MaxEventsBytes
	}
	if l.MaxEventBytes <= 0 {
		l.MaxEventBytes = defaults.MaxEventBytes
	}
	if l.MaxEventDepth <= 0 {
		l.MaxEventDepth = defaults.MaxEventDepth
	}
	if l.MaxStateBytes <= 0 {
		l.MaxStateBytes = defaults.MaxStateBytes
	}
	if l.MaxStateDepth <= 0 {
		l.MaxStateDepth = defaults.MaxStateDepth
	}
	if l.MaxOutputs <= 0 {
		l.MaxOutputs = defaults.MaxOutputs
	}
	if l.MaxTotalOutputBytes <= 0 {
		l.MaxTotalOutputBytes = defaults.MaxTotalOutputBytes
	}
	if l.MaxMessageBytes <= 0 {
		l.MaxMessageBytes = defaults.MaxMessageBytes
	}
	if l.MaxMetadataValueBytes <= 0 {
		l.MaxMetadataValueBytes = defaults.MaxMetadataValueBytes
	}
	if l.MaxCodeBytes <= 0 {
		l.MaxCodeBytes = defaults.MaxCodeBytes
	}
	return l
}

// publicProtocolState distinguishes an omitted state property from an explicit JSON
// null. Value is always a validated, detached JSON value when Present is true.
type publicProtocolState struct {
	Present bool
	Null    bool
	Value   json.RawMessage
}

func (s publicProtocolState) PluginValue() (any, error) {
	if !s.Present || s.Null {
		return nil, nil
	}
	var value any
	if err := json.Unmarshal(s.Value, &value); err != nil {
		return nil, err
	}
	return value, nil
}

type publicSemanticEvent struct {
	Type     string
	Progress *float64
	Message  *string
	Data     json.RawMessage
	Code     *string
}

type publicEventBatch struct {
	Events []publicSemanticEvent
	State  publicProtocolState
	Done   bool
}

// decodePublicEventBatch converts an exported JS value into the
// deliberately small semantic event contract. Unknown fields are rejected so
// plugins cannot smuggle protocol-owned wire fields into the response.
func decodePublicEventBatch(value any, limits publicProtocolLimits) (publicEventBatch, error) {
	limits = limits.withDefaults()
	encoded, err := json.Marshal(value)
	if err != nil {
		return publicEventBatch{}, fmt.Errorf("protocol event result is not JSON-compatible: %w", err)
	}
	maxResultBytes := limits.MaxEventsBytes + limits.MaxStateBytes + 4096
	if len(encoded) > maxResultBytes {
		return publicEventBatch{}, fmt.Errorf("protocol event result exceeds %d bytes", maxResultBytes)
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil || fields == nil {
		return publicEventBatch{}, errors.New("protocol event result must be an object")
	}
	for name := range fields {
		switch name {
		case "events", "state", "done":
		default:
			return publicEventBatch{}, fmt.Errorf("protocol event result contains unknown field %q", name)
		}
	}

	rawEvents, ok := fields["events"]
	if !ok || isJSONNull(rawEvents) {
		return publicEventBatch{}, errors.New("protocol event result events must be an array")
	}
	if len(rawEvents) > limits.MaxEventsBytes {
		return publicEventBatch{}, fmt.Errorf("protocol events exceed %d bytes", limits.MaxEventsBytes)
	}
	var encodedEvents []json.RawMessage
	if err := json.Unmarshal(rawEvents, &encodedEvents); err != nil {
		return publicEventBatch{}, errors.New("protocol event result events must be an array")
	}
	if len(encodedEvents) > limits.MaxEventsPerTick {
		return publicEventBatch{}, fmt.Errorf("protocol events exceed limit of %d", limits.MaxEventsPerTick)
	}

	rawDone, ok := fields["done"]
	if !ok || isJSONNull(rawDone) {
		return publicEventBatch{}, errors.New("protocol event result done must be a boolean")
	}
	var done bool
	if err := json.Unmarshal(rawDone, &done); err != nil {
		return publicEventBatch{}, errors.New("protocol event result done must be a boolean")
	}

	result := publicEventBatch{
		Events: make([]publicSemanticEvent, 0, len(encodedEvents)),
		Done:   done,
	}
	for _, rawEvent := range encodedEvents {
		event, err := decodePluginSemanticEvent(rawEvent, limits)
		if err != nil {
			return publicEventBatch{}, err
		}
		result.Events = append(result.Events, event)
	}

	if rawState, exists := fields["state"]; exists {
		if len(rawState) > limits.MaxStateBytes {
			return publicEventBatch{}, fmt.Errorf("protocol state exceeds %d bytes", limits.MaxStateBytes)
		}
		depth, err := pluginJSONDepth(rawState)
		if err != nil {
			return publicEventBatch{}, errors.New("protocol state must be JSON-compatible")
		}
		if depth > limits.MaxStateDepth {
			return publicEventBatch{}, fmt.Errorf("protocol state exceeds depth limit of %d", limits.MaxStateDepth)
		}
		result.State = publicProtocolState{
			Present: true,
			Null:    isJSONNull(rawState),
			Value:   append(json.RawMessage(nil), rawState...),
		}
	}
	return result, nil
}

func decodePluginSemanticEvent(raw json.RawMessage, limits publicProtocolLimits) (publicSemanticEvent, error) {
	if len(raw) > limits.MaxEventBytes {
		return publicSemanticEvent{}, fmt.Errorf("protocol event exceeds %d bytes", limits.MaxEventBytes)
	}
	depth, err := pluginJSONDepth(raw)
	if err != nil {
		return publicSemanticEvent{}, errors.New("protocol event must be a JSON object")
	}
	if depth > limits.MaxEventDepth {
		return publicSemanticEvent{}, fmt.Errorf("protocol event exceeds depth limit of %d", limits.MaxEventDepth)
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return publicSemanticEvent{}, errors.New("protocol event must be a JSON object")
	}
	rawType, ok := fields["type"]
	if !ok || isJSONNull(rawType) {
		return publicSemanticEvent{}, errors.New("protocol event type is required")
	}
	var eventType string
	if err := json.Unmarshal(rawType, &eventType); err != nil {
		return publicSemanticEvent{}, errors.New("protocol event type must be a string")
	}

	event := publicSemanticEvent{Type: eventType}
	switch eventType {
	case "progress":
		if err := rejectUnknownProtocolFields(fields, "type", "progress", "message"); err != nil {
			return publicSemanticEvent{}, err
		}
		if rawProgress, exists := fields["progress"]; exists {
			if isJSONNull(rawProgress) {
				return publicSemanticEvent{}, errors.New("progress event progress must be a number")
			}
			var progress float64
			if err := json.Unmarshal(rawProgress, &progress); err != nil ||
				math.IsNaN(progress) || math.IsInf(progress, 0) ||
				progress < 0 || progress > 100 {
				return publicSemanticEvent{}, errors.New("progress event progress must be between 0 and 100")
			}
			event.Progress = &progress
		}
		if rawMessage, exists := fields["message"]; exists {
			message, err := decodeBoundedProtocolString(rawMessage, "progress event message", limits.MaxMetadataValueBytes, false)
			if err != nil {
				return publicSemanticEvent{}, err
			}
			event.Message = &message
		}
	case "output":
		if err := rejectUnknownProtocolFields(fields, "type", "data"); err != nil {
			return publicSemanticEvent{}, err
		}
		rawData, exists := fields["data"]
		if !exists {
			return publicSemanticEvent{}, errors.New("output event data is required")
		}
		if len(rawData) > limits.MaxEventBytes {
			return publicSemanticEvent{}, fmt.Errorf("output event data exceeds %d bytes", limits.MaxEventBytes)
		}
		event.Data = append(json.RawMessage(nil), rawData...)
	case "error":
		if err := rejectUnknownProtocolFields(fields, "type", "code", "message"); err != nil {
			return publicSemanticEvent{}, err
		}
		rawMessage, exists := fields["message"]
		if !exists {
			return publicSemanticEvent{}, errors.New("error event message is required")
		}
		message, err := decodeBoundedProtocolString(rawMessage, "error event message", limits.MaxMessageBytes, true)
		if err != nil {
			return publicSemanticEvent{}, err
		}
		event.Message = &message
		if rawCode, exists := fields["code"]; exists {
			code, err := decodeBoundedProtocolString(rawCode, "error event code", limits.MaxCodeBytes, false)
			if err != nil {
				return publicSemanticEvent{}, err
			}
			event.Code = &code
		}
	default:
		return publicSemanticEvent{}, fmt.Errorf("unsupported protocol event type %q", eventType)
	}
	return event, nil
}

func rejectUnknownProtocolFields(fields map[string]json.RawMessage, allowed ...string) error {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, name := range allowed {
		allowedSet[name] = struct{}{}
	}
	for name := range fields {
		if _, ok := allowedSet[name]; !ok {
			return fmt.Errorf("protocol event contains unknown field %q", name)
		}
	}
	return nil
}

func decodeBoundedProtocolString(raw json.RawMessage, field string, maxBytes int, required bool) (string, error) {
	if isJSONNull(raw) {
		return "", fmt.Errorf("%s must be a string", field)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("%s must be a string", field)
	}
	if required && strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	if len(value) > maxBytes {
		return "", fmt.Errorf("%s exceeds %d bytes", field, maxBytes)
	}
	return value, nil
}

func pluginJSONDepth(raw json.RawMessage) (int, error) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return 0, err
	}
	var depth func(any) int
	depth = func(current any) int {
		switch typed := current.(type) {
		case []any:
			maxChild := 0
			for _, child := range typed {
				maxChild = max(maxChild, depth(child))
			}
			return 1 + maxChild
		case map[string]any:
			maxChild := 0
			for _, child := range typed {
				maxChild = max(maxChild, depth(child))
			}
			return 1 + maxChild
		default:
			return 1
		}
	}
	return depth(value), nil
}

func isJSONNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}
