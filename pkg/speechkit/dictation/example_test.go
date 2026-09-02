package dictation_test

import (
	"context"
	"fmt"
	"log"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/dictation"
)

// silentRecorder stands in for a microphone. A real host plugs in its own
// speechkit.AudioRecorder (for example a WASAPI or PortAudio capture).
type silentRecorder struct{}

func (silentRecorder) Start() error { return nil }

func (silentRecorder) Stop() ([]byte, error) {
	// One second of 16 kHz mono S16LE silence — enough to clear MinPCMBytes.
	return make([]byte, 16000*2), nil
}

func (silentRecorder) SetPCMHandler(func([]byte)) {}

// echoTranscriber stands in for an STT backend. Use stt.AsTranscriber to adapt
// any stt.STTProvider instead of writing this by hand.
type echoTranscriber struct{}

func (echoTranscriber) Transcribe(_ context.Context, _ []byte, _ float64, language string) (speechkit.Transcript, error) {
	return speechkit.Transcript{Text: "hello world", Language: language, Provider: "example"}, nil
}

// printOutput is the host's delivery sink; the reference app injects into the
// focused text field, a server host would post to a channel or a queue.
type printOutput struct{}

func (printOutput) Deliver(_ context.Context, transcript speechkit.Transcript, target any) error {
	fmt.Printf("deliver %q to %v\n", transcript.Text, target)
	return nil
}

// Example shows the whole Dictation round trip: start capturing, stop, get
// the final text delivered and returned as a DictationRun. Dictation never
// rewrites the transcript — no LLM, no codewords — which is what makes it
// safe to route into an editor unchanged.
func Example() {
	rt, err := dictation.NewRuntime(dictation.Options{
		Recorder:    silentRecorder{},
		Transcriber: echoTranscriber{},
		Output:      printOutput{},
		Language:    "en",
		Target:      "editor",
	})
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	if err := rt.Start(ctx); err != nil {
		log.Fatal(err)
	}
	run, err := rt.Stop(ctx)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(run.Transcript.Text, run.Transcript.Language, run.AudioDurationMs)
	// Output:
	// deliver "hello world" to editor
	// hello world en 1000
}

// Example_policy shows how a host pins Dictation to one provider profile
// through speechkit.RuntimePolicy. NewRuntime validates the policy against the
// built-in catalog up front, so a misconfigured host fails at construction
// rather than on the first recording, and every DictationRun reports the
// profile that was in force.
func Example_policy() {
	rt, err := dictation.NewRuntime(dictation.Options{
		Recorder:    silentRecorder{},
		Transcriber: echoTranscriber{},
		Policy: speechkit.RuntimePolicy{
			EnabledModes:  []speechkit.Mode{speechkit.ModeDictation},
			FixedProfiles: map[speechkit.Mode]string{speechkit.ModeDictation: "stt.local.whispercpp"},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	_ = rt.Start(ctx)
	run, _ := rt.Stop(ctx)
	fmt.Println(run.ProviderProfile)

	// Enabling a non-dictation mode is rejected: this runtime is Dictation only.
	_, err = dictation.NewRuntime(dictation.Options{
		Recorder:    silentRecorder{},
		Transcriber: echoTranscriber{},
		Policy:      speechkit.RuntimePolicy{EnabledModes: []speechkit.Mode{speechkit.ModeAssist}},
	})
	fmt.Println(err != nil)
	// Output:
	// stt.local.whispercpp
	// true
}
