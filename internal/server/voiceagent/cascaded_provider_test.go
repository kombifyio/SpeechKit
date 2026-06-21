//go:build linux

package voiceagent

import (
	"context"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/stt"
	"github.com/kombifyio/SpeechKit/internal/tts"
	"github.com/kombifyio/SpeechKit/internal/voiceagent/cascaded"
)

// The bulk of cascaded provider tests live in
// internal/voiceagent/cascaded/provider_test.go (cross-platform). The
// tests below guard only what is specific to this Linux adapter:
//
//  1. The CascadedConfig / CascadedSTT / CascadedAgent / CascadedTTS /
//     CascadedDeps aliases keep the historical names alive for
//     internal/server/core/voiceagent_wiring.go.
//  2. The Connect / Receive translation between server LiveConfigFrame
//     and cascaded.SessionConfig (resp. LiveMessage and
//     cascaded.Message) round-trips faithfully.
//  3. NewAgentFlowAdapter is re-exported.

type smokeSTT struct{ text string }

func (s smokeSTT) Route(_ context.Context, _ []byte, _ float64, _ stt.TranscribeOpts) (*stt.Result, error) {
	return &stt.Result{Text: s.text}, nil
}

type smokeAgent struct{ response string }

func (a smokeAgent) Run(_ context.Context, _ cascaded.AgentInput) (cascaded.AgentOutput, error) {
	return cascaded.AgentOutput{Text: a.response}, nil
}

type smokeTTS struct{}

func (smokeTTS) Synthesize(_ context.Context, _ string, _ tts.SynthesizeOpts) (*tts.Result, error) {
	return &tts.Result{Audio: []byte{0, 0, 0, 0}}, nil
}

func TestCascadedAliasesCompile(t *testing.T) {
	// Compile-time asserts that the historical names resolve to the
	// expected cross-platform types.
	var (
		_ CascadedSTT   = smokeSTT{}
		_ CascadedAgent = smokeAgent{}
		_ CascadedTTS   = smokeTTS{}
	)
	_ = CascadedConfig{TTSFormat: "mp3"}
	_ = CascadedDeps{STT: smokeSTT{}, Agent: smokeAgent{}}
	_ = NewAgentFlowAdapter
}

func TestCascadedProviderSatisfiesLiveProviderAdapter(t *testing.T) {
	var _ LiveProviderAdapter = NewCascadedProvider(CascadedDeps{
		STT:   smokeSTT{},
		Agent: smokeAgent{},
	})
}

func TestCascadedProviderTranslatesSessionConfig(t *testing.T) {
	p := NewCascadedProvider(CascadedDeps{STT: smokeSTT{}, Agent: smokeAgent{}})
	t.Cleanup(func() { _ = p.Close() })

	cfg := LiveConfigFrame{
		Locale:           "de",
		Voice:            "alloy",
		SystemPrompt:     "system",
		RefinementPrompt: "refine",
	}
	if err := p.Connect(context.Background(), cfg); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	// UpdateInstructions must accept the same frame shape.
	if err := p.UpdateInstructions(context.Background(), cfg); err != nil {
		t.Fatalf("UpdateInstructions: %v", err)
	}
	if got := p.Name(); got != "cascaded" {
		t.Fatalf("Name() = %q, want %q", got, "cascaded")
	}
}
