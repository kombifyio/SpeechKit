//go:build linux

package voiceagent

import (
	"encoding/json"
	"testing"
)

func TestAdapter_ProviderMessagesRelayedToClient(t *testing.T) {
	provider := newFakeProvider()
	defer provider.Close() //nolint:errcheck
	resolver := &fakeResolver{}
	env := startAdapterEnv(t, 0, provider, resolver)

	sendStart(t, env.conn, StartFrame{})
	var stateMsg StateFrame
	readJSONFrame(t, env.conn, &stateMsg)

	// Push input transcript + audio chunk.
	provider.push(&LiveMessage{
		InputTranscript:     "hello",
		InputTranscriptDone: true,
		ProviderMetadata:    map[string]any{"provider_event": "fake.input.final"},
		SessionResumable:    true,
	})
	provider.push(&LiveMessage{Audio: []byte{0xAA, 0xBB}})

	typeName, raw := readEnvelope(t, env.conn)
	if typeName != MsgInputTranscript {
		t.Fatalf("expected input_transcript, got %s body=%s", typeName, string(raw))
	}
	var transcript TranscriptFrame
	if err := json.Unmarshal(raw, &transcript); err != nil {
		t.Fatalf("unmarshal transcript: %v", err)
	}
	if transcript.Text != "hello" || !transcript.Done {
		t.Fatalf("unexpected transcript frame: %+v", transcript)
	}
	if transcript.EventType != EventInputFinal ||
		!eventTypesContain(transcript.EventTypes, EventInputFinal) ||
		!eventTypesContain(transcript.EventTypes, EventSessionResumable) {
		t.Fatalf("transcript event fields = %+v", transcript.EventFrameFields)
	}
	if transcript.ProviderMetadata["provider_event"] != "fake.input.final" {
		t.Fatalf("provider metadata = %#v", transcript.ProviderMetadata)
	}

	got := readBinaryFrame(t, env.conn)
	if len(got) != 2 || got[0] != 0xAA || got[1] != 0xBB {
		t.Fatalf("unexpected audio bytes: %x", got)
	}
}

func TestAdapter_OutputTranscriptCarriesProviderEventFields(t *testing.T) {
	provider := newFakeProvider()
	defer provider.Close() //nolint:errcheck
	resolver := &fakeResolver{}
	env := startAdapterEnv(t, 0, provider, resolver)

	sendStart(t, env.conn, StartFrame{})
	var stateMsg StateFrame
	readJSONFrame(t, env.conn, &stateMsg)

	provider.push(&LiveMessage{
		EventType:            EventOutputText,
		EventTypes:           []string{EventOutputText, EventTurnEnd},
		OutputTranscript:     "done",
		OutputTranscriptDone: true,
		ProviderMetadata:     map[string]any{"provider_event": "fake.output.final"},
	})

	typeName, raw := readEnvelope(t, env.conn)
	if typeName != MsgOutputTranscript {
		t.Fatalf("expected output_transcript, got %s body=%s", typeName, string(raw))
	}
	var transcript TranscriptFrame
	if err := json.Unmarshal(raw, &transcript); err != nil {
		t.Fatalf("unmarshal output transcript: %v", err)
	}
	if transcript.Text != "done" || !transcript.Done {
		t.Fatalf("unexpected output transcript frame: %+v", transcript)
	}
	if transcript.EventType != EventOutputText ||
		!eventTypesContain(transcript.EventTypes, EventOutputText) ||
		!eventTypesContain(transcript.EventTypes, EventTurnEnd) {
		t.Fatalf("output transcript event fields = %+v", transcript.EventFrameFields)
	}
	if transcript.ProviderMetadata["provider_event"] != "fake.output.final" {
		t.Fatalf("output transcript provider metadata = %#v", transcript.ProviderMetadata)
	}
}

func TestAdapter_InterruptedCarriesProviderEventFields(t *testing.T) {
	provider := newFakeProvider()
	defer provider.Close() //nolint:errcheck
	resolver := &fakeResolver{}
	env := startAdapterEnv(t, 0, provider, resolver)

	sendStart(t, env.conn, StartFrame{})
	var stateMsg StateFrame
	readJSONFrame(t, env.conn, &stateMsg)

	provider.push(&LiveMessage{
		Interrupted:      true,
		ProviderMetadata: map[string]any{"provider_event": "fake.barge_in"},
	})

	typeName, raw := readEnvelope(t, env.conn)
	if typeName != MsgInterrupted {
		t.Fatalf("expected interrupted, got %s body=%s", typeName, string(raw))
	}
	var frame InterruptedFrame
	if err := json.Unmarshal(raw, &frame); err != nil {
		t.Fatalf("unmarshal interrupted: %v", err)
	}
	if frame.EventType != EventInterrupted || !eventTypesContain(frame.EventTypes, EventInterrupted) {
		t.Fatalf("interrupted event fields = %+v", frame.EventFrameFields)
	}
	if frame.ProviderMetadata["provider_event"] != "fake.barge_in" {
		t.Fatalf("interrupted provider metadata = %#v", frame.ProviderMetadata)
	}
}

func TestAdapter_StandaloneProviderEventRelayedToClient(t *testing.T) {
	provider := newFakeProvider()
	defer provider.Close() //nolint:errcheck
	resolver := &fakeResolver{}
	env := startAdapterEnv(t, 0, provider, resolver)

	sendStart(t, env.conn, StartFrame{})
	var stateMsg StateFrame
	readJSONFrame(t, env.conn, &stateMsg)

	provider.push(&LiveMessage{
		Done:             true,
		SessionResumable: true,
		ProviderMetadata: map[string]any{"provider_event": "fake.turn.done"},
	})

	typeName, raw := readEnvelope(t, env.conn)
	if typeName != MsgEvent {
		t.Fatalf("expected event, got %s body=%s", typeName, string(raw))
	}
	var frame EventFrame
	if err := json.Unmarshal(raw, &frame); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}
	if frame.EventType != EventSessionResumable ||
		!eventTypesContain(frame.EventTypes, EventSessionResumable) ||
		!eventTypesContain(frame.EventTypes, EventTurnEnd) {
		t.Fatalf("event fields = %+v", frame.EventFrameFields)
	}
	if frame.ProviderMetadata["provider_event"] != "fake.turn.done" {
		t.Fatalf("provider metadata = %#v", frame.ProviderMetadata)
	}

	provider.push(&LiveMessage{
		Audio:            []byte{0xCC},
		Done:             true,
		ProviderMetadata: map[string]any{"provider_event": "fake.audio.done"},
	})
	if got := readBinaryFrame(t, env.conn); len(got) != 1 || got[0] != 0xCC {
		t.Fatalf("audio frame = %x, want cc", got)
	}
	typeName, raw = readEnvelope(t, env.conn)
	if typeName != MsgEvent {
		t.Fatalf("expected audio follow-up event, got %s body=%s", typeName, string(raw))
	}
	if err := json.Unmarshal(raw, &frame); err != nil {
		t.Fatalf("unmarshal audio follow-up event: %v", err)
	}
	if frame.EventType != EventTurnEnd ||
		!eventTypesContain(frame.EventTypes, EventOutputAudio) ||
		!eventTypesContain(frame.EventTypes, EventTurnEnd) {
		t.Fatalf("audio follow-up event fields = %+v", frame.EventFrameFields)
	}
	if frame.ProviderMetadata["provider_event"] != "fake.audio.done" {
		t.Fatalf("audio follow-up provider metadata = %#v", frame.ProviderMetadata)
	}
}
