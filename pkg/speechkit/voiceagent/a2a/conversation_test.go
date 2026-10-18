package a2a

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/cascaded"
)

func TestConversationSpeechWaitsForPanelAndHonorsCancellation(t *testing.T) {
	for _, cancelSpeech := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelSpeech), func(t *testing.T) {
			control, _ := NewConversationSession(ConversationCapability{Version: 1, Mode: "oneshot", VoiceSessionID: "voice-1", ConversationSessionID: "assist-1", OneShotSessionID: "assist-1"}, func(context.Context, VoiceModeReceipt) error { return nil })
			panelReady, releasePanel := make(chan struct{}), make(chan struct{})
			var speechCalls atomic.Int32
			client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				var wire struct {
					ID     string `json:"id"`
					Params struct {
						Message struct {
							ContextID string `json:"contextId"`
							Parts     []struct {
								Kind string `json:"kind"`
							} `json:"parts"`
						} `json:"message"`
					} `json:"params"`
				}
				_ = json.NewDecoder(request.Body).Decode(&wire)
				body := `{"result":{"parts":[{"kind":"text","text":"Ready"}]}}`
				if wire.Params.Message.Parts[0].Kind == "data" {
					close(panelReady)
					<-releasePanel
					body = fmt.Sprintf(`{"id":%q,"result":{"metadata":{"io.kombify.voice_mode":{"version":1,"mode":"conversation","voice_session_id":"voice-1","conversation_session_id":"conversation-1","request_id":"panel-1","status":"accepted"}}}}`, wire.ID)
				} else {
					speechCalls.Add(1)
					if wire.Params.Message.ContextID != "conversation-1" {
						t.Error("queued utterance used stale history")
					}
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			agent, _ := New(Config{Endpoint: "https://agents.example.test/a2a/agents/assistant", TargetAgentID: "assistant", SessionID: "assist-1", Conversation: control, HTTPClient: client})
			panelDone := make(chan error, 1)
			go func() { panelDone <- agent.SetVoiceMode(context.Background(), "conversation", "panel-1") }()
			<-panelReady
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			speechDone := make(chan error, 1)
			go func() { _, err := agent.Run(ctx, cascaded.AgentInput{Utterance: "Continue"}); speechDone <- err }()
			deadline := time.After(time.Second)
			for len(agent.speechWaiter) == 0 {
				select {
				case err := <-speechDone:
					t.Fatalf("utterance dropped during panel: %v", err)
				case <-deadline:
					t.Fatal("utterance did not wait")
				case <-time.After(time.Millisecond):
				}
			}
			if _, err := agent.Run(context.Background(), cascaded.AgentInput{Utterance: "Extra"}); !errors.Is(err, ErrConversationBusy) {
				t.Fatal("extra waiter admitted", err)
			}
			if cancelSpeech {
				cancel()
				if err := <-speechDone; !errors.Is(err, context.Canceled) {
					t.Fatal("wait ignored cancellation", err)
				}
			}
			close(releasePanel)
			if err := <-panelDone; err != nil {
				t.Fatal(err)
			}
			if !cancelSpeech {
				if err := <-speechDone; err != nil {
					t.Fatal("queued utterance failed", err)
				}
			}
			wantCalls := int32(1)
			if cancelSpeech {
				wantCalls = 0
			}
			if speechCalls.Load() != wantCalls {
				t.Fatal("unexpected speech dispatch", speechCalls.Load())
			}
		})
	}
}

