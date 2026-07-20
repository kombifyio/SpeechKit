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
	err     error
	calls   int
}

func (f *fakeAssist) Process(_ context.Context, req speechkit.AssistRequest) (speechkit.AssistResult, error) {
	f.calls++
	f.request = req
	if f.err != nil {
		return speechkit.AssistResult{}, f.err
	}
	return f.result, nil
}

type fakeTTSProvider struct {
	text  string
	calls int
}

func (p *fakeTTSProvider) Synthesize(_ context.Context, text string, _ tts.SynthesizeOpts) (*tts.Result, error) {
	p.calls++
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

func TestHandsFreeWakeInvokesOnResult(t *testing.T) {
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

	var got speechkit.AssistResult
	calls := 0
	handsFree, err := NewHandsFree(Options{
		Runtime: runtime,
		WakeRequest: func(_ context.Context, ev wakeword.DetectionEvent) (speechkit.AssistRequest, bool) {
			return speechkit.AssistRequest{Text: ev.Phrase, Locale: "en-US"}, true
		},
		Assist: assist,
		TTS:    ttsService,
		OnResult: func(_ context.Context, r speechkit.AssistResult) {
			calls++
			got = r
		},
	})
	if err != nil {
		t.Fatalf("NewHandsFree: %v", err)
	}

	if err := handsFree.HandleWake(context.Background(), wakeword.DetectionEvent{
		Phrase: "set a timer",
		Mode:   "assist",
	}); err != nil {
		t.Fatalf("HandleWake: %v", err)
	}

	if calls != 1 {
		t.Fatalf("OnResult calls = %d, want 1", calls)
	}
	if got.Text != "Timer set." {
		t.Fatalf("OnResult result text = %q", got.Text)
	}
	if got.Audio.Len() == 0 {
		t.Fatal("OnResult result should carry synthesized audio for host playback")
	}
}

func TestHandsFreeWakePublishesStagesInOrder(t *testing.T) {
	runtime := speechkit.NewRuntime(speechkit.Snapshot{}, speechkit.Hooks{})
	defer runtime.Close()

	assist := &fakeAssist{result: speechkit.AssistResult{
		Text:      "Es sind 21 Grad.",
		SpeakText: "Es sind 21 Grad.",
		Locale:    "de-DE",
	}}
	ttsService, err := tts.NewService(tts.NewRouter(tts.StrategyLocalOnly, &fakeTTSProvider{}))
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	var stages []Stage
	handsFree, err := NewHandsFree(Options{
		Runtime: runtime,
		WakeRequest: func(_ context.Context, ev wakeword.DetectionEvent) (speechkit.AssistRequest, bool) {
			return speechkit.AssistRequest{Text: ev.Phrase, Locale: "de-DE"}, true
		},
		Assist:  assist,
		TTS:     ttsService,
		OnStage: func(s Stage) { stages = append(stages, s) },
	})
	if err != nil {
		t.Fatalf("NewHandsFree: %v", err)
	}

	if err := handsFree.HandleWake(context.Background(), wakeword.DetectionEvent{
		Phrase: "wie warm ist es",
		Mode:   "assist",
	}); err != nil {
		t.Fatalf("HandleWake: %v", err)
	}

	want := []Stage{StageWake, StageListening, StageThinking, StageSpeaking, StageIdle}
	assertStages(t, stages, want)
}

func TestHandsFreeWakeStagesOnAbortedCapture(t *testing.T) {
	runtime := speechkit.NewRuntime(speechkit.Snapshot{}, speechkit.Hooks{})
	defer runtime.Close()

	var stages []Stage
	handsFree, err := NewHandsFree(Options{
		Runtime: runtime,
		WakeRequest: func(context.Context, wakeword.DetectionEvent) (speechkit.AssistRequest, bool) {
			return speechkit.AssistRequest{}, false
		},
		Assist:  &fakeAssist{},
		OnStage: func(s Stage) { stages = append(stages, s) },
	})
	if err != nil {
		t.Fatalf("NewHandsFree: %v", err)
	}

	if err := handsFree.HandleWake(context.Background(), wakeword.DetectionEvent{Mode: "assist"}); err != nil {
		t.Fatalf("HandleWake: %v", err)
	}

	assertStages(t, stages, []Stage{StageWake, StageListening, StageIdle})
}

func TestHandsFreeWakeStagesOnAssistError(t *testing.T) {
	runtime := speechkit.NewRuntime(speechkit.Snapshot{}, speechkit.Hooks{})
	defer runtime.Close()

	var stages []Stage
	handsFree, err := NewHandsFree(Options{
		Runtime: runtime,
		WakeRequest: func(_ context.Context, ev wakeword.DetectionEvent) (speechkit.AssistRequest, bool) {
			return speechkit.AssistRequest{Text: ev.Phrase}, true
		},
		Assist:  &fakeAssist{err: context.DeadlineExceeded},
		OnStage: func(s Stage) { stages = append(stages, s) },
	})
	if err != nil {
		t.Fatalf("NewHandsFree: %v", err)
	}

	if err := handsFree.HandleWake(context.Background(), wakeword.DetectionEvent{Mode: "assist"}); err == nil {
		t.Fatal("HandleWake should propagate the assist error")
	}

	assertStages(t, stages, []Stage{StageWake, StageListening, StageThinking, StageError})
}

func assertStages(t *testing.T, got, want []Stage) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("stages = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("stage[%d] = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}

type fakeVoiceAgent struct {
	started bool
}

func (f *fakeVoiceAgent) Start(context.Context) error {
	f.started = true
	return nil
}

func (f *fakeVoiceAgent) Stop(context.Context) (speechkit.VoiceAgentSession, error) {
	return speechkit.VoiceAgentSession{}, nil
}

func (f *fakeVoiceAgent) SendText(context.Context, string) error { return nil }

func (f *fakeVoiceAgent) CurrentSession(context.Context) (speechkit.VoiceAgentSession, error) {
	return speechkit.VoiceAgentSession{}, nil
}

func TestHandsFreeWakeStartsVoiceAgent(t *testing.T) {
	runtime := speechkit.NewRuntime(speechkit.Snapshot{}, speechkit.Hooks{})
	defer runtime.Close()

	voiceAgent := &fakeVoiceAgent{}
	handsFree, err := NewHandsFree(Options{
		Runtime:    runtime,
		TargetMode: TargetVoiceAgent,
		VoiceAgent: voiceAgent,
	})
	if err != nil {
		t.Fatalf("NewHandsFree: %v", err)
	}

	if err := handsFree.HandleWake(context.Background(), wakeword.DetectionEvent{
		Phrase: "join the game",
		Mode:   "assist",
	}); err != nil {
		t.Fatalf("HandleWake: %v", err)
	}

	if !voiceAgent.started {
		t.Fatal("voice agent was not started")
	}
	if event := <-runtime.Events(); event.Type != speechkit.EventWakeFired || event.Mode != string(TargetVoiceAgent) {
		t.Fatalf("wake event = %+v", event)
	}
	if event := <-runtime.Events(); event.Type != speechkit.EventCompanionSessionStarted || event.Mode != "voice_agent" {
		t.Fatalf("start event = %+v", event)
	}
}

func TestHandsFreeWakeUsesEventModeWhenTargetModeUnset(t *testing.T) {
	runtime := speechkit.NewRuntime(speechkit.Snapshot{}, speechkit.Hooks{})
	defer runtime.Close()

	voiceAgent := &fakeVoiceAgent{}
	handsFree, err := NewHandsFree(Options{
		Runtime:    runtime,
		VoiceAgent: voiceAgent,
	})
	if err != nil {
		t.Fatalf("NewHandsFree: %v", err)
	}

	if err := handsFree.HandleWake(context.Background(), wakeword.DetectionEvent{
		Phrase: "join the game",
		Mode:   "voice_agent",
	}); err != nil {
		t.Fatalf("HandleWake: %v", err)
	}

	if !voiceAgent.started {
		t.Fatal("voice agent was not started")
	}
}

func TestHandsFreeWakeStartsDictationUIAssistedWithoutTTS(t *testing.T) {
	var gotCommand speechkit.Command
	runtime := speechkit.NewRuntime(speechkit.Snapshot{}, speechkit.Hooks{
		HandleCommand: func(_ context.Context, command speechkit.Command) error {
			gotCommand = command
			return nil
		},
	})
	defer runtime.Close()

	assist := &fakeAssist{result: speechkit.AssistResult{
		Text:      "ignored",
		SpeakText: "ignored",
		Surface:   speechkit.AssistSurfaceActionAck,
	}}
	provider := &fakeTTSProvider{}
	ttsService, err := tts.NewService(tts.NewRouter(tts.StrategyLocalOnly, provider))
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	handsFree, err := NewHandsFree(Options{
		Runtime:    runtime,
		TargetMode: TargetDictationUIAssisted,
		Assist:     assist,
		TTS:        ttsService,
	})
	if err != nil {
		t.Fatalf("NewHandsFree: %v", err)
	}

	if err := handsFree.HandleWake(context.Background(), wakeword.DetectionEvent{
		Phrase: "start notes",
		Mode:   "dictation_ui_assisted",
	}); err != nil {
		t.Fatalf("HandleWake: %v", err)
	}

	if gotCommand.Type != speechkit.CommandStartMode {
		t.Fatalf("command type = %q, want %q", gotCommand.Type, speechkit.CommandStartMode)
	}
	if gotCommand.Metadata["mode"] != "dictate" {
		t.Fatalf("command mode = %q, want dictate", gotCommand.Metadata["mode"])
	}
	if gotCommand.Metadata["hands_free_target"] != string(TargetDictationUIAssisted) {
		t.Fatalf("hands_free_target = %q", gotCommand.Metadata["hands_free_target"])
	}
	if assist.calls != 0 {
		t.Fatalf("assist calls = %d, want 0", assist.calls)
	}
	if provider.calls != 0 {
		t.Fatalf("tts calls = %d, want 0", provider.calls)
	}
	if event := <-runtime.Events(); event.Type != speechkit.EventWakeFired || event.Mode != string(TargetDictationUIAssisted) {
		t.Fatalf("wake event = %+v", event)
	}
}
