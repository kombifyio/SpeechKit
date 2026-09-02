package tts_test

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/tts"
)

// fakeVoice is a minimal tts.Provider. Real hosts use tts.Piper for the
// local-only path or one of the cloud providers (tts.OpenAI, tts.Deepgram, ...).
type fakeVoice struct {
	name string
	kind tts.ProviderKind
	fail bool
}

func (v fakeVoice) Name() string                 { return v.name }
func (v fakeVoice) Kind() tts.ProviderKind       { return v.kind }
func (v fakeVoice) Health(context.Context) error { return nil }
func (v fakeVoice) Synthesize(_ context.Context, text string, opts tts.SynthesizeOpts) (*tts.Result, error) {
	if v.fail {
		return nil, errors.New(v.name + " unavailable")
	}
	return &tts.Result{Audio: []byte(text), Format: "pcm", Provider: v.name, Voice: opts.Voice}, nil
}

// ExampleNewService shows the stable embedding path: build a Router with the
// providers the host has credentials for, wrap it in a Service that carries
// the host's default voice, and synthesize. The Router falls back in order
// when a provider fails, so a flaky cloud voice never blocks speech output.
func ExampleNewService() {
	router := tts.NewRouter(tts.StrategyCloudFirst,
		fakeVoice{name: "cloud", kind: tts.ProviderKindCloudProvider, fail: true},
		fakeVoice{name: "piper", kind: tts.ProviderKindLocalBuiltIn},
	)
	svc, err := tts.NewService(router, tts.WithDefaultOpts(tts.SynthesizeOpts{Voice: "de-thorsten"}))
	if err != nil {
		log.Fatal(err)
	}

	res, err := svc.Synthesize(context.Background(), "Guten Morgen")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(res.Provider, res.Voice, len(res.Audio) > 0)
	// Output: piper de-thorsten true
}

// ExampleNewRouter_localOnly is the fresh-install path: no cloud providers are
// eligible, so a cloud voice in the list is skipped without being called.
func ExampleNewRouter_localOnly() {
	router := tts.NewRouter(tts.StrategyLocalOnly,
		fakeVoice{name: "cloud", kind: tts.ProviderKindCloudProvider},
		fakeVoice{name: "piper", kind: tts.ProviderKindLocalBuiltIn},
	)

	res, err := router.Synthesize(context.Background(), "hello", tts.SynthesizeOpts{})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(res.Provider)
	// Output: piper
}
