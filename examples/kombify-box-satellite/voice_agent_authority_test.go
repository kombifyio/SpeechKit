//go:build windows && cgo

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/agentkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
)

const authoritativeHAText = "The kitchen light is on."

func TestHATurnGateAllowsGeneralConversation(t *testing.T) {
	gate := newHATurnGate(newHABridge(&Config{}), "en-US")
	gate.beginSession("test-session")
	turnID := gate.beginTurn()
	observeSealedInput(t, gate, turnID, "Explain photosynthesis.")
	mustCloseInput(t, gate, turnID)
	gate.observeAudio()

	allowed, reason := gate.finishTurn(turnID)
	if !allowed || reason != "general_conversation" {
		t.Fatalf("finishTurn allowed=%v reason=%q", allowed, reason)
	}
}

func TestHATurnGateAuthorizesExactFinalTranscriptAndNarrowsArgs(t *testing.T) {
	gate := newHATurnGate(newHABridge(&Config{}), "de-DE")
	gate.beginSession("current-session")
	turnID := gate.beginTurn()
	const finalTranscript = "  schalte das Küchenlicht\n an  "
	observeSealedInput(t, gate, turnID, finalTranscript)
	mustCloseInput(t, gate, turnID)

	call := agentkit.ToolCall{
		ID:   "exact-call",
		Name: intentHomeAssistant,
		Args: map[string]any{
			"query":  "schalte  das Küchenlicht an",
			"locale": "model-controlled",
			"extra":  "must be dropped",
		},
	}
	args, err := gate.authorizeToolCall("current-session", call)
	if err != nil {
		t.Fatalf("authorize exact final transcript: %v", err)
	}
	if args["query"] != finalTranscript || args["locale"] != "de-DE" {
		t.Fatalf("authorized args = %#v", args)
	}
	if _, exists := args["extra"]; exists {
		t.Fatalf("model-controlled extra argument survived: %#v", args)
	}

	response := agentkit.ToolResponse{
		ID:       call.ID,
		Name:     call.Name,
		Response: map[string]any{"matched": true, "text": "Das Küchenlicht ist an."},
	}
	gate.observeToolResult("current-session", call, response)
	gate.observeAudio()
	gate.observeOutputTranscript("Das Küchenlicht ist an.", true)
	if allowed, reason := gate.finishTurn(turnID); !allowed || reason != "home_assistant_receipt_verified" {
		t.Fatalf("exact host-bound call allowed=%v reason=%q", allowed, reason)
	}
}

func TestHATurnGateRejectsUnboundToolCalls(t *testing.T) {
	tests := []struct {
		name       string
		transcript string
		query      string
		sessionID  string
		callID     string
	}{
		{
			name:       "general prompt cannot authorize Home Assistant",
			transcript: "Explain photosynthesis.",
			query:      "Explain photosynthesis.",
			sessionID:  "current-session",
			callID:     "general-call",
		},
		{
			name:       "model cannot expand the user command",
			transcript: "turn on the kitchen light",
			query:      "turn off every light",
			sessionID:  "current-session",
			callID:     "changed-query",
		},
		{
			name:       "foreign session cannot reuse the active turn",
			transcript: "turn on the kitchen light",
			query:      "turn on the kitchen light",
			sessionID:  "stale-session",
			callID:     "stale-call",
		},
		{
			name:       "empty call id is not bindable",
			transcript: "turn on the kitchen light",
			query:      "turn on the kitchen light",
			sessionID:  "current-session",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gate := newHATurnGate(newHABridge(&Config{}), "en-US")
			gate.beginSession("current-session")
			turnID := gate.beginTurn()
			observeSealedInput(t, gate, turnID, tc.transcript)
			mustCloseInput(t, gate, turnID)
			_, err := gate.authorizeToolCall(tc.sessionID, agentkit.ToolCall{
				ID:   tc.callID,
				Name: intentHomeAssistant,
				Args: map[string]any{"query": tc.query},
			})
			if err == nil {
				t.Fatal("unbound Home Assistant call was authorized")
			}
			if allowed, reason := gate.finishTurn(turnID); allowed || reason != "home_assistant_tool_call_rejected" {
				t.Fatalf("rejected call allowed=%v reason=%q", allowed, reason)
			}
		})
	}
}

