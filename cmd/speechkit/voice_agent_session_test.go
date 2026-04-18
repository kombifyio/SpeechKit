package main

import (
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/config"
)

func TestBuildVoiceAgentCallbacks_OnTextUsesPrompterWhenOutputTranscriptDisabled(t *testing.T) {
	prompter := &fakeOverlayWindow{}
	bubble := &fakeOverlayWindow{}
	state := &appState{
		prompterWindow: prompter,
		assistBubble:   bubble,
	}

	callbacks := buildVoiceAgentCallbacks(state, &config.Config{
		VoiceAgent: config.VoiceAgentConfig{
			EnableOutputTranscript: false,
		},
	})

	callbacks.OnText("Quick spoken answer")

	combinedScripts := strings.Join(prompter.scripts, "\n")
	if !strings.Contains(combinedScripts, `role:"assistant",text:"Quick spoken answer",done:false`) {
		t.Fatalf("prompter scripts = %s, want assistant live text message", combinedScripts)
	}
	if bubble.showCalls != 0 {
		t.Fatalf("assist bubble show calls = %d, want 0", bubble.showCalls)
	}
	if len(bubble.scripts) != 0 {
		t.Fatalf("assist bubble scripts = %v, want none", bubble.scripts)
	}
}

func TestBuildVoiceAgentCallbacks_OnTextDoesNotTouchAssistBubbleWhenTranscriptEnabled(t *testing.T) {
	prompter := &fakeOverlayWindow{}
	bubble := &fakeOverlayWindow{}
	state := &appState{
		prompterWindow: prompter,
		assistBubble:   bubble,
	}

	callbacks := buildVoiceAgentCallbacks(state, &config.Config{
		VoiceAgent: config.VoiceAgentConfig{
			EnableOutputTranscript: true,
		},
	})

	callbacks.OnText("This should stay off the assist bubble")

	if len(prompter.scripts) != 0 {
		t.Fatalf("prompter scripts = %v, want none when output transcript is enabled", prompter.scripts)
	}
	if bubble.showCalls != 0 {
		t.Fatalf("assist bubble show calls = %d, want 0", bubble.showCalls)
	}
	if len(bubble.scripts) != 0 {
		t.Fatalf("assist bubble scripts = %v, want none", bubble.scripts)
	}
}
