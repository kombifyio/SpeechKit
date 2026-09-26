//go:build linux

package voiceagent

import (
	"strings"
)

func standaloneEventType(msg *LiveMessage) string {
	if msg == nil {
		return ""
	}
	if msg.InputTranscript != "" ||
		msg.OutputTranscript != "" ||
		len(msg.ToolCalls) > 0 ||
		msg.Interrupted ||
		msg.GoAway {
		return ""
	}
	for _, eventType := range inferServerEventTypes(msg) {
		if len(msg.Audio) > 0 && eventType == EventOutputAudio {
			continue
		}
		return eventType
	}
	return ""
}

func eventFrameFields(msg *LiveMessage, primary string) EventFrameFields {
	types := appendEventTypeString(nil, primary)
	for _, eventType := range inferServerEventTypes(msg) {
		types = appendEventTypeString(types, eventType)
	}
	eventType := strings.TrimSpace(primary)
	if eventType == "" && len(types) > 0 {
		eventType = types[0]
	}
	return EventFrameFields{
		EventType:        eventType,
		EventTypes:       types,
		ProviderMetadata: copyProviderMetadata(msg),
	}
}

func (a *Adapter) eventFrameFields(msg *LiveMessage, primary string) EventFrameFields {
	fields := eventFrameFields(msg, primary)
	if a.Session != nil {
		fields.AISessionID = a.Session.AISessionID
	}
	return fields
}

func inferServerEventTypes(msg *LiveMessage) []string {
	if msg == nil {
		return nil
	}
	var out []string
	out = appendEventTypeString(out, msg.EventType)
	for _, eventType := range msg.EventTypes {
		out = appendEventTypeString(out, eventType)
	}
	if len(out) > 0 {
		return out
	}
	if msg.GoAway {
		out = appendEventTypeString(out, EventSessionEnd)
	}
	if msg.SessionResumable {
		out = appendEventTypeString(out, EventSessionResumable)
	}
	if msg.Interrupted {
		out = appendEventTypeString(out, EventInterrupted)
	}
	if msg.InputTranscript != "" {
		if msg.InputTranscriptDone {
			out = appendEventTypeString(out, EventInputFinal)
		} else {
			out = appendEventTypeString(out, EventInputPartial)
		}
	}
	if len(msg.Audio) > 0 {
		out = appendEventTypeString(out, EventOutputAudio)
	}
	if msg.OutputTranscript != "" {
		out = appendEventTypeString(out, EventOutputText)
	}
	if len(msg.ToolCalls) > 0 {
		out = appendEventTypeString(out, EventToolCall)
	}
	if msg.Done {
		out = appendEventTypeString(out, EventTurnEnd)
	}
	return out
}

func appendEventTypeString(base []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return base
	}
	for _, existing := range base {
		if existing == value {
			return base
		}
	}
	return append(base, value)
}

func copyProviderMetadata(msg *LiveMessage) map[string]any {
	if msg == nil || len(msg.ProviderMetadata) == 0 {
		return nil
	}
	out := make(map[string]any, len(msg.ProviderMetadata))
	for key, value := range msg.ProviderMetadata {
		out[key] = value
	}
	return out
}