func TestHATurnGateRejectsProviderCommandThatOmitsLaterHostCapturedCorrection(t *testing.T) {
	gate := newHATurnGate(newHABridge(&Config{}), "en-US")
	gate.beginSession("current-session")
	turnID := gate.beginTurn()
	// The host transcript covers the complete capture. The realtime provider
	// first exposes an actionable partial segment and only later emits the
	// correction. Neither provider segment may replace the sealed authority.
	mustSealHostTranscript(t, gate, turnID, "turn on the kitchen light actually do not")
	gate.observeInputTranscript("turn on the kitchen light", true)
	mustCloseInput(t, gate, turnID)

	_, err := gate.authorizeToolCall("current-session", agentkit.ToolCall{
		ID:   "segmented-command",
		Name: intentHomeAssistant,
		Args: map[string]any{"query": "turn on the kitchen light"},
	})
	if err == nil {
		t.Fatal("provider partial transcript bypassed the sealed whole-capture transcript")
	}
	gate.observeInputTranscript("actually do not", true)
	if allowed, reason := gate.finishTurn(turnID); allowed || reason != "home_assistant_tool_call_rejected" {
		t.Fatalf("segmented input allowed=%v reason=%q", allowed, reason)
	}
}

func TestLocalVoiceAgentSealsTheCompleteBufferedCaptureBeforeProviderSegments(t *testing.T) {
	cfg := &Config{}
	cfg.VoiceAgent.Locale = "en-US"
	provider := &fakeAuthoritySTT{text: "turn on the kitchen light actually do not"}
	gate := newHATurnGate(newHABridge(cfg), cfg.VoiceAgent.Locale)
	gate.beginSession("current-session")
	turnID := gate.beginTurn()
	runtime := &localVoiceAgentRuntime{cfg: cfg, authority: gate, authoritySTT: provider}
	pcm := bytes.Repeat([]byte{1, 2, 3, 4}, 900)

	if !runtime.sealAuthorityTranscript(context.Background(), turnID, pcm) {
		t.Fatal("complete host capture was not sealed")
	}
	if !bytes.HasSuffix(provider.audio, pcm) {
		t.Fatal("authority STT did not receive the complete immutable capture")
	}
	if provider.opts.Language != cfg.VoiceAgent.Locale {
		t.Fatalf("authority STT locale = %q, want %q", provider.opts.Language, cfg.VoiceAgent.Locale)
	}
	gate.observeInputTranscript("turn on the kitchen light", true)
	mustCloseInput(t, gate, turnID)
	if _, err := gate.authorizeToolCall("current-session", agentkit.ToolCall{
		ID:   "provider-partial",
		Name: intentHomeAssistant,
		Args: map[string]any{"query": "turn on the kitchen light"},
	}); err == nil {
		t.Fatal("provider segment replaced the complete host transcript")
	}
}

func TestLocalVoiceAgentMissingOrFailedAuthoritySTTFailsClosed(t *testing.T) {
	tests := []struct {
		name     string
		provider stt.STTProvider
	}{
		{name: "missing provider"},
		{name: "provider error", provider: &fakeAuthoritySTT{err: errors.New("unavailable")}},
		{name: "empty transcript", provider: &fakeAuthoritySTT{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{}
			cfg.VoiceAgent.Locale = "en-US"
			gate := newHATurnGate(newHABridge(cfg), cfg.VoiceAgent.Locale)
			gate.beginSession("current-session")
			turnID := gate.beginTurn()
			runtime := &localVoiceAgentRuntime{cfg: cfg, authority: gate, authoritySTT: tc.provider}
			if runtime.sealAuthorityTranscript(context.Background(), turnID, []byte{1, 2, 3, 4}) {
				t.Fatal("unavailable authority STT produced a seal")
			}
			gate.observeInputTranscript("turn on the kitchen light", true)
			mustCloseInput(t, gate, turnID)
			if _, err := gate.authorizeToolCall("current-session", agentkit.ToolCall{
				ID:   "must-not-run",
				Name: intentHomeAssistant,
				Args: map[string]any{"query": "turn on the kitchen light"},
			}); err == nil {
				t.Fatal("Home Assistant call was authorized without a host transcript seal")
			}
		})
	}
}

