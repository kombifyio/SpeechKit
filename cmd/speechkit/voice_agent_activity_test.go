package main

import (
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/internal/voiceagent"
)

func TestVoiceAgentUserActivityLevelStaysVisibleDuringActiveSession(t *testing.T) {
	for _, state := range []voiceagent.State{
		voiceagent.StateConnecting,
		voiceagent.StateListening,
		voiceagent.StateProcessing,
	} {
		t.Run(string(state), func(t *testing.T) {
			if got := voiceAgentUserActivityLevel(state, 0.42, nil); got != 0.42 {
				t.Fatalf("activity level for %s = %.2f, want 0.42", state, got)
			}
		})
	}
}

func TestVoiceAgentUserActivityLevelHidesWhenInactiveOrDeactivating(t *testing.T) {
	for _, state := range []voiceagent.State{
		voiceagent.StateInactive,
		voiceagent.StateDeactivating,
	} {
		t.Run(string(state), func(t *testing.T) {
			if got := voiceAgentUserActivityLevel(state, 0.42, nil); got != 0 {
				t.Fatalf("activity level for %s = %.2f, want 0", state, got)
			}
		})
	}
}

func TestVoiceAgentUserActivityLevelHidesWhileAgentIsSpeaking(t *testing.T) {
	if got := voiceAgentUserActivityLevel(voiceagent.StateSpeaking, 0.42, nil); got != 0 {
		t.Fatalf("activity level while speaking = %.2f, want 0", got)
	}
}

func TestVoiceAgentEchoGuardSuppressesMicDuringAssistantAudioTail(t *testing.T) {
	now := time.Unix(100, 0)
	guard := newVoiceAgentEchoGuard(400 * time.Millisecond)
	guard.now = func() time.Time { return now }

	if !voiceAgentMicFrameAllowed(voiceagent.StateListening, guard) {
		t.Fatal("expected mic frame before assistant playback to be allowed")
	}

	guard.markAssistantAudio()
	if voiceAgentMicFrameAllowed(voiceagent.StateListening, guard) {
		t.Fatal("expected mic frame during assistant echo tail to be suppressed")
	}
	if got := voiceAgentUserActivityLevel(voiceagent.StateListening, 0.42, guard); got != 0 {
		t.Fatalf("activity level during echo tail = %.2f, want 0", got)
	}

	now = now.Add(401 * time.Millisecond)
	if !voiceAgentMicFrameAllowed(voiceagent.StateListening, guard) {
		t.Fatal("expected mic frame after assistant echo tail to be allowed")
	}
	if got := voiceAgentUserActivityLevel(voiceagent.StateListening, 0.42, guard); got != 0.42 {
		t.Fatalf("activity level after echo tail = %.2f, want 0.42", got)
	}
}

func TestVoiceAgentEchoGuardDefaultTailAllowsPromptFollowUp(t *testing.T) {
	now := time.Unix(100, 0)
	guard := newVoiceAgentEchoGuard(0)
	guard.now = func() time.Time { return now }

	guard.markAssistantAudio()
	now = now.Add(220 * time.Millisecond)

	if !voiceAgentMicFrameAllowed(voiceagent.StateListening, guard) {
		t.Fatal("expected default echo tail to allow a prompt follow-up after assistant audio")
	}
}
