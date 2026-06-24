package voiceagent

import (
	"context"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/internal/stt"
	"github.com/kombifyio/SpeechKit/internal/tts"
	"github.com/kombifyio/SpeechKit/internal/voiceagent/cascaded"
	live "github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
)

type stubSTT struct{}

func (stubSTT) Route(_ context.Context, _ []byte, _ float64, _ stt.TranscribeOpts) (*stt.Result, error) {
	return &stt.Result{Text: "hello"}, nil
}

type stubAgent struct{}

func (stubAgent) Run(_ context.Context, _ cascaded.AgentInput) (cascaded.AgentOutput, error) {
	return cascaded.AgentOutput{Text: "world"}, nil
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

func TestLocalVoiceAgentProviderReceiveAddsEventAttribution(t *testing.T) {
	p := NewLocalVoiceAgentProvider(LocalVoiceAgentDeps{STT: stubSTT{}, Agent: stubAgent{}, TTS: stubTTS{}})
	t.Cleanup(func() { _ = p.Close() })
	if err := p.Connect(context.Background(), live.LiveConfig{Locale: "en"}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := p.SendText("hello"); err != nil {
		t.Fatalf("SendText: %v", err)
	}

	textMsg := receiveUntil(t, p, func(msg *live.LiveMessage) bool {
		return msg.OutputTranscript == "world"
	})
	if textMsg.EventType != live.LiveEventOutputText ||
		!liveEventTypesContain(textMsg.EventTypes, live.LiveEventOutputText) ||
		textMsg.ProviderMetadata["provider_event"] != "cascaded.message" {
		t.Fatalf("text event metadata = %+v", textMsg)
	}

	audioMsg := receiveUntil(t, p, func(msg *live.LiveMessage) bool {
		return len(msg.Audio) > 0
	})
	if audioMsg.EventType != live.LiveEventOutputAudio ||
		!liveEventTypesContain(audioMsg.EventTypes, live.LiveEventOutputAudio) ||
		audioMsg.ProviderMetadata["provider_event"] != "cascaded.message" {
		t.Fatalf("audio event metadata = %+v", audioMsg)
	}
}

func receiveUntil(t *testing.T, p *LocalVoiceAgentProvider, match func(*live.LiveMessage) bool) *live.LiveMessage {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for {
		msg, err := p.Receive(ctx)
		if err != nil {
			t.Fatalf("Receive: %v", err)
		}
		if match(msg) {
			return msg
		}
	}
}

func liveEventTypesContain(values []live.LiveEventType, want live.LiveEventType) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