func TestHATurnGateNeverUsesProviderTranscriptAsPlaybackAuthority(t *testing.T) {
	gate := newHATurnGate(newHABridge(&Config{}), "en-US")
	gate.beginSession("current-session")
	turnID := gate.beginTurn()
	// A compromised provider labels an actual smart-home turn as general and
	// supplies plausible audio. Without an independent local host seal, none of
	// it may become playable.
	gate.observeInputTranscript("Explain photosynthesis.", true)
	mustCloseInput(t, gate, turnID)
	gate.observeAudio()
	gate.observeOutputTranscript("The kitchen light is on.", true)
	if allowed, reason := gate.finishTurn(turnID); allowed || reason != "host_input_seal_missing" {
		t.Fatalf("provider-only authority allowed=%v reason=%q", allowed, reason)
	}
}

func TestSendBufferedInputPreservesBytesAndChunkBounds(t *testing.T) {
	input := make([]byte, 6501)
	for i := range input {
		input[i] = byte(i % 251)
	}
	var chunks [][]byte
	if err := sendBufferedInput(input, func(chunk []byte) error {
		chunks = append(chunks, append([]byte(nil), chunk...))
		return nil
	}); err != nil {
		t.Fatalf("send buffered input: %v", err)
	}
	if len(chunks) != 3 || len(chunks[0]) != 3200 || len(chunks[1]) != 3200 || len(chunks[2]) != 101 {
		t.Fatalf("unexpected chunk sizes: %d/%d/%d", len(chunks[0]), len(chunks[1]), len(chunks[2]))
	}
	if got := bytes.Join(chunks, nil); !bytes.Equal(got, input) {
		t.Fatal("buffered input changed while chunking")
	}
}

func TestHATurnGateRequiresHostCaptureCloseBeforeAuthorization(t *testing.T) {
	gate := newHATurnGate(newHABridge(&Config{}), "en-US")
	gate.beginSession("current-session")
	turnID := gate.beginTurn()
	observeSealedInput(t, gate, turnID, "turn on the kitchen light")

	call, _ := authorityCallAndResponse("early-server-vad-call", authoritativeHAText)
	if _, err := gate.authorizeToolCall("current-session", call); err == nil {
		t.Fatal("provider-final transcript authorized a side effect before host capture closed")
	}
	mustCloseInput(t, gate, turnID)
	call.ID = "retry-after-close"
	if _, err := gate.authorizeToolCall("current-session", call); err == nil {
		t.Fatal("turn recovered after an early unbound tool call")
	}
	if allowed, reason := gate.finishTurn(turnID); allowed || reason != "home_assistant_tool_call_rejected" {
		t.Fatalf("early call turn allowed=%v reason=%q", allowed, reason)
	}
}

func TestHATurnGateAuthorizesAtMostOneConcurrentHACall(t *testing.T) {
	gate := newHATurnGate(newHABridge(&Config{}), "en-US")
	gate.beginSession("current-session")
	turnID := gate.beginTurn()
	observeSealedInput(t, gate, turnID, "turn on the kitchen light")
	mustCloseInput(t, gate, turnID)

	const contenders = 32
	start := make(chan struct{})
	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < contenders; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			<-start
			_, err := gate.authorizeToolCall("current-session", agentkit.ToolCall{
				ID:   fmt.Sprintf("call-%d", id),
				Name: intentHomeAssistant,
				Args: map[string]any{"query": "turn on the kitchen light"},
			})
			if err == nil {
				successes.Add(1)
			}
		}(i)
	}
	close(start)
	wg.Wait()
	if got := successes.Load(); got != 1 {
		t.Fatalf("authorized Home Assistant calls = %d, want exactly 1", got)
	}
}

