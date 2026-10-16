//go:build linux

package voiceagent

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/a2a"
)

// Sensitive provider boundary: a delegating vendor (Gemini Live here) never
// receives the persona prompt or tools, each delegation runs exactly one turn
// of the bound registered agent on the caller's transcript, the answer goes
// back to the vendor only, and a retransmitted delegation cannot replay it.
func TestNativeDelegationRunsOneRegisteredAgentTurn(t *testing.T) {
	var executions atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		executions.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"result\":{\"parts\":[{\"kind\":\"text\",\"text\":\"Your table is booked.\"}]}}\n\n")
	}))
	defer upstream.Close()
	sessions := &nativeVoiceSessions{signingSecret: func() string { return "fixture-secret" }, callbacks: make(map[string]*a2a.OpenAIHandler),
		consentReader: func(_ context.Context, cfg LiveConfigFrame) (NativeVoiceConsent, error) {
			return cfg.NativeConsent, nil
		}}
	media := newFakeProvider()
	provider := &registeredNativeProvider{LiveProviderAdapter: media, sessions: sessions, provider: "gemini"}
	defer func() { _ = provider.Close() }()
	cfg := nativeConsentTestConfig()
	cfg.AgentEndpoint = upstream.URL
	cfg.SystemPrompt = "Persona prompt that must not reach the vendor."
	cfg.Tools = []ToolDefinitionFrame{{Name: "persona_tool"}}
	if err := provider.Connect(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	media.mu.Lock()
	vendor := *media.connectCfg
	media.mu.Unlock()
	if strings.Contains(vendor.SystemPrompt, "Persona") || len(vendor.Tools) != 1 || vendor.Tools[0].Name == "persona_tool" {
		t.Fatal("vendor received persona instructions or tools instead of the single delegation")
	}
	delegation := vendor.Tools[0].Name

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	turn := func() ToolResponseFrame {
		media.mu.Lock()
		answered := len(media.toolResponses)
		media.mu.Unlock()
		media.receiveQueue <- &LiveMessage{InputTranscript: "Book a table"}
		media.receiveQueue <- &LiveMessage{ToolCalls: []ToolCall{{ID: "call-1", Name: delegation}}}
		media.receiveQueue <- &LiveMessage{Audio: []byte{1}} // Vendor's own speech before the result.
		if msg, err := provider.Receive(ctx); err != nil || msg.InputTranscript == "" {
			t.Fatalf("caller transcript did not reach the client: %v", err)
		}
		spoken := make(chan *LiveMessage, 1)
		go func() {
			msg, _ := provider.Receive(ctx)
			spoken <- msg
		}()
		var response ToolResponseFrame
		for response.ID == "" && ctx.Err() == nil {
			time.Sleep(10 * time.Millisecond)
			media.mu.Lock()
			if len(media.toolResponses) > answered {
				response = media.toolResponses[answered]
			}
			media.mu.Unlock()
		}
		media.receiveQueue <- &LiveMessage{Audio: []byte{2}} // Speech answering the result.
		media.receiveQueue <- &LiveMessage{Done: true}
		if msg := <-spoken; msg == nil || len(msg.ToolCalls) != 0 || len(msg.Audio) != 1 || msg.Audio[0] != 2 {
			t.Fatalf("client got %+v; want only the speech that answers the result", msg)
		}
		if msg, err := provider.Receive(ctx); err != nil || !msg.Done {
			t.Fatalf("vendor turn end did not reach the client: %v", err)
		}
		return response
	}
	first := turn()
	if result, _ := first.Response["result"].(string); first.ID != "call-1" || !strings.HasSuffix(result, "Your table is booked.") || !strings.Contains(result, "AI") {
		t.Fatalf("registered agent answer did not reach the vendor with the disclosure: %v", first.Response)
	}
	if replay := turn(); replay.Response["error"] == nil || strings.Contains(fmt.Sprint(replay.Response), "booked") || executions.Load() != 1 {
		t.Fatalf("retransmitted delegation replayed the registered agent turn: %v", replay.Response)
	}
}

// Vendor errors and resumption notices are not delegation traffic: the speech
// gate must not swallow them.
func TestNativeDelegationForwardsVendorStatus(t *testing.T) {
	media := newFakeProvider()
	provider := &registeredNativeProvider{LiveProviderAdapter: media, provider: "xai"}
	media.receiveQueue <- &LiveMessage{Audio: []byte{1}}
	media.receiveQueue <- &LiveMessage{ErrorCode: "rate_limited"}
	media.receiveQueue <- &LiveMessage{SessionResumable: true}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if msg, err := provider.Receive(ctx); err != nil || msg.ErrorCode != "rate_limited" {
		t.Fatalf("vendor error did not reach the client: %+v %v", msg, err)
	}
	if msg, err := provider.Receive(ctx); err != nil || !msg.SessionResumable {
		t.Fatalf("session resumption did not reach the client: %+v %v", msg, err)
	}
}
