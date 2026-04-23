package dictation

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

type fakeRecorder struct {
	pcm []byte
}

func (r *fakeRecorder) Start() error {
	return nil
}

func (r *fakeRecorder) Stop() ([]byte, error) {
	return append([]byte(nil), r.pcm...), nil
}

func (r *fakeRecorder) SetPCMHandler(func([]byte)) {}

type fakeTranscriber struct {
	transcript speechkit.Transcript
}

func (t fakeTranscriber) Transcribe(context.Context, []byte, float64, string) (speechkit.Transcript, error) {
	return t.transcript, nil
}

type fakeOutput struct {
	mu         sync.Mutex
	transcript speechkit.Transcript
	called     bool
}

func (o *fakeOutput) Deliver(_ context.Context, transcript speechkit.Transcript, _ any) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.transcript = transcript
	o.called = true
	return nil
}

func TestRuntimeRunsDictationOnlyWithFixedProfile(t *testing.T) {
	output := &fakeOutput{}
	runtime, err := NewRuntime(Options{
		Recorder: &fakeRecorder{pcm: []byte(strings.Repeat("a", 6400))},
		Transcriber: fakeTranscriber{transcript: speechkit.Transcript{
			Text:     "hello framework",
			Language: "en",
			Provider: "openai",
			Model:    "whisper-1",
			Duration: 25 * time.Millisecond,
		}},
		Output:   output,
		Language: "en",
		Policy: speechkit.RuntimePolicy{
			EnabledModes: []speechkit.Mode{speechkit.ModeDictation},
			FixedProfiles: map[speechkit.Mode]string{
				speechkit.ModeDictation: "stt.openai.whisper-1",
			},
		},
	})
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}

	if err := runtime.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	run, err := runtime.Stop(context.Background())
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if got, want := run.Transcript.Text, "hello framework"; got != want {
		t.Fatalf("run transcript = %q, want %q", got, want)
	}
	if got, want := run.ProviderProfile, "stt.openai.whisper-1"; got != want {
		t.Fatalf("provider profile = %q, want %q", got, want)
	}
	if !output.called {
		t.Fatal("output was not called")
	}
}

func TestRuntimeRejectsAssistOnlyPolicy(t *testing.T) {
	_, err := NewRuntime(Options{
		Recorder:    &fakeRecorder{pcm: []byte(strings.Repeat("a", 6400))},
		Transcriber: fakeTranscriber{},
		Policy: speechkit.RuntimePolicy{
			EnabledModes: []speechkit.Mode{speechkit.ModeAssist},
		},
	})

	if err == nil || !strings.Contains(err.Error(), "non-dictation mode") {
		t.Fatalf("NewRuntime() error = %v, want non-dictation rejection", err)
	}
}

func TestRuntimeRejectsTooShortAudio(t *testing.T) {
	runtime, err := NewRuntime(Options{
		Recorder:    &fakeRecorder{pcm: []byte("short")},
		Transcriber: fakeTranscriber{},
		Policy: speechkit.RuntimePolicy{
			EnabledModes: []speechkit.Mode{speechkit.ModeDictation},
		},
	})
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}

	if err := runtime.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	_, err = runtime.Stop(context.Background())
	if !errors.Is(err, ErrAudioTooShort) {
		t.Fatalf("Stop() error = %v, want %v", err, ErrAudioTooShort)
	}
}
