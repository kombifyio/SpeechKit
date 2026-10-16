//go:build linux

package voiceagent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/cascaded"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live/openailive"
)

// Delegated native media: GPT-Live (client delegation), Gemini Live (one
// blocking function) and xAI Grok Voice (one function tool) have no custom-LLM
// callback, so the registered agent answers the vendor's own delegation event
// in-process through the same a2a.OpenAIHandler authority (consent
// revalidation, expiry, final-turn binding, replay protection and disclosure)
// that serves the AssemblyAI and Deepgram HTTP callback. The vendor model only
// listens and speaks: its instructions are replaced by nativeDelegationPrompt,
// its only tool is nativeDelegateTool, and the persona or bridge tools never
// reach it.

const (
	// nativeDelegateTool is the single function a Gemini or Grok session can
	// call; GPT-Live raises session.delegation.created instead.
	nativeDelegateTool = "ask_registered_agent"
	// nativeHeardWait bounds how long a delegation waits for the caller's
	// transcript when the vendor's tool call overtakes it.
	nativeHeardWait = 1500 * time.Millisecond
	// nativeHeardQuiet is the pause after the last transcript fragment that
	// ends the caller's utterance when no transcript-done event arrives.
	nativeHeardQuiet = 300 * time.Millisecond
)

const nativeDelegationPrompt = `You are only the voice of a registered agent. You never answer, decide or act on your own, and you use no knowledge or tools of your own.
For every caller turn, %s and wait for the result.
Speak the result to the caller faithfully and completely in the caller's language, without adding, omitting or changing any fact, number, name or commitment.
If the result reports an error, say only the sentence given in the result.
If the result is exactly "` + nativeInterruptedResult + `", the caller has interrupted: say nothing and wait for the caller.`

// nativeInterruptedResult settles a delegation whose agent turn a barge-in
// canceled; the prompt tells the vendor to ignore it without speaking.
const nativeInterruptedResult = "[interrupted]"

// delegatedNativeProvider reports whether the media provider is answered
// in-process instead of through the vendor custom-LLM callback.
func delegatedNativeProvider(provider string) bool {
	switch provider {
	case "gpt-live", "gemini", "xai":
		return true
	default:
		return false
	}
}

// applyNativeDelegation replaces the vendor's instructions and tools so the
// registered agent stays the only reasoner.
func applyNativeDelegation(cfg *LiveConfigFrame, provider string) {
	cfg.RefinementPrompt = ""
	cfg.Tools = nil
	if provider == "gpt-live" {
		cfg.NativeClientDelegation = true
		cfg.SystemPrompt = fmt.Sprintf(nativeDelegationPrompt, "delegate the caller's request to the client application")
		return
	}
	cfg.SystemPrompt = fmt.Sprintf(nativeDelegationPrompt, "call the function "+nativeDelegateTool+" exactly once")
	cfg.Tools = []ToolDefinitionFrame{{
		Name:        nativeDelegateTool,
		Description: "Hands the caller's latest request to the registered agent and returns the answer to speak.",
		Behavior:    "blocking",
	}}
}

// delegateTurns records the caller's transcript and starts one agent turn per
// vendor delegation. It strips the vendor's tool calls so they never reach
// the client, and reports whether the message carried only tool calls or
// gated vendor speech, so nothing is left to forward.
func (p *registeredNativeProvider) delegateTurns(message *LiveMessage) bool {
	consumed := len(message.ToolCalls) > 0 || len(message.Audio) > 0 || message.OutputTranscript != "" || message.OutputTranscriptDone
	p.heardMu.Lock()
	if message.Interrupted {
		// Barge-in starts a new caller turn: leftovers must not prefix it.
		p.heard, p.heardDone, p.heardTaken = "", false, false
	}
	if p.heardTaken && (len(message.Audio) > 0 || message.OutputTranscript != "") {
		// The vendor answers the delegation: fragments heard since it took
		// the utterance belong to no turn (GPT-Live never reports Done).
		p.heard, p.heardDone, p.heardTaken = "", false, false
	}
	if message.InputTranscript != "" {
		if full, ok := message.ProviderMetadata["transcript"].(string); ok {
			p.heard = full // Cumulative transcript that may correct earlier words.
		} else {
			p.heard += message.InputTranscript
		}
		p.heardAt = time.Now()
	}
	if message.InputTranscriptDone {
		p.heardDone = true
	}
	if message.Done && p.heardTaken {
		// The vendor turn that answered a delegation ended; fragments that
		// arrived after the delegation took the utterance belong to no turn.
		p.heard, p.heardDone, p.heardTaken = "", false, false
	}
	p.gateSpeech(message)
	p.heardMu.Unlock()
	for _, call := range message.ToolCalls {
		if call.Name == nativeDelegateTool || call.Name == openailive.ClientDelegationTool {
			go p.answerDelegation(call)
		}
	}
	message.ToolCalls = nil
	return consumed && len(message.Audio) == 0 && message.OutputTranscript == "" && !message.OutputTranscriptDone &&
		message.InputTranscript == "" && !message.InputTranscriptDone && !message.Done && !message.Interrupted &&
		!message.GoAway && !message.SessionResumable && message.ErrorCode == "" && message.ErrorMessage == ""
}

