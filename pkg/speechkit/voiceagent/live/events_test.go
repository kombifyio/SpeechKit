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
		if !liveEventTypesContain(got, want) {
			t.Fatalf("InferLiveEventTypes = %v, missing %s", got, want)
		}
	}
}

func TestNormalizeLiveMessageEventsAddsProviderMetadata(t *testing.T) {
	msg := normalizeLiveMessageEvents(&LiveMessage{Audio: []byte{1}}, "response.audio.delta")
	if msg.EventType != LiveEventOutputAudio {
		t.Fatalf("EventType = %q, want %q", msg.EventType, LiveEventOutputAudio)
	}
	if !liveEventTypesContain(msg.EventTypes, LiveEventOutputAudio) {
		t.Fatalf("EventTypes = %v, want output audio", msg.EventTypes)
	}
	if msg.ProviderMetadata["provider_event"] != "response.audio.delta" {
		t.Fatalf("ProviderMetadata = %#v", msg.ProviderMetadata)
	}
}
