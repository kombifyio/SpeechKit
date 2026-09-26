package cascaded

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live/livecontract"
)

func newTestLiveProvider(t *testing.T, deps Deps) *LiveProvider {
	t.Helper()
	p := NewLiveProvider(deps)
	if p == nil {
		t.Fatal("NewLiveProvider returned nil")
	}
	t.Cleanup(func() { _ = p.Close() })
	return p
}

func liveDeps() Deps {
	return Deps{
		STT:   &fakeSTT{text: "hello"},
		Agent: &fakeAgent{response: "world"},
		TTS:   &fakeTTS{audio: []byte{0, 0}},
	}
}

func TestLiveProviderSatisfiesLiveContracts(t *testing.T) {
	p := newTestLiveProvider(t, liveDeps())
	var _ live.LiveProvider = p
	var _ live.LiveInstructionUpdater = p
}

func TestLiveProviderNameIsLocalCascaded(t *testing.T) {
	p := newTestLiveProvider(t, liveDeps())
	if got, want := p.Name(), "local-cascaded"; got != want {
		t.Fatalf("Name() = %q, want %q", got, want)
	}
}

func TestLiveProviderConnectRequiresSTTAndAgent(t *testing.T) {
	cases := map[string]Deps{
		"missing STT":   {Agent: &fakeAgent{response: "world"}},
		"missing Agent": {STT: &fakeSTT{text: "hello"}},
	}
	for name, deps := range cases {
		t.Run(name, func(t *testing.T) {
			p := newTestLiveProvider(t, deps)
			err := p.Connect(context.Background(), live.LiveConfig{Locale: "en"})
			if !errors.Is(err, ErrNotConfigured) {
				t.Fatalf("Connect error = %v, want ErrNotConfigured", err)
			}
		})
	}
}

func TestLiveProviderConnectAcceptsLiveConfig(t *testing.T) {
	p := newTestLiveProvider(t, Deps{STT: &fakeSTT{text: "hello"}, Agent: &fakeAgent{response: "world"}})

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
	locale, voice, systemPrompt := p.inner.currentInstructionSnapshot()
	if locale != "de" || voice != "alloy" {
		t.Fatalf("locale/voice = %q/%q, want de/alloy", locale, voice)
	}
	if systemPrompt == "" {
		t.Fatal("system prompt was not mapped from FrameworkPrompt")
	}
}

func TestLiveProviderReceiveAddsEventAttribution(t *testing.T) {
	p := newTestLiveProvider(t, liveDeps())
	if err := p.Connect(context.Background(), live.LiveConfig{Locale: "en"}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := p.SendText("hello"); err != nil {
		t.Fatalf("SendText: %v", err)
	}

	textMsg := receiveLiveUntil(t, p, func(msg *live.LiveMessage) bool {
		return msg.OutputTranscript == "world"
	})
	if textMsg.EventType != live.LiveEventOutputText ||
		!liveEventTypesContain(textMsg.EventTypes, live.LiveEventOutputText) ||
		textMsg.ProviderMetadata["provider_event"] != "cascaded.message" {
		t.Fatalf("text event metadata = %+v", textMsg)
	}

	audioMsg := receiveLiveUntil(t, p, func(msg *live.LiveMessage) bool {
		return len(msg.Audio) > 0
	})
	if audioMsg.EventType != live.LiveEventOutputAudio ||
		!liveEventTypesContain(audioMsg.EventTypes, live.LiveEventOutputAudio) ||
		audioMsg.ProviderMetadata["provider_event"] != "cascaded.message" {
		t.Fatalf("audio event metadata = %+v", audioMsg)
	}
}

func TestLiveProviderSendToolResponseIsNoop(t *testing.T) {
	p := newTestLiveProvider(t, liveDeps())
	if err := p.SendToolResponse(live.ToolResponse{ID: "tool-1", Name: "noop"}); err != nil {
		t.Fatalf("SendToolResponse: %v", err)
	}
}

func TestLiveProviderPassesLiveContract(t *testing.T) {
	newProvider := func() live.LiveProvider { return NewLiveProvider(liveDeps()) }
	livecontract.Run(t, livecontract.Case{
		NewProvider:   newProvider,
		Config:        live.LiveConfig{Locale: "en"},
		WantName:      "local-cascaded",
		WantEventType: live.LiveEventInputFinal,
	})
	livecontract.RunReceiveCancellation(t, livecontract.Case{
		NewProvider: newProvider,
		Config:      live.LiveConfig{Locale: "en"},
	})
}

func receiveLiveUntil(t *testing.T, p *LiveProvider, match func(*live.LiveMessage) bool) *live.LiveMessage {
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