func TestHATurnGateRejectsCallbacksFromRetiredSession(t *testing.T) {
	gate := newHATurnGate(newHABridge(&Config{}), "en-US")
	gate.beginSession("old-session")
	oldTurn := gate.beginTurn()
	observeSealedInput(t, gate, oldTurn, "turn on the kitchen light")
	mustCloseInput(t, gate, oldTurn)
	gate.endSession("old-session")

	gate.beginSession("new-session")
	turnID := gate.beginTurn()
	observeSealedInput(t, gate, turnID, "turn on the kitchen light")
	mustCloseInput(t, gate, turnID)
	_, err := gate.authorizeToolCall("old-session", agentkit.ToolCall{
		ID:   "late-old-call",
		Name: intentHomeAssistant,
		Args: map[string]any{"query": "turn on the kitchen light"},
	})
	if err == nil {
		t.Fatal("retired session callback was authorized in the new turn")
	}
	if allowed, reason := gate.finishTurn(turnID); allowed || reason != "home_assistant_tool_call_rejected" {
		t.Fatalf("new turn after stale callback allowed=%v reason=%q", allowed, reason)
	}
}

func TestHATurnGateRejectsRetiredSessionResultWithReusedCallID(t *testing.T) {
	gate := newHATurnGate(newHABridge(&Config{}), "en-US")
	gate.beginSession("old-session")
	oldTurn := gate.beginTurn()
	observeSealedInput(t, gate, oldTurn, "turn on the kitchen light")
	mustCloseInput(t, gate, oldTurn)
	call, _ := authorityCallAndResponse("reused-call-id", "old result must not bind")
	if _, err := gate.authorizeToolCall("old-session", call); err != nil {
		t.Fatalf("authorize old call: %v", err)
	}
	gate.endSession("old-session")

	gate.beginSession("new-session")
	turnID := gate.beginTurn()
	observeSealedInput(t, gate, turnID, "turn on the kitchen light")
	mustCloseInput(t, gate, turnID)
	if _, err := gate.authorizeToolCall("new-session", call); err != nil {
		t.Fatalf("authorize current call: %v", err)
	}
	gate.observeToolResult("old-session", call, agentkit.ToolResponse{
		ID:       call.ID,
		Name:     call.Name,
		Response: map[string]any{"matched": true, "text": "old result must not bind"},
	})
	gate.observeToolResult("new-session", call, agentkit.ToolResponse{
		ID:       call.ID,
		Name:     call.Name,
		Response: map[string]any{"matched": true, "text": authoritativeHAText},
	})
	gate.observeAudio()
	gate.observeOutputTranscript(authoritativeHAText, true)
	if allowed, reason := gate.finishTurn(turnID); !allowed || reason != "home_assistant_receipt_verified" {
		t.Fatalf("current result after stale result allowed=%v reason=%q", allowed, reason)
	}
}