// gateSpeech drops vendor speech (audio and assistant transcript) outside the
// window that opens when a delegation result is handed to the vendor and
// closes when that response ends: the vendor must not answer from its own
// knowledge. Gemini and Grok end the window at their turn end (or barge-in);
// GPT-Live has no turn end, so its window closes, best effort, when the
// caller speaks again after the answer started. Caller with heardMu held.
func (p *registeredNativeProvider) gateSpeech(message *LiveMessage) {
	if !p.speakOpen {
		message.Audio, message.OutputTranscript, message.OutputTranscriptDone = nil, "", false
		return
	}
	if len(message.Audio) > 0 || message.OutputTranscript != "" {
		p.speakSeen = true
	}
	callerAgain := p.provider == "gpt-live" && message.InputTranscript != "" && p.speakSeen
	if message.Interrupted || callerAgain || (message.Done && p.speakSeen) {
		p.speakOpen, p.speakSeen = false, false
	}
}

// openSpeech admits the vendor's response to a delegation result.
func (p *registeredNativeProvider) openSpeech() {
	p.heardMu.Lock()
	p.speakOpen, p.speakSeen = true, false
	p.heardMu.Unlock()
}

// answerDelegation runs the registered agent on the caller's transcript and
// returns its answer, or a safe failure code, to the vendor.
func (p *registeredNativeProvider) answerDelegation(call ToolCall) {
	p.mu.Lock()
	callback, turns, ctx, closed, locale := p.callback, p.turns, p.ctx, p.closed, p.binding.Locale
	p.mu.Unlock()
	responder, ok := p.LiveProviderAdapter.(LiveToolResponder)
	if closed || callback == nil || turns == nil || ctx == nil || !ok {
		return
	}
	code := "caller_speech_not_captured"
	if utterance := p.takeHeard(ctx); utterance != "" {
		turns.Observe(utterance, call.ID)
		answer, err := callback.Answer(ctx, utterance)
		switch {
		case ctx.Err() != nil:
			return // Teardown: the vendor session is closing.
		case errors.Is(err, context.Canceled):
			// Barge-in canceled the agent turn: settle the pending delegation
			// without prompting vendor speech over the caller. The speech gate
			// stays closed; Gemini has no silent result for blocking calls and
			// relies on the prompt to ignore it.
			frame := ToolResponseFrame{ID: call.ID, Name: call.Name, Response: map[string]any{"result": nativeInterruptedResult}}
			if p.provider != "gemini" {
				frame.Scheduling = string(live.ToolResponseSchedulingSilent)
			}
			_ = responder.SendToolResponse(frame)
			return
		case err != nil:
			code = "agent_turn_failed"
			var coded cascaded.CodedError
			if errors.As(err, &coded) {
				code = cascaded.SafeFailureCode(coded.Code())
			}
		case strings.TrimSpace(answer) == "":
			code = "agent_turn_failed"
		default:
			p.openSpeech()
			_ = responder.SendToolResponse(ToolResponseFrame{ID: call.ID, Name: call.Name, Response: map[string]any{"result": answer}})
			return
		}
	}
	// A failed or empty turn never lets the vendor improvise: the result is
	// a fixed host line, and the follow-up response may only say it.
	line := nativeFailureLine(locale, code)
	p.openSpeech()
	_ = responder.SendToolResponse(ToolResponseFrame{ID: call.ID, Name: call.Name,
		Response:             map[string]any{"error": code, "result": line},
		FollowUpInstructions: "Say exactly this sentence and nothing else: " + line})
}

// nativeFailureLine is the host-authored sentence spoken for a delegation
// that produced no agent answer.
func nativeFailureLine(locale, code string) string {
	german := strings.HasPrefix(strings.ToLower(locale), "de")
	switch {
	case code == "caller_speech_not_captured" && german:
		return "Entschuldigung, das habe ich nicht verstanden. Bitte sag es noch einmal."
	case code == "caller_speech_not_captured":
		return "Sorry, I didn't catch that. Please say it again."
	case german:
		return "Entschuldigung, das konnte ich gerade nicht erledigen. Bitte versuch es noch einmal."
	default:
		return "Sorry, I couldn't complete that request. Please try again."
	}
}

// takeHeard returns and clears the caller's transcript since the last
// delegated turn. When the vendor's delegation arrives before or during
// transcription it waits for a transcript-done event or a quiet pause after
// the last fragment, bounded by nativeHeardWait.
func (p *registeredNativeProvider) takeHeard(ctx context.Context) string {
	deadline := time.Now().Add(nativeHeardWait)
	for {
		p.heardMu.Lock()
		heard := strings.TrimSpace(p.heard)
		now := time.Now()
		settled := heard != "" && (p.heardDone || now.Sub(p.heardAt) >= nativeHeardQuiet)
		if settled || !now.Before(deadline) || ctx.Err() != nil {
			p.heard, p.heardDone = "", false
			p.heardTaken = heard != ""
			p.heardMu.Unlock()
			return heard
		}
		p.heardMu.Unlock()
		select {
		case <-ctx.Done():
		case <-time.After(25 * time.Millisecond):
		}
	}
}