// A mode action must never cross a voice lifetime or become a spoken promise
// before delivery; subsequent reasoning must use its separate canonical history.
func TestConversationControlFencesVoiceHistoryAndAnswer(t *testing.T) {
	for _, voiceID := range []string{"voice-1", "foreign-voice"} {
		t.Run(voiceID, func(t *testing.T) {
			var effects []string
			control, err := NewConversationSession(ConversationCapability{Version: 1, Mode: "oneshot", VoiceSessionID: "voice-1", ConversationSessionID: "assist-1", OneShotSessionID: "assist-1"},
				func(context.Context, VoiceModeReceipt) error { effects = append(effects, "delivered"); return nil })
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				calls++
				var wire struct {
					ID     string `json:"id"`
					Params struct {
						Message struct {
							ContextID string `json:"contextId"`
						} `json:"message"`
					} `json:"params"`
				}
				if err := json.NewDecoder(request.Body).Decode(&wire); err != nil {
					t.Fatal(err)
				}
				if calls == 2 && wire.Params.Message.ContextID != "conversation-1" {
					t.Fatal("next turn used Assist history")
				}
				body := `data: {"result":{"parts":[{"kind":"text","text":"Answer"}]}}` + "\n\n"
				if calls == 1 {
					body = fmt.Sprintf("data: {\"id\":%q,\"result\":{\"metadata\":{\"io.kombify.voice_mode\":{\"version\":1,\"mode\":\"conversation\",\"voice_session_id\":%q,\"conversation_session_id\":\"conversation-1\",\"request_id\":\"tool-1\",\"status\":\"accepted\"}}}}\n\n", wire.ID, voiceID) + body
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			agent, err := New(Config{Endpoint: "https://agents.example.test/a2a/agents/assistant", TargetAgentID: "assistant", SessionID: "assist-1", Conversation: control, HTTPClient: client})
			if err != nil {
				t.Fatal(err)
			}
			_, err = agent.StreamTurn(context.Background(), cascaded.AgentInput{Utterance: "Start a conversation"}, "turn-1", func(string) error { effects = append(effects, "spoken"); return nil })
			if voiceID != "voice-1" {
				if err == nil || len(effects) != 0 || control.Snapshot().ConversationSessionID != "assist-1" {
					t.Fatal("foreign voice control changed history or reached speech", err, effects)
				}
				return
			}
			if err != nil || strings.Join(effects, ",") != "delivered,spoken" {
				t.Fatal("mode promise overtook its delivery", err, effects)
			}
			if _, err := agent.Run(context.Background(), cascaded.AgentInput{Utterance: "Continue"}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// A panel request is consumed before a possibly uncertain HTTP effect.
func TestConversationPanelDoesNotReplayUncertainCommand(t *testing.T) {
	control, _ := NewConversationSession(ConversationCapability{Version: 1, Mode: "oneshot", VoiceSessionID: "voice-1", ConversationSessionID: "assist-1", OneShotSessionID: "assist-1"}, func(context.Context, VoiceModeReceipt) error { return nil })
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, io.ErrUnexpectedEOF })}
	agent, _ := New(Config{Endpoint: "https://agents.example.test/a2a/agents/assistant", TargetAgentID: "assistant", SessionID: "assist-1", Conversation: control, HTTPClient: client})
	if agent.SetVoiceMode(context.Background(), "conversation", "panel-1") == nil {
		t.Fatal("uncertain command succeeded")
	}
	if agent.SetVoiceMode(context.Background(), "conversation", "panel-1") == nil || calls != 1 {
		t.Fatal("uncertain panel command replayed")
	}
}

func TestConversationPanelBusyHasNoEffectAndDoesNotConsumeCommand(t *testing.T) {
	control, _ := NewConversationSession(ConversationCapability{Version: 1, Mode: "oneshot", VoiceSessionID: "voice-1", ConversationSessionID: "assist-1", OneShotSessionID: "assist-1"}, func(context.Context, VoiceModeReceipt) error { return nil })
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, io.ErrUnexpectedEOF })}
	agent, _ := New(Config{Endpoint: "https://agents.example.test/a2a/agents/assistant", TargetAgentID: "assistant", SessionID: "assist-1", Conversation: control, HTTPClient: client})
	agent.conversationTurn <- struct{}{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := agent.SetVoiceMode(ctx, "conversation", "panel-1"); !errors.Is(err, ErrConversationBusy) || calls != 0 {
		t.Fatal("busy panel command dispatched or lost its explicit no-effect outcome", err, calls)
	}
	<-agent.conversationTurn
	// A deliberate later request can dispatch: busy did not consume its ID.
	_ = agent.SetVoiceMode(context.Background(), "conversation", "panel-1")
	if calls != 1 || control.Snapshot().Mode != "oneshot" {
		t.Fatal("busy command changed mode or consumed its ID")
	}
}

func TestConversationRejectedReceiptStopsLaterSpokenPromise(t *testing.T) {
	delivered, spoken := false, false
	control, _ := NewConversationSession(ConversationCapability{Version: 1, Mode: "oneshot", VoiceSessionID: "voice-1", ConversationSessionID: "assist-1", OneShotSessionID: "assist-1"}, func(_ context.Context, receipt VoiceModeReceipt) error {
		delivered = receipt.Status == "rejected"
		return nil
	})
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var wire struct {
			ID string `json:"id"`
		}
		_ = json.NewDecoder(request.Body).Decode(&wire)
		body := fmt.Sprintf("data: {\"id\":%q,\"result\":{\"metadata\":{\"io.kombify.voice_mode\":{\"version\":1,\"mode\":\"oneshot\",\"voice_session_id\":\"voice-1\",\"conversation_session_id\":\"assist-1\",\"request_id\":\"tool-1\",\"status\":\"rejected\",\"code\":\"session_owner_scope_denied\"}}}}\n\ndata: {\"result\":{\"parts\":[{\"kind\":\"text\",\"text\":\"Conversation started\"}]}}\n\n", wire.ID)
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	agent, _ := New(Config{Endpoint: "https://agents.example.test/a2a/agents/assistant", TargetAgentID: "assistant", SessionID: "assist-1", Conversation: control, HTTPClient: client})
	_, err := agent.Stream(context.Background(), cascaded.AgentInput{Utterance: "Start a conversation"}, func(string) error { spoken = true; return nil })
	if err == nil || !delivered || spoken || control.Snapshot().Mode != "oneshot" {
		t.Fatal("denied control became a spoken promise", err)
	}
}