func TestHATurnGateRequiresSameTurnReceiptAndExactTranscript(t *testing.T) {
	tests := []struct {
		name        string
		configure   func(*haTurnGate)
		wantAllowed bool
		wantReason  string
	}{
		{
			name: "verified receipt and whitespace-normalized exact output",
			configure: func(g *haTurnGate) {
				call, response := authorityCallAndResponse("call-1", authoritativeHAText)
				mustAuthorizeHAToolCall(t, g, call)
				g.observeToolResult("test-session", call, response)
				g.observeAudio()
				g.observeOutputTranscript("  The  kitchen light\n is on. ", true)
			},
			wantAllowed: true,
			wantReason:  "home_assistant_receipt_verified",
		},
		{
			name: "missing receipt",
			configure: func(g *haTurnGate) {
				g.observeOutputTranscript(authoritativeHAText, true)
			},
			wantReason: "home_assistant_receipt_missing",
		},
		{
			name: "mismatched model output",
			configure: func(g *haTurnGate) {
				call, response := authorityCallAndResponse("call-2", authoritativeHAText)
				mustAuthorizeHAToolCall(t, g, call)
				g.observeToolResult("test-session", call, response)
				g.observeAudio()
				g.observeOutputTranscript("The kitchen light is probably on.", true)
			},
			wantReason: "output_transcript_mismatch",
		},
		{
			name: "missing final output transcript",
			configure: func(g *haTurnGate) {
				call, response := authorityCallAndResponse("call-3", authoritativeHAText)
				mustAuthorizeHAToolCall(t, g, call)
				g.observeToolResult("test-session", call, response)
				g.observeAudio()
			},
			wantReason: "output_transcript_missing",
		},
		{
			name: "multiple final output transcripts",
			configure: func(g *haTurnGate) {
				call, response := authorityCallAndResponse("call-multiple-output", authoritativeHAText)
				mustAuthorizeHAToolCall(t, g, call)
				g.observeToolResult("test-session", call, response)
				g.observeAudio()
				g.observeOutputTranscript(authoritativeHAText, true)
				g.observeOutputTranscript(authoritativeHAText, true)
			},
			wantReason: "output_transcript_ambiguous",
		},
		{
			name: "audio before receipt",
			configure: func(g *haTurnGate) {
				g.observeAudio()
				call, response := authorityCallAndResponse("call-4", authoritativeHAText)
				mustAuthorizeHAToolCall(t, g, call)
				g.observeToolResult("test-session", call, response)
				g.observeOutputTranscript(authoritativeHAText, true)
			},
			wantReason: "audio_preceded_home_assistant_receipt",
		},
		{
			name: "unmatched tool result",
			configure: func(g *haTurnGate) {
				call, response := authorityCallAndResponse("call-5", authoritativeHAText)
				response.Response["matched"] = false
				mustAuthorizeHAToolCall(t, g, call)
				g.observeToolResult("test-session", call, response)
				g.observeOutputTranscript(authoritativeHAText, true)
			},
			wantReason: "home_assistant_receipt_missing",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gate := newHATurnGate(newHABridge(&Config{}), "en-US")
			gate.beginSession("test-session")
			turnID := gate.beginTurn()
			observeSealedInput(t, gate, turnID, "turn on the kitchen light")
			mustCloseInput(t, gate, turnID)
			tc.configure(gate)
			allowed, reason := gate.finishTurn(turnID)
			if allowed != tc.wantAllowed || reason != tc.wantReason {
				t.Fatalf("finishTurn allowed=%v reason=%q, want allowed=%v reason=%q", allowed, reason, tc.wantAllowed, tc.wantReason)
			}
		})
	}
}

func TestHATurnGateSuppressesUnknownAndStaleReceipts(t *testing.T) {
	gate := newHATurnGate(newHABridge(&Config{}), "en-US")
	gate.beginSession("test-session")
	unknownTurn := gate.beginTurn()
	if allowed, reason := gate.finishTurn(unknownTurn); allowed || reason != "input_capture_unverified" {
		t.Fatalf("unknown turn allowed=%v reason=%q", allowed, reason)
	}

	oldTurn := gate.beginTurn()
	observeSealedInput(t, gate, oldTurn, "turn on the kitchen light")
	mustCloseInput(t, gate, oldTurn)
	call, response := authorityCallAndResponse("stale-call", authoritativeHAText)
	mustAuthorizeHAToolCall(t, gate, call)
	gate.abandonTurn(oldTurn)

	newTurn := gate.beginTurn()
	observeSealedInput(t, gate, newTurn, "turn on the kitchen light")
	mustCloseInput(t, gate, newTurn)
	gate.observeToolResult("test-session", call, response)
	gate.observeOutputTranscript(authoritativeHAText, true)
	if allowed, reason := gate.finishTurn(newTurn); allowed || reason != "home_assistant_receipt_missing" {
		t.Fatalf("stale receipt turn allowed=%v reason=%q", allowed, reason)
	}
}

