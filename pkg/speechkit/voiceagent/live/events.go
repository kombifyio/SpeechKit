package live

import "strings"

// InferLiveEventTypes returns the provider-neutral event meanings represented
// by a LiveMessage. Providers should set EventType/EventTypes themselves when
// translating native frames; this helper gives custom providers and tests the
// same fallback semantics.
func InferLiveEventTypes(msg *LiveMessage) []LiveEventType {
	if msg == nil {
		return nil
	}
	if len(msg.EventTypes) > 0 {
		return append([]LiveEventType(nil), msg.EventTypes...)
	}
	var out []LiveEventType
	if msg.EventType != "" {
		out = appendLiveEventType(out, msg.EventType)
	}
	if msg.GoAway {
		out = appendLiveEventType(out, LiveEventSessionEnd)
	}
	if msg.SessionResumable {
		out = appendLiveEventType(out, LiveEventSessionResumable)
	}
	if msg.Interrupted {
		out = appendLiveEventType(out, LiveEventInterrupted)
	}
	if msg.InputTranscript != "" {
		if msg.InputTranscriptDone {
			out = appendLiveEventType(out, LiveEventInputFinal)
		} else {
			out = appendLiveEventType(out, LiveEventInputPartial)
		}
	}
	if len(msg.Audio) > 0 {
		out = appendLiveEventType(out, LiveEventOutputAudio)
	}
	if msg.Text != "" || msg.OutputTranscript != "" {
		out = appendLiveEventType(out, LiveEventOutputText)
	}
	if len(msg.ToolCalls) > 0 {
		out = appendLiveEventType(out, LiveEventToolCall)
	}
	if msg.Done {
		out = appendLiveEventType(out, LiveEventTurnEnd)
	}
	return out
}

func NormalizeMessageEvents(msg *LiveMessage, providerEvent string) *LiveMessage {
	if msg == nil {
		return nil
	}
	if len(msg.EventTypes) == 0 {
		msg.EventTypes = InferLiveEventTypes(msg)
	}
	if msg.EventType == "" && len(msg.EventTypes) > 0 {
		msg.EventType = msg.EventTypes[0]
	}
	providerEvent = strings.TrimSpace(providerEvent)
	if providerEvent != "" {
		if msg.ProviderMetadata == nil {
			msg.ProviderMetadata = map[string]any{}
		}
		if _, ok := msg.ProviderMetadata["provider_event"]; !ok {
			msg.ProviderMetadata["provider_event"] = providerEvent
		}
	}
	return msg
}

func appendLiveEventType(base []LiveEventType, value LiveEventType) []LiveEventType {
	if strings.TrimSpace(string(value)) == "" {
		return base
	}
	for _, existing := range base {
		if existing == value {
			return base
		}
	}
	return append(base, value)
}

// EventTypesContain reports whether want appears in values. Frames carry
// several normalized meanings when one provider event maps to more than
// one, so callers ask rather than compare a single field.
func EventTypesContain(values []LiveEventType, want LiveEventType) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
