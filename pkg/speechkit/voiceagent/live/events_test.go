package live

import "testing"

func TestInferLiveEventTypesCoversCombinedProviderFrames(t *testing.T) {
	msg := &LiveMessage{
		Audio:                []byte{1, 2},
		Text:                 "hello",
		InputTranscript:      "user",
		InputTranscriptDone:  true,
		OutputTranscript:     "agent",
		OutputTranscriptDone: true,
		ToolCalls:            []ToolCall{{ID: "call-1", Name: "lookup"}},
		Interrupted:          true,
		GoAway:               true,
		SessionResumable:     true,
		Done:                 true,
	}

	got := InferLiveEventTypes(msg)
	for _, want := range []LiveEventType{
		LiveEventSessionEnd,
		LiveEventSessionResumable,
		LiveEventInterrupted,
		LiveEventInputFinal,
		LiveEventOutputAudio,
		LiveEventOutputText,
		LiveEventToolCall,
		LiveEventTurnEnd,
	} {
		if !EventTypesContain(got, want) {
			t.Fatalf("InferLiveEventTypes = %v, missing %s", got, want)
		}
	}
}

func TestInferLiveEventTypesReturnsCopyOfExplicitEvents(t *testing.T) {
	msg := &LiveMessage{
		EventTypes: []LiveEventType{LiveEventInputPartial, LiveEventOutputText},
		Text:       "ignored because provider was explicit",
	}

	got := InferLiveEventTypes(msg)
	if len(got) != 2 || got[0] != LiveEventInputPartial || got[1] != LiveEventOutputText {
		t.Fatalf("InferLiveEventTypes = %v, want explicit event order", got)
	}
	got[0] = LiveEventSessionEnd
	if msg.EventTypes[0] != LiveEventInputPartial {
		t.Fatalf("InferLiveEventTypes should not expose the provider-owned EventTypes slice: %v", msg.EventTypes)
	}
}

func TestNormalizeLiveMessageEventsAddsProviderMetadata(t *testing.T) {
	msg := NormalizeMessageEvents(&LiveMessage{Audio: []byte{1}}, "response.audio.delta")
	if msg.EventType != LiveEventOutputAudio {
		t.Fatalf("EventType = %q, want %q", msg.EventType, LiveEventOutputAudio)
	}
	if !EventTypesContain(msg.EventTypes, LiveEventOutputAudio) {
		t.Fatalf("EventTypes = %v, want output audio", msg.EventTypes)
	}
	if msg.ProviderMetadata["provider_event"] != "response.audio.delta" {
		t.Fatalf("ProviderMetadata = %#v", msg.ProviderMetadata)
	}
}

func TestNormalizeLiveMessageEventsPreservesProviderSuppliedMetadataEvent(t *testing.T) {
	msg := NormalizeMessageEvents(&LiveMessage{
		Text: "hello",
		ProviderMetadata: map[string]any{
			"provider_event": "native.output.text",
		},
	}, "fallback.output.text")

	if msg.EventType != LiveEventOutputText {
		t.Fatalf("EventType = %q, want output_text", msg.EventType)
	}
	if msg.ProviderMetadata["provider_event"] != "native.output.text" {
		t.Fatalf("ProviderMetadata provider_event = %#v, want native event preserved", msg.ProviderMetadata["provider_event"])
	}
}