func TestHATurnGateRejectsLateAudioAfterSuppressedHATurn(t *testing.T) {
	gate := newHATurnGate(newHABridge(&Config{}), "en-US")
	gate.beginSession("test-session")
	turnID := gate.beginTurn()
	observeSealedInput(t, gate, turnID, "turn on the kitchen light")
	mustCloseInput(t, gate, turnID)
	if allowed, reason := gate.finishTurn(turnID); allowed || reason != "home_assistant_receipt_missing" {
		t.Fatalf("suppressed turn allowed=%v reason=%q", allowed, reason)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	accepted := make(chan bool, 1)
	go func() {
		defer wg.Done()
		_, ok := gate.observeAudio()
		accepted <- ok
	}()
	wg.Wait()
	if <-accepted {
		t.Fatal("late audio from a closed HA generation was accepted")
	}
	if _, ok := gate.beginIdleOutput(1); ok {
		t.Fatal("suppressed HA generation allowed a later idle generation in the same session")
	}
}

func TestHATurnGateConcurrentFinishCannotLeavePlayableGeneration(t *testing.T) {
	for iteration := 0; iteration < 100; iteration++ {
		gate := newHATurnGate(newHABridge(&Config{}), "en-US")
		gate.beginSession("test-session")
		turnID := gate.beginTurn()
		observeSealedInput(t, gate, turnID, "turn on the kitchen light")
		mustCloseInput(t, gate, turnID)

		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, _ = gate.finishTurn(turnID)
		}()
		go func() {
			defer wg.Done()
			<-start
			generation, accepted := gate.observeAudio()
			if accepted && gate.acceptsAudioGeneration(generation) {
				// This can be true only while finishTurn has not acquired the
				// gate yet. The post-join assertion below is the playback barrier.
				return
			}
		}()
		close(start)
		wg.Wait()

		if gate.acceptsAudioGeneration(turnID) {
			t.Fatalf("iteration %d left closed generation %d playable", iteration, turnID)
		}
		if _, ok := gate.beginIdleOutput(1); ok {
			t.Fatalf("iteration %d reopened idle output after HA retirement", iteration)
		}
	}
}

func TestHATurnGateIdleGenerationIgnoresStaleListeningAndAllowsReminder(t *testing.T) {
	gate := newHATurnGate(newHABridge(&Config{}), "en-US")
	gate.beginSession("test-session")
	generalTurn := gate.beginTurn()
	observeSealedInput(t, gate, generalTurn, "Explain photosynthesis.")
	mustCloseInput(t, gate, generalTurn)
	if allowed, reason := gate.finishTurn(generalTurn); !allowed || reason != "general_conversation" {
		t.Fatalf("general turn allowed=%v reason=%q", allowed, reason)
	}
	idleGeneration, ok := gate.beginIdleOutput(41)
	if !ok || idleGeneration == 0 {
		t.Fatal("trusted idle prompt did not open a generation")
	}

	// A stale Listening transition before any output must not close or play the
	// newly announced reminder generation.
	if generation, ready := gate.finishIdleOutput(); ready || generation != 0 {
		t.Fatalf("stale transition completed idle generation=%d ready=%v", generation, ready)
	}
	gotGeneration, accepted := gate.observeAudio()
	if !accepted || gotGeneration != idleGeneration {
		t.Fatalf("idle audio generation=%d accepted=%v, want %d", gotGeneration, accepted, idleGeneration)
	}
	if generation, ready := gate.finishIdleOutput(); !ready || generation != idleGeneration {
		t.Fatalf("legitimate idle output generation=%d ready=%v, want %d", generation, ready, idleGeneration)
	}
	if !gate.claimIdlePlayback(idleGeneration) {
		t.Fatal("completed legitimate idle generation could not be claimed for playback")
	}
	if _, accepted := gate.observeAudio(); accepted {
		t.Fatal("audio after the completed idle generation was accepted")
	}
}

func TestHATurnGateVerifiedHATurnStillBlocksSameSessionIdleOutput(t *testing.T) {
	gate := newHATurnGate(newHABridge(&Config{}), "en-US")
	gate.beginSession("test-session")
	turnID := gate.beginTurn()
	observeSealedInput(t, gate, turnID, "turn on the kitchen light")
	mustCloseInput(t, gate, turnID)
	call, response := authorityCallAndResponse("verified-ha-retirement", authoritativeHAText)
	mustAuthorizeHAToolCall(t, gate, call)
	gate.observeToolResult("test-session", call, response)
	gate.observeAudio()
	gate.observeOutputTranscript(authoritativeHAText, true)
	if allowed, reason := gate.finishTurn(turnID); !allowed || reason != "home_assistant_receipt_verified" {
		t.Fatalf("verified HA turn allowed=%v reason=%q", allowed, reason)
	}
	if _, ok := gate.beginIdleOutput(1); ok {
		t.Fatal("verified HA turn did not quarantine the retired session")
	}
}

