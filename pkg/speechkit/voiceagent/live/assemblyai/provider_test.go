package assemblyai

import (
	"encoding/base64"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
)

func TestAssemblyAISessionUpdateMapsNativeKeytermsAndControls(t *testing.T) {
	cfg := live.LiveConfig{
		FrameworkPrompt:  "You are a concise voice assistant.",
		RefinementPrompt: "Keep answers short.",
		VocabularyHint:   "Prefer recognition of Kombify and SpeechKit in responses.",
		Voice:            "ivy",
		ProviderOptions: provideropts.Values{
			provideropts.OptionKeyterms:      []string{"Kombify", "SpeechKit"},
			provideropts.OptionContextPrompt: "Current app: SpeechKit provider switching.",
		},
		Policies: live.LivePolicies{
			ActivityDetection: live.ActivityDetectionPolicy{
				SilenceDurationMs: 700,
				ActivityHandling:  live.ActivityHandlingNoInterrupt,
			},
		},
		Tools: []live.ToolDefinition{{
			Name:        "lookup",
			Description: "Look up a value.",
			ParametersJSONSchema: map[string]any{
				"type": "object",
			},
		}},
	}

	update := assemblyAISessionUpdate(cfg)
	session := update["session"].(map[string]any)
	input := session["input"].(map[string]any)
	output := session["output"].(map[string]any)

	if got := update["type"]; got != "session.update" {
		t.Fatalf("type = %v", got)
	}
	if got := output["voice"]; got != "ivy" {
		t.Fatalf("voice = %v", got)
	}
	keyterms := input["keyterms"].([]string)
	if strings.Join(keyterms, ",") != "Kombify,SpeechKit" {
		t.Fatalf("keyterms = %v", keyterms)
	}
	if strings.Contains(strings.Join(keyterms, "\n"), "Prefer recognition") {
		t.Fatalf("prose vocabulary hint leaked into native keyterms: %v", keyterms)
	}
	turnDetection := input["turn_detection"].(map[string]any)
	if got := turnDetection["min_silence"]; got != 700 {
		t.Fatalf("min_silence = %v", got)
	}
	if got := turnDetection["interrupt_response"]; got != false {
		t.Fatalf("interrupt_response = %v", got)
	}
	if got := session["system_prompt"].(string); !strings.Contains(got, "Keep answers short.") ||
		!strings.Contains(got, "Current app: SpeechKit provider switching.") {
		t.Fatalf("system_prompt = %q", got)
	}
	tools := session["tools"].([]map[string]any)
	if len(tools) != 1 || tools[0]["name"] != "lookup" {
		t.Fatalf("tools = %#v", tools)
	}
}

func TestAssemblyAISessionUpdateFallsBackForProviderForeignVoice(t *testing.T) {
	update := assemblyAISessionUpdate(live.LiveConfig{Voice: "Kore"})
	session := update["session"].(map[string]any)
	output := session["output"].(map[string]any)
	if got := output["voice"]; got != assemblyAIDefaultVoice {
		t.Fatalf("voice = %v, want %s", got, assemblyAIDefaultVoice)
	}
}

func TestAssemblyAISessionUpdateKeepsLegacyPlainVocabularyFallback(t *testing.T) {
	update := assemblyAISessionUpdate(live.LiveConfig{VocabularyHint: "Kombify, SpeechKit"})
	session := update["session"].(map[string]any)
	input := session["input"].(map[string]any)

	keyterms := input["keyterms"].([]string)
	if strings.Join(keyterms, ",") != "Kombify,SpeechKit" {
		t.Fatalf("keyterms = %v", keyterms)
	}
}

