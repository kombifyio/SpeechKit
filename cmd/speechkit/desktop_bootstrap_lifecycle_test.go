package main

import (
	"testing"

	appai "github.com/kombifyio/SpeechKit/internal/ai"
	"github.com/kombifyio/SpeechKit/internal/config"
)

// TestDesktopRuntimeAINeeded exercises the cold-start gating predicate
// for the Genkit AI runtime. The contract: Genkit is needed only when
// Assist or Voice-Agent is enabled — Dictation-only hosts skip it.
func TestDesktopRuntimeAINeeded(t *testing.T) {
	cases := []struct {
		name string
		cfg  *config.Config
		want bool
	}{
		{name: "nil cfg", cfg: nil, want: false},
		{name: "all off", cfg: &config.Config{}, want: false},
		{
			name: "dictation only",
			cfg: &config.Config{General: config.GeneralConfig{
				DictateEnabled: true,
			}},
			want: false,
		},
		{
			name: "assist on",
			cfg: &config.Config{General: config.GeneralConfig{
				AssistEnabled: true,
			}},
			want: true,
		},
		{
			name: "voiceagent on",
			cfg: &config.Config{General: config.GeneralConfig{
				VoiceAgentEnabled: true,
			}},
			want: true,
		},
		{
			name: "dictation + voiceagent",
			cfg: &config.Config{General: config.GeneralConfig{
				DictateEnabled:    true,
				VoiceAgentEnabled: true,
			}},
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := desktopRuntimeAINeeded(tc.cfg); got != tc.want {
				t.Errorf("desktopRuntimeAINeeded() = %v; want %v", got, tc.want)
			}
		})
	}
}

// TestDesktopRuntimeTTSNeeded mirrors the AI predicate today but is
// kept separate so future modes can shift the boundary without
// touching the AI semantics.
func TestDesktopRuntimeTTSNeeded(t *testing.T) {
	cases := []struct {
		name string
		cfg  *config.Config
		want bool
	}{
		{name: "nil cfg", cfg: nil, want: false},
		{name: "all off", cfg: &config.Config{}, want: false},
		{
			name: "dictation only — TTS NOT needed",
			cfg: &config.Config{General: config.GeneralConfig{
				DictateEnabled: true,
			}},
			want: false,
		},
		{
			name: "assist on",
			cfg: &config.Config{General: config.GeneralConfig{
				AssistEnabled: true,
			}},
			want: true,
		},
		{
			name: "voiceagent on",
			cfg: &config.Config{General: config.GeneralConfig{
				VoiceAgentEnabled: true,
			}},
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := desktopRuntimeTTSNeeded(tc.cfg); got != tc.want {
				t.Errorf("desktopRuntimeTTSNeeded() = %v; want %v", got, tc.want)
			}
		})
	}
}

// TestEnsureSharedDepsForCfgNoopWhenAlreadyPopulated verifies the
// idempotency contract of ensureSharedDepsForCfg: when state already
// has the runtime populated, a second call MUST NOT re-initialise.
// We can't easily mock appai.Init in this layer, but we CAN observe
// the no-op path by stashing a sentinel *appai.Runtime in state.
// If the gating is broken, initDesktopAIRuntime would replace it
// (or fail and leave it intact — both detectable by pointer compare).
func TestEnsureSharedDepsForCfgNoopWhenAlreadyPopulated(t *testing.T) {
	state := &appState{}

	// Empty-but-non-nil sentinel. The real Runtime is initialised
	// via appai.Init; we never call that here, so this sentinel
	// never sees real Genkit traffic.
	sentinel := &appai.Runtime{}
	state.mu.Lock()
	state.genkitRT = sentinel
	state.mu.Unlock()

	cfg := &config.Config{General: config.GeneralConfig{
		AssistEnabled: true,
	}}

	ensureSharedDepsForCfg(t.Context(), cfg, state, nil)

	state.mu.Lock()
	after := state.genkitRT
	state.mu.Unlock()
	if after != sentinel {
		t.Errorf("ensureSharedDepsForCfg overwrote already-populated genkitRT — expected idempotent no-op (sentinel=%p after=%p)", sentinel, after)
	}
}

func TestEnsureSharedDepsForCfgSkipsWhenNotNeeded(t *testing.T) {
	state := &appState{}
	cfg := &config.Config{General: config.GeneralConfig{
		DictateEnabled: true,
		// Assist + VoiceAgent both off — no shared deps required.
	}}

	ensureSharedDepsForCfg(t.Context(), cfg, state, nil)

	state.mu.Lock()
	defer state.mu.Unlock()
	if state.genkitRT != nil {
		t.Error("genkitRT was populated for Dictation-only cfg; want nil")
	}
	if state.ttsRouter != nil {
		t.Error("ttsRouter was populated for Dictation-only cfg; want nil")
	}
}