func TestHATurnGateHostPromptSendFailureRevokesGeneration(t *testing.T) {
	gate := newHATurnGate(newHABridge(&Config{}), "en-US")
	gate.beginSession("test-session")
	const promptID = 73
	generation, ok := gate.beginIdleOutput(promptID)
	if !ok {
		t.Fatal("host prompt start did not open idle generation")
	}
	if got := gate.abandonIdleOutput(promptID + 1); got != 0 {
		t.Fatalf("foreign prompt failure revoked generation %d", got)
	}
	if got := gate.abandonIdleOutput(promptID); got != generation {
		t.Fatalf("matching prompt failure revoked generation %d, want %d", got, generation)
	}
	if _, accepted := gate.observeAudio(); accepted {
		t.Fatal("audio accepted after host prompt send failure")
	}

	// Also cover an immediate provider response that reached the queued state
	// before SendText reported its failure.
	queued, ok := gate.beginIdleOutput(promptID + 2)
	if !ok {
		t.Fatal("second host prompt did not open idle generation")
	}
	gate.observeAudio()
	if got, ready := gate.finishIdleOutput(); !ready || got != queued {
		t.Fatalf("queued generation=%d ready=%v, want %d", got, ready, queued)
	}
	if got := gate.abandonIdlePlayback(promptID + 2); got != queued {
		t.Fatalf("send failure did not revoke queued generation: got %d want %d", got, queued)
	}
	if gate.claimIdlePlayback(queued) {
		t.Fatal("failed host prompt remained claimable for playback")
	}
}

func TestNewTurnRevokesQueuedIdlePlayback(t *testing.T) {
	gate := newHATurnGate(newHABridge(&Config{}), "en-US")
	gate.beginSession("test-session")
	const promptID = 91
	idleGeneration, ok := gate.beginIdleOutput(promptID)
	if !ok {
		t.Fatal("idle prompt did not open generation")
	}
	gate.observeAudio()
	if _, ready := gate.finishIdleOutput(); !ready {
		t.Fatal("idle generation did not become queued for playback")
	}

	// This is the same playMu-serialized revocation sequence used immediately
	// before CaptureAndSendTurn opens the next user generation.
	if revoked := gate.abandonIdlePlayback(0); revoked != idleGeneration {
		t.Fatalf("revoked idle generation = %d, want %d", revoked, idleGeneration)
	}
	turnID := gate.beginTurn()
	if gate.claimIdlePlayback(idleGeneration) {
		t.Fatal("queued idle playback remained claimable after a new turn began")
	}
	observeSealedInput(t, gate, turnID, "turn on the kitchen light")
	if generation, accepted := gate.observeAudio(); !accepted || generation != turnID {
		t.Fatalf("new turn audio generation=%d accepted=%v, want %d", generation, accepted, turnID)
	}
}

func TestHostPromptSendPhaseBlocksTurnStartUntilSent(t *testing.T) {
	runtime := &localVoiceAgentRuntime{
		authority: newHATurnGate(newHABridge(&Config{}), "en-US"),
	}
	runtime.authority.beginSession("test-session")
	const promptID = 123
	if !runtime.onHostPrompt(live.HostPromptEvent{ID: promptID, Kind: live.HostPromptIdleReminder, Type: live.HostPromptStarted}) {
		t.Fatal("host prompt start was rejected")
	}

	turnStarted := make(chan uint64, 1)
	go func() {
		turnStarted <- runtime.beginCaptureTurn()
	}()
	select {
	case <-turnStarted:
		t.Fatal("new turn started inside Started→SendText→Sent barrier")
	case <-time.After(50 * time.Millisecond):
	}

	if !runtime.onHostPrompt(live.HostPromptEvent{ID: promptID, Kind: live.HostPromptIdleReminder, Type: live.HostPromptSent}) {
		t.Fatal("host prompt sent phase was rejected")
	}
	select {
	case turnID := <-turnStarted:
		if turnID == 0 {
			t.Fatal("new turn returned an empty generation")
		}
	case <-time.After(time.Second):
		t.Fatal("new turn did not start after HostPromptSent released the barrier")
	}
}

