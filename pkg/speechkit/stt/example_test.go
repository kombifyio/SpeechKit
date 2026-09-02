package stt_test

import (
	"context"
	"fmt"
	"log"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
)

// echoProvider is a minimal stt.STTProvider. Real hosts use the providers in
// the stt subpackages (local, deepgram, openai, ...) or their own backend.
type echoProvider struct{ name string }

func (p echoProvider) Name() string                 { return p.name }
func (p echoProvider) Health(context.Context) error { return nil }
func (p echoProvider) Transcribe(_ context.Context, _ []byte, opts stt.TranscribeOpts) (*stt.Result, error) {
	return &stt.Result{Text: "hello from " + p.name, Language: opts.Language, Provider: p.name}, nil
}

// ExampleAsTranscriber bridges an STTProvider to the kernel's
// speechkit.Transcriber so it can be handed straight to dictation.NewRuntime
// or a TranscriptionWorker. The per-call language wins over the base options.
func ExampleAsTranscriber() {
	var transcriber speechkit.Transcriber = stt.AsTranscriber(
		echoProvider{name: "echo"},
		stt.WithTranscribeOpts(stt.TranscribeOpts{Language: "de"}),
	)

	wav := speechkit.PCMToWAV(make([]byte, 16000*2))
	transcript, err := transcriber.Transcribe(context.Background(), wav, 1.0, "")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(transcript.Text, transcript.Language, transcript.Provider)

	transcript, _ = transcriber.Transcribe(context.Background(), wav, 1.0, "en")
	fmt.Println(transcript.Language)
	// Output:
	// hello from echo de echo
	// en
}

// ExampleRouter wires one local provider into a Router and pins the strategy
// to local-only, which is the fresh-install path with no cloud keys. The
// per-instance OnProviderSelected hook replaces the deprecated process-wide
// observer, so two routers in one process never share audit state.
func ExampleRouter() {
	router := &stt.Router{
		Strategy: stt.StrategyLocalOnly,
		OnProviderSelected: func(_ context.Context, provider string, strategy stt.Strategy) {
			fmt.Println("selected", provider, "via", strategy)
		},
	}
	router.SetLocal(echoProvider{name: "local"})

	result, err := router.Route(context.Background(), speechkit.PCMToWAV(make([]byte, 16000*2)), 1.0, stt.TranscribeOpts{Language: "en"})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(result.Text)
	// Output:
	// selected local via local-only
	// hello from local
}
