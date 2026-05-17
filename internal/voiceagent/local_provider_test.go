package voiceagent

import (
	"context"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/ai/flows"
	"github.com/kombifyio/SpeechKit/internal/stt"
	"github.com/kombifyio/SpeechKit/internal/tts"
	live "github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
)

type stubSTT struct{}

func (stubSTT) Route(_ context.Context, _ []byte, _ float64, _ stt.TranscribeOpts) (*stt.Result, error) {
	return &stt.Result{Text: "hello"}, nil
}

type stubAgent struct{}

func (stubAgent) Run(_ context.Context, _ flows.AgentInput) (flows.AgentOutput, error) {
	return flows.AgentOutput{Text: "world"}, nil
}

type stubTTS struct{}

func (stubTTS) Synthesize(_ context.Context, _ string, _ tts.SynthesizeOpts) (*tts.Result, error) {
	return &tts.Result{Audio: []byte{0, 0}}, nil
}

func TestNewLocalVoiceAgentProviderProducesProvider(t *testing.T) {
	p := NewLocalVoiceAgentProvider(LocalVoiceAgentDeps{
		STT:   stubSTT{},
		Agent: stubAgent{},
		TTS:   stubTTS{},
	})
	if p == nil {
		t.Fatal("NewLocalVoiceAgentProvider returned nil")
	}
	var _ live.LiveProvider = p
	var _ live.LiveInstructionUpdater = p
}

func TestLocalVoiceAgentProviderNameIsLocalCascaded(t *testing.T) {
	p := NewLocalVoiceAgentProvider(LocalVoiceAgentDeps{
		STT:   stubSTT{},
		Agent: stubAgent{},
		TTS:   stubTTS{},
	})
	if got, want := p.Name(), "local-cascaded"; got != want {
		t.Fatalf("Name() = %q, want %q", got, want)
	}
}

func TestNewLocalVoiceAgentProviderRejectsNilSTT(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on nil STT")
		}
	}()
	_ = NewLocalVoiceAgentProvider(LocalVoiceAgentDeps{Agent: stubAgent{}})
}

func TestNewLocalVoiceAgentProviderRejectsNilAgent(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on nil Agent")
		}
	}()
	_ = NewLocalVoiceAgentProvider(LocalVoiceAgentDeps{STT: stubSTT{}})
}

func TestLocalVoiceAgentProviderConnectAcceptsLiveConfig(t *testing.T) {
	p := NewLocalVoiceAgentProvider(LocalVoiceAgentDeps{STT: stubSTT{}, Agent: stubAgent{}})
	t.Cleanup(func() { _ = p.Close() })

	cfg := live.LiveConfig{
		Locale:           "de",
		Voice:            "alloy",
		FrameworkPrompt:  "framework",
		RefinementPrompt: "refine",
	}
	if err := p.Connect(context.Background(), cfg); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := p.UpdateInstructions(context.Background(), cfg); err != nil {
		t.Fatalf("UpdateInstructions: %v", err)
	}
}

func TestPickSystemPromptPrefersFrameworkPrompt(t *testing.T) {
	cases := []struct {
		name string
		cfg  live.LiveConfig
		want string
	}{
		{"framework wins", live.LiveConfig{FrameworkPrompt: "fw", SystemPrompt: "sys", Instruction: "ins"}, "fw"},
		{"system fallback", live.LiveConfig{SystemPrompt: "sys", Instruction: "ins"}, "sys"},
		{"instruction last", live.LiveConfig{Instruction: "ins"}, "ins"},
		{"all empty", live.LiveConfig{}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := pickSystemPrompt(c.cfg); got != c.want {
				t.Fatalf("pickSystemPrompt(%+v) = %q, want %q", c.cfg, got, c.want)
			}
		})
	}
}
