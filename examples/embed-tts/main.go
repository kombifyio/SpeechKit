package main

import (
	"context"
	"fmt"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/tts"
)

type silentProvider struct{}

func (silentProvider) Name() string                 { return "silent" }
func (silentProvider) Kind() tts.ProviderKind       { return tts.ProviderKindLocalBuiltIn }
func (silentProvider) Health(context.Context) error { return nil }
func (silentProvider) Synthesize(context.Context, string, tts.SynthesizeOpts) (*tts.Result, error) {
	return &tts.Result{Audio: []byte{}, Format: "pcm", SampleRate: 24000, Duration: time.Millisecond, Provider: "silent"}, nil
}

func main() {
	service, err := tts.NewService(tts.NewRouter(tts.StrategyCloudFirst, silentProvider{}))
	if err != nil {
		panic(err)
	}
	result, err := service.Synthesize(context.Background(), "hello", tts.SynthesizeOpts{Locale: "en-US"})
	if err != nil {
		panic(err)
	}
	fmt.Println(result.Provider)
}
