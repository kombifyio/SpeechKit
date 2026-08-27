//go:build linux

package voiceagent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
)

// fixturePath is the golden contract file shared with the TypeScript and
// Android clients. The Go structs in protocol.go are the producer of truth;
// this test pins their wire shape to the interchange artifact.
const fixturePath = "voiceagent.v1.json"

func loadVoiceAgentFixture(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	path := filepath.Join("..", "..", "..", "docs", "server", "fixtures", fixturePath)
	raw, err := os.ReadFile(path) // #nosec G304 -- fixed repo-relative test fixture path.
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var doc struct {
		Frames map[string]json.RawMessage `json:"frames"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	if len(doc.Frames) == 0 {
		t.Fatal("fixture has no frames")
	}
	return doc.Frames
}

func fixtureFrame(t *testing.T, frames map[string]json.RawMessage, name string) map[string]any {
	t.Helper()
	entry, ok := frames[name]
	if !ok {
		t.Fatalf("fixture has no frame %q", name)
	}
	var out map[string]any
	if err := json.Unmarshal(entry, &out); err != nil {
		t.Fatalf("parse fixture frame %q: %v", name, err)
	}
	return out
}

// assertVoiceAgentWireEqual marshals v and compares the generic JSON value
// with the golden fixture frame — a byte-order-insensitive producer
// drift-check, matching the dictation stream_protocol_test.go approach.
func assertVoiceAgentWireEqual(t *testing.T, frames map[string]json.RawMessage, name string, v any) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal %q: %v", name, err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("re-parse %q: %v", name, err)
	}
	want := fixtureFrame(t, frames, name)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("wire drift on %q:\n got: %s\nwant fixture frame", name, data)
	}
}

func TestVoiceAgentProtocol_GoldenFixtures(t *testing.T) {
	frames := loadVoiceAgentFixture(t)

	// ── client → server ──────────────────────────────────────────────────
	assertVoiceAgentWireEqual(t, frames, "start", StartFrame{
		Type:           MsgStart,
		PersonaID:      "brainstorm",
		SequenceID:     "seq-1",
		Provider:       "deepgram",
		MediaTransport: MediaTransportWebSocket,
		Locale:         "de-DE",
		Thinking:       "low",
	})
	assertVoiceAgentWireEqual(t, frames, "start_full", StartFrame{
		Type:           MsgStart,
		PersonaID:      "brainstorm",
		RoleID:         "moderator",
		SequenceID:     "seq-1",
		Provider:       "gemini",
		MediaTransport: MediaTransportLiveKit,
		Voice:          "Aoede",
		Locale:         "de-DE",
		Model:          "gemini-2.5-flash-native-audio",
		Thinking:       "low",
		ActivityDetection: &ActivityDetectionFrame{
			Automatic:         true,
			StartSensitivity:  "high",
			EndSensitivity:    "low",
			PrefixPaddingMs:   120,
			SilenceDurationMs: 600,
			ActivityHandling:  "start_of_activity_interrupts",
			TurnCoverage:      "turn_includes_only_activity",
		},
		Speaker: &speaker.Options{
			Enabled:           true,
			Diarization:       true,
			ProviderProfileID: "speaker.deepgram.nova-3",
			SpeakersExpected:  2,
		},
		SystemPromptOverride: "Antworte knapp und auf Deutsch.",
	})
	assertVoiceAgentWireEqual(t, frames, "text", TextFrame{
		Type: MsgText,
		Text: "Wie ist das Wetter in Berlin?",
	})
	assertVoiceAgentWireEqual(t, frames, "tool_response", ToolResponseFrame{
		Type:     MsgToolResponse,
		ID:       "t1",
		Name:     "weather",
		Response: map[string]any{"city": "Berlin", "temperature_c": 21.5},
	})
	assertVoiceAgentWireEqual(t, frames, "advance_step", AdvanceStepFrame{
		Type:   MsgAdvanceStep,
		StepID: "step-2",
		Reason: "user_done",
	})
	assertVoiceAgentWireEqual(t, frames, "audio_end", envelope{Type: MsgAudioEnd})
	assertVoiceAgentWireEqual(t, frames, "ping", envelope{Type: MsgPing})
	assertVoiceAgentWireEqual(t, frames, "stop", envelope{Type: MsgStop})
	assertVoiceAgentWireEqual(t, frames, "cancel", envelope{Type: MsgCancel})

	// ── server → client ──────────────────────────────────────────────────
	assertVoiceAgentWireEqual(t, frames, "state_session_ready", StateFrame{
		Type:             MsgState,
		EventFrameFields: EventFrameFields{EventType: EventSessionReady},
		State:            "listening",
		Provider:         "deepgram",
		MediaTransport:   MediaTransportWebSocket,
	})
	assertVoiceAgentWireEqual(t, frames, "state", StateFrame{
		Type:  MsgState,
		State: "speaking",
	})
	assertVoiceAgentWireEqual(t, frames, "input_transcript_partial", TranscriptFrame{
		Type:             MsgInputTranscript,
		EventFrameFields: EventFrameFields{EventType: EventInputPartial},
		Text:             "wie ist das",
		Done:             false,
	})
	assertVoiceAgentWireEqual(t, frames, "input_transcript_final", TranscriptFrame{
		Type:             MsgInputTranscript,
		EventFrameFields: EventFrameFields{EventType: EventInputFinal},
		Text:             "Wie ist das Wetter in Berlin?",
		Done:             true,
	})
	assertVoiceAgentWireEqual(t, frames, "input_transcript_speaker", TranscriptFrame{
		Type:              MsgInputTranscript,
		EventFrameFields:  EventFrameFields{EventType: EventInputFinal},
		Text:              "Wie ist das Wetter in Berlin?",
		Done:              true,
		SpeakerLabel:      "S1",
		PersonID:          "person-7",
		DisplayName:       "Marcel",
		SpeakerConfidence: 0.87,
	})
	assertVoiceAgentWireEqual(t, frames, "output_transcript", TranscriptFrame{
		Type:             MsgOutputTranscript,
		EventFrameFields: EventFrameFields{EventType: EventOutputText},
		Text:             "In Berlin sind es 21 Grad und sonnig.",
		Done:             true,
	})
	assertVoiceAgentWireEqual(t, frames, "tool_call", ToolCallFrame{
		Type:             MsgToolCall,
		EventFrameFields: EventFrameFields{EventType: EventToolCall},
		ID:               "t1",
		Name:             "weather",
		Args:             map[string]any{"city": "Berlin"},
	})
	assertVoiceAgentWireEqual(t, frames, "sequence_step", SequenceStepFrame{
		Type:       MsgSequenceStep,
		SequenceID: "seq-1",
		StepID:     "step-2",
		StepIndex:  2,
		Status:     "entered",
		Reason:     "advance_step",
	})
	assertVoiceAgentWireEqual(t, frames, "event", EventFrame{
		Type:             MsgEvent,
		EventFrameFields: EventFrameFields{EventType: EventTurnEnd},
	})
	assertVoiceAgentWireEqual(t, frames, "interrupted", InterruptedFrame{
		Type:             MsgInterrupted,
		EventFrameFields: EventFrameFields{EventType: EventInterrupted},
	})
	assertVoiceAgentWireEqual(t, frames, "error", ErrorFrame{
		Type:    MsgError,
		Code:    "provider_unavailable",
		Message: `provider "gemini" is not configured on this server`,
	})
	assertVoiceAgentWireEqual(t, frames, "session_end", SessionEndFrame{
		Type:             MsgSessionEnd,
		EventFrameFields: EventFrameFields{EventType: EventSessionEnd},
		Reason:           "idle",
	})
	assertVoiceAgentWireEqual(t, frames, "pong", PongFrame{Type: MsgPong})
}

// TestVoiceAgentFixture_CoversEveryMessageType fails when a message type is
// added to protocol.go without extending the golden fixture (or a fixture
// frame carries a type the protocol does not define).
func TestVoiceAgentFixture_CoversEveryMessageType(t *testing.T) {
	protocolTypes := map[string]bool{
		// client → server
		MsgStart:        true,
		MsgAudioEnd:     true,
		MsgText:         true,
		MsgToolResponse: true,
		MsgPing:         true,
		MsgStop:         true,
		MsgAdvanceStep:  true,
		MsgCancel:       true,
		// server → client
		MsgState:            true,
		MsgInputTranscript:  true,
		MsgOutputTranscript: true,
		MsgToolCall:         true,
		MsgSequenceStep:     true,
		MsgEvent:            true,
		MsgInterrupted:      true,
		MsgError:            true,
		MsgSessionEnd:       true,
		MsgPong:             true,
	}

	frames := loadVoiceAgentFixture(t)
	covered := map[string]bool{}
	for name := range frames {
		frame := fixtureFrame(t, frames, name)
		typ, _ := frame["type"].(string)
		if typ == "" {
			t.Errorf("fixture frame %q has no type field", name)
			continue
		}
		if !protocolTypes[typ] {
			t.Errorf("fixture frame %q carries type %q which protocol.go does not define", name, typ)
			continue
		}
		covered[typ] = true
	}
	for typ := range protocolTypes {
		if !covered[typ] {
			t.Errorf("protocol message type %q has no golden frame in %s", typ, fixturePath)
		}
	}
}