func TestHATurnGateMissingOutputTranscriptCapabilityFailsClosed(t *testing.T) {
	gate := newHATurnGate(newHABridge(&Config{}), "en-US")
	gate.beginSession("test-session")
	turnID := gate.beginTurn()
	observeSealedInput(t, gate, turnID, "turn on the kitchen light")
	mustCloseInput(t, gate, turnID)
	call, response := authorityCallAndResponse("no-output-transcript", authoritativeHAText)
	mustAuthorizeHAToolCall(t, gate, call)
	gate.observeToolResult("test-session", call, response)
	generation, accepted := gate.observeAudio()
	if !accepted || generation != turnID {
		t.Fatalf("authorized audio generation=%d accepted=%v", generation, accepted)
	}
	if allowed, reason := gate.finishTurn(turnID); allowed || reason != "output_transcript_missing" {
		t.Fatalf("provider without output transcript allowed=%v reason=%q", allowed, reason)
	}
}

func TestTerminalHAAuthorityTextRejectsInvalidReceipts(t *testing.T) {
	for _, result := range []map[string]any{
		{"matched": false, "text": authoritativeHAText},
		{"matched": true, "text": ""},
		{"matched": true, "text": string([]byte{0xff})},
		{"matched": true, "text": string(make([]byte, maxHAAuthorityTextBytes+1))},
	} {
		if _, ok := terminalHAAuthorityText(result); ok {
			t.Fatalf("invalid receipt accepted: %#v", result)
		}
	}
}

func authorityCallAndResponse(id, text string) (agentkit.ToolCall, agentkit.ToolResponse) {
	call := agentkit.ToolCall{
		ID:   id,
		Name: intentHomeAssistant,
		Args: map[string]any{"query": "turn on the kitchen light", "locale": "model-controlled"},
	}
	return call, agentkit.ToolResponse{
		ID:   id,
		Name: intentHomeAssistant,
		Response: map[string]any{
			"matched": true,
			"text":    text,
		},
	}
}

func mustAuthorizeHAToolCall(t *testing.T, gate *haTurnGate, call agentkit.ToolCall) {
	t.Helper()
	args, err := gate.authorizeToolCall("test-session", call)
	if err != nil {
		t.Fatalf("authorize Home Assistant call %q: %v", call.ID, err)
	}
	if args["query"] != "turn on the kitchen light" || args["locale"] != "en-US" {
		t.Fatalf("authorized args = %#v", args)
	}
}

func mustCloseInput(t *testing.T, gate *haTurnGate, turnID uint64) {
	t.Helper()
	if !gate.closeInput(turnID) {
		t.Fatalf("close host input for turn %d", turnID)
	}
}

func observeSealedInput(t *testing.T, gate *haTurnGate, turnID uint64, text string) {
	t.Helper()
	mustSealHostTranscript(t, gate, turnID, text)
	gate.observeInputTranscript(text, true)
}

func mustSealHostTranscript(t *testing.T, gate *haTurnGate, turnID uint64, text string) {
	t.Helper()
	if !gate.sealHostTranscript(turnID, sha256.Sum256([]byte("test-audio:"+text)), text) {
		t.Fatalf("seal host transcript for turn %d", turnID)
	}
}

type fakeAuthoritySTT struct {
	text  string
	err   error
	audio []byte
	opts  stt.TranscribeOpts
}

func (p *fakeAuthoritySTT) Transcribe(_ context.Context, audio []byte, opts stt.TranscribeOpts) (*stt.Result, error) {
	p.audio = append([]byte(nil), audio...)
	p.opts = opts
	if p.err != nil {
		return nil, p.err
	}
	return &stt.Result{Text: p.text, Provider: p.Name()}, nil
}

func (*fakeAuthoritySTT) Name() string { return "fake-authority" }

func (*fakeAuthoritySTT) Health(context.Context) error { return nil }
