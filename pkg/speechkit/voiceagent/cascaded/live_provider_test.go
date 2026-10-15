package cascaded

import (
	"context"
	"encoding/binary"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/audio"
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
		TTS:   &fakeTTS{audio: audio.PCMToWAV([]byte{0, 0}), format: "wav"},
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

// A synthesized file must never be played as raw samples. The live contract
// preserves the speech duration and amplitude at 24 kHz, regardless of WAV input.
func TestLiveProviderConvertsSynthesizedSpeechForPlayback(t *testing.T) {
	pcm := make([]byte, audio.SampleRate*audio.BytesPerSample)
	for i := 0; i < len(pcm); i += audio.BytesPerSample {
		binary.LittleEndian.PutUint16(pcm[i:], 1000)
	}
	p := newTestLiveProvider(t, Deps{
		STT: &fakeSTT{text: "hello"}, Agent: &fakeAgent{response: "world"},
		TTS: &fakeTTS{audio: audio.PCMToWAV(pcm), format: "wav"},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := p.Connect(ctx, live.LiveConfig{Locale: "en"}); err != nil {
		t.Fatal(err)
	}
	if err := p.SendText("hello"); err != nil {
		t.Fatal(err)
	}
	var playback []byte
	for {
		msg, err := p.Receive(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if msg.TurnError != nil {
			t.Fatal(msg.TurnError)
		}
		playback = append(playback, msg.Audio...)
		if msg.Done {
			break
		}
	}
	duration := time.Duration(len(playback)) * time.Second / (OutputSampleRate * audio.BytesPerSample)
	if duration != time.Second {
		t.Fatalf("playback duration = %v, want the original second of speech", duration)
	}
	for i := 0; i < len(playback); i += audio.BytesPerSample {
		if sample := binary.LittleEndian.Uint16(playback[i:]); sample != 1000 {
			t.Fatalf("speech amplitude changed: sample %d = %d", i/audio.BytesPerSample, sample)
		}
	}
}

// A failed local turn used to disappear at the adapter and strand the session.
// The public callback must receive safe guidance and the next turn must work.
func TestLiveProviderTurnFailureLeavesSessionUsable(t *testing.T) {
	agent := &fakeAgent{response: "world", err: errors.New("private upstream diagnostic")}
	p := newTestLiveProvider(t, Deps{STT: &fakeSTT{text: "hello"}, Agent: agent})
	errorsSeen := make(chan error, 1)
	output := make(chan string, 1)
	session := live.NewSession(p, live.Callbacks{
		OnError:     func(err error) { t.Errorf("recoverable failure ended the session: %v", err) },
		OnTurnError: func(err error) { errorsSeen <- err },
		OnOutputTranscript: func(text string, done bool) {
			if done {
				output <- text
			}
		},
	})
	t.Cleanup(session.Stop)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := session.Start(ctx, live.LiveConfig{Locale: "en"}, live.IdleConfig{}); err != nil {
		t.Fatal(err)
	}
	if err := session.SendText("first turn"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errorsSeen:
		var coded CodedError
		if !errors.As(err, &coded) || coded.Code() != "turn_failed" || strings.Contains(err.Error(), "private upstream") {
			t.Fatalf("unsafe or unclassified turn error: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("failed turn never reached the public error callback")
	}
	if session.CurrentState() != live.StateListening {
		t.Fatalf("failed turn left session in %v", session.CurrentState())
	}
	agent.mu.Lock()
	agent.err = nil
	agent.mu.Unlock()
	if err := session.SendText("next turn"); err != nil {
		t.Fatal(err)
	}
	select {
	case text := <-output:
		if text != "world" {
			t.Fatalf("next turn output = %q", text)
		}
	case <-ctx.Done():
		t.Fatal("session did not recover for the next turn")
	}
}

type authorityFailure string

func (e authorityFailure) Error() string { return "private authority diagnostic" }
func (e authorityFailure) Code() string  { return string(e) }

// Losing authority is terminal on the embedded path just as on the server.
func TestLiveProviderAuthorityFailureEndsSession(t *testing.T) {
	p := newTestLiveProvider(t, Deps{
		STT: &fakeSTT{text: "hello"}, Agent: &fakeAgent{err: authorityFailure("auth_expired")},
	})
	errorsSeen := make(chan error, 1)
	ended := make(chan struct{}, 1)
	session := live.NewSession(p, live.Callbacks{
		OnError:      func(err error) { errorsSeen <- err },
		OnTurnError:  func(err error) { t.Errorf("expired authority remained recoverable: %v", err) },
		OnSessionEnd: func() { ended <- struct{}{} },
	})
	t.Cleanup(session.Stop)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := session.Start(ctx, live.LiveConfig{Locale: "en"}, live.IdleConfig{}); err != nil {
		t.Fatal(err)
	}
	if err := session.SendText("unauthorized turn"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errorsSeen:
		var coded CodedError
		if !errors.As(err, &coded) || coded.Code() != "auth_expired" || strings.Contains(err.Error(), "private") {
			t.Fatalf("unsafe or unclassified authority error: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("authority denial did not reach the terminal error callback")
	}
	select {
	case <-ended:
		if session.CurrentState() != live.StateInactive {
			t.Fatalf("session remained active: %v", session.CurrentState())
		}
	case <-ctx.Done():
		t.Fatal("authority denial did not end the session")
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
