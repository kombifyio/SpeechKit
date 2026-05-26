package companion

import (
	"context"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/tts"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/wakeword"
)

type fakeAssist struct {
	request speechkit.AssistRequest
	result  speechkit.AssistResult
}

func (f *fakeAssist) Process(_ context.Context, req speechkit.AssistRequest) (speechkit.AssistResult, error) {
	f.request = req
	return f.result, nil
}

type fakeTTSProvider struct {
	text string
}

func (p *fakeTTSProvider) Synthesize(_ context.Context, text string, _ tts.SynthesizeOpts) (*tts.Result, error) {
	p.text = text
	return &tts.Result{Audio: []byte("audio"), Format: "wav", Provider: "fake-local"}, nil
}

func (p *fakeTTSProvider) Name() string                 { return "fake-local" }
func (p *fakeTTSProvider) Kind() tts.ProviderKind       { return tts.ProviderKindLocalBuiltIn }
func (p *fakeTTSProvider) Health(context.Context) error { return nil }

func TestHandsFreeWakeRunsAssistAndTTS(t *testing.T) {
	runtime := speechkit.NewRuntime(speechkit.Snapshot{}, speechkit.Hooks{})
	defer runtime.Close()

	assist := &fakeAssist{result: speechkit.AssistResult{
		Text:      "Timer set.",
		SpeakText: "Timer set.",
		Locale:    "en-US",
		Surface:   speechkit.AssistSurfaceActionAck,
	}}
	provider := &fakeTTSProvider{}
	ttsService, err := tts.NewService(tts.NewRouter(tts.StrategyLocalOnly, provider))
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	var downstream wakeword.DetectionEvent
	handsFree, err := NewHandsFree(Options{
		Runtime: runtime,
		WakeSink: wakeword.SinkFunc(func(ev wakeword.DetectionEvent) {
			downstream = ev
		}),
		WakeRequest: func(_ context.Context, ev wakeword.DetectionEvent) (speechkit.AssistRequest, bool) {
			return speechkit.AssistRequest{Text: ev.Phrase, Locale: "en-US"}, true
		},
		Assist: assist,
		TTS:    ttsService,
	})
	if err != nil {
		t.Fatalf("NewHandsFree: %v", err)
	}

	if err := handsFree.HandleWake(context.Background(), wakeword.DetectionEvent{
		Phrase:      "set a timer",
		Keyword:     "hey_quby",
		Mode:        "assist",
		Probability: 0.91,
	}); err != nil {
		t.Fatalf("HandleWake: %v", err)
	}

	if assist.request.Text != "set a timer" {
		t.Fatalf("assist request text = %q", assist.request.Text)
	}
	if provider.text != "Timer set." {
		t.Fatalf("tts text = %q", provider.text)
	}
	if downstream.Keyword != "hey_quby" {
		t.Fatalf("downstream wake = %+v", downstream)
	}

	want := []speechkit.EventType{
		speechkit.EventWakeFired,
		speechkit.EventProcessingStarted,
		speechkit.EventSkillExecuted,
		speechkit.EventTTSStarted,
		speechkit.EventTTSFinished,
	}
	for _, typ := range want {
		event := <-runtime.Events()
		if event.Type != typ {
			t.Fatalf("event = %s, want %s", event.Type, typ)
		}
	}
}