func TestAssemblyAIParseEventMapsProviderEvents(t *testing.T) {
	provider := New()
	audio := []byte{1, 2, 3, 4}
	encodedAudio := base64.StdEncoding.EncodeToString(audio)

	cases := []struct {
		name              string
		event             string
		wantEvent         live.LiveEventType
		wantProviderEvent string
		check             func(*testing.T, *live.LiveMessage)
	}{
		{
			name:              "reply audio",
			event:             `{"type":"reply.audio","data":"` + encodedAudio + `"}`,
			wantEvent:         live.LiveEventOutputAudio,
			wantProviderEvent: "reply.audio",
			check: func(t *testing.T, msg *live.LiveMessage) {
				if string(msg.Audio) != string(audio) {
					t.Fatalf("audio = %v", msg.Audio)
				}
			},
		},
		{
			name:              "user delta",
			event:             `{"type":"transcript.user.delta","text":"hel"}`,
			wantEvent:         live.LiveEventInputPartial,
			wantProviderEvent: "transcript.user.delta",
			check: func(t *testing.T, msg *live.LiveMessage) {
				if msg.InputTranscript != "hel" || msg.InputTranscriptDone {
					t.Fatalf("input transcript = %+v", msg)
				}
			},
		},
		{
			name:              "user final",
			event:             `{"type":"transcript.user","text":"hello"}`,
			wantEvent:         live.LiveEventInputFinal,
			wantProviderEvent: "transcript.user",
			check: func(t *testing.T, msg *live.LiveMessage) {
				if msg.InputTranscript != "hello" || !msg.InputTranscriptDone {
					t.Fatalf("input final = %+v", msg)
				}
			},
		},
		{
			name:              "agent transcript interrupted",
			event:             `{"type":"transcript.agent","text":"answer","interrupted":true}`,
			wantEvent:         live.LiveEventOutputText,
			wantProviderEvent: "transcript.agent",
			check: func(t *testing.T, msg *live.LiveMessage) {
				if msg.Text != "answer" || msg.OutputTranscript != "answer" || !msg.OutputTranscriptDone || !msg.Interrupted {
					t.Fatalf("agent transcript = %+v", msg)
				}
				if !live.EventTypesContain(msg.EventTypes, live.LiveEventInterrupted) {
					t.Fatalf("event types = %v, want interrupted", msg.EventTypes)
				}
			},
		},
		{
			name:              "reply done interrupted",
			event:             `{"type":"reply.done","status":"interrupted"}`,
			wantEvent:         live.LiveEventTurnEnd,
			wantProviderEvent: "reply.done",
			check: func(t *testing.T, msg *live.LiveMessage) {
				if !msg.Done || !msg.Interrupted {
					t.Fatalf("reply done = %+v", msg)
				}
			},
		},
		{
			name:              "tool call",
			event:             `{"type":"tool.call","call_id":"call-1","name":"lookup","arguments":{"city":"Berlin"}}`,
			wantEvent:         live.LiveEventToolCall,
			wantProviderEvent: "tool.call",
			check: func(t *testing.T, msg *live.LiveMessage) {
				if len(msg.ToolCalls) != 1 || msg.ToolCalls[0].ID != "call-1" || msg.ToolCalls[0].Name != "lookup" {
					t.Fatalf("tool calls = %+v", msg.ToolCalls)
				}
				if msg.ToolCalls[0].Args["city"] != "Berlin" {
					t.Fatalf("tool args = %+v", msg.ToolCalls[0].Args)
				}
			},
		},
		{
			name:              "session ended",
			event:             `{"type":"session.ended"}`,
			wantEvent:         live.LiveEventSessionEnd,
			wantProviderEvent: "session.ended",
			check: func(t *testing.T, msg *live.LiveMessage) {
				if !msg.Done || !msg.GoAway {
					t.Fatalf("session ended = %+v", msg)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg, swallow, err := provider.parseEvent([]byte(tc.event))
			if err != nil {
				t.Fatalf("parseEvent: %v", err)
			}
			if swallow {
				t.Fatal("parseEvent swallowed event")
			}
			if msg.EventType != tc.wantEvent {
				t.Fatalf("EventType = %q, want %q", msg.EventType, tc.wantEvent)
			}
			if !live.EventTypesContain(msg.EventTypes, tc.wantEvent) {
				t.Fatalf("EventTypes = %v, want %q", msg.EventTypes, tc.wantEvent)
			}
			if msg.ProviderMetadata["provider_event"] != tc.wantProviderEvent {
				t.Fatalf("ProviderMetadata = %#v", msg.ProviderMetadata)
			}
			tc.check(t, msg)
		})
	}
}

func TestAssemblyAIParseEventReturnsSessionErrors(t *testing.T) {
	_, _, err := New().parseEvent([]byte(`{"type":"session.error","code":"bad_request","message":"invalid","param":"session"}`))
	if err == nil || !strings.Contains(err.Error(), "bad_request") || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("session error = %v", err)
	}
}

func TestAssemblyAIReconnectRequiresSessionID(t *testing.T) {
	var _ live.LiveReconnector = New()
	if err := New().Reconnect(t.Context()); err == nil || !strings.Contains(err.Error(), "no resumable session id") {
		t.Fatalf("Reconnect without session = %v", err)
	}
}
