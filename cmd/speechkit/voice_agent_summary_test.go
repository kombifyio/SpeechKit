package main

import (
	"context"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/textactions"
)

func TestVoiceAgentSessionSummaryUsesFinalDialogTurns(t *testing.T) {
	prompter := &fakeOverlayWindow{}
	state := &appState{
		prompterWindow: prompter,
		voiceAgentSummaryTool: textactions.SummaryTool{
			Summarizer: textactions.SummarizerFunc(func(_ context.Context, input textactions.Input) (string, error) {
				if !strings.Contains(input.Text, "User: We need a V23 plan") {
					t.Fatalf("summary input missing user turn: %q", input.Text)
				}
				if !strings.Contains(input.Text, "Assistant: Start with mode contracts") {
					t.Fatalf("summary input missing assistant turn: %q", input.Text)
				}
				if got, want := input.Locale, "en"; got != want {
					t.Fatalf("summary locale = %q, want %q", got, want)
				}
				return "Decisions: standardize modes. Next: implement providers.", nil
			}),
		},
	}

	state.resetVoiceAgentSessionSummary()
	state.recordVoiceAgentDialogTurn("user", "We need a V23 plan", true)
	state.recordVoiceAgentDialogTurn("assistant", "Start with mode contracts", true)

	summary := state.finishVoiceAgentSessionSummary(context.Background(), &config.Config{
		General: config.GeneralConfig{Language: "en"},
		VoiceAgent: config.VoiceAgentConfig{
			EnableSessionSummary: true,
		},
	})

	if !strings.Contains(summary, "Decisions: standardize modes") {
		t.Fatalf("summary = %q", summary)
	}
	combinedScripts := strings.Join(prompter.scripts, "\n")
	if !strings.Contains(combinedScripts, "Session summary") {
		t.Fatalf("prompter scripts missing session summary: %s", combinedScripts)
	}
	if !strings.Contains(combinedScripts, "implement providers") {
		t.Fatalf("prompter scripts missing generated summary: %s", combinedScripts)
	}
}

func TestVoiceAgentSessionSummaryIgnoresPartialTranscriptSegments(t *testing.T) {
	state := &appState{}
	state.resetVoiceAgentSessionSummary()
	state.recordVoiceAgentDialogTurn("user", "partial", false)
	state.recordVoiceAgentDialogTurn("assistant", "final", true)

	text := state.voiceAgentSessionTranscript()
	if strings.Contains(text, "partial") {
		t.Fatalf("transcript should ignore partial segments: %q", text)
	}
	if !strings.Contains(text, "Assistant: final") {
		t.Fatalf("transcript missing final segment: %q", text)
	}
}
