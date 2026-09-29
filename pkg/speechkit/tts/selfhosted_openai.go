package tts

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
)

const (
	selfHostedOpenAIName         = "kokoro"
	selfHostedOpenAIDefaultModel = "kokoro"
	selfHostedOpenAIDefaultVoice = "af_bella"
)

// SelfHostedOpenAIOpts configures [SelfHostedOpenAI].
type SelfHostedOpenAIOpts struct {
	// BaseURL is the server root, e.g. "http://speechkit-tts:8880"; a
	// trailing "/v1" is accepted and stripped. Required.
	BaseURL string
	// APIKey is sent as a bearer token when set; most local servers ignore it.
	APIKey string
	// Model defaults to "kokoro".
	Model string
	// Voice defaults to the Kokoro voice "af_bella".
	Voice string
	// RequireLocal rejects endpoints that resolve to a public address. Hosts
	// running a restricted network scope set it.
	RequireLocal bool
}

// SelfHostedOpenAI is a Local Provider that speaks the OpenAI
// /v1/audio/speech API to a TTS server the operator runs on the same host or
// private network, such as Kokoro-FastAPI (Apache-2.0) or Speaches (MIT). It
// reports the provider id "kokoro", which the tts.openedai.* model_selection
// profiles pin. Plain HTTP and private addresses are allowed; no request
// leaves the operator's network unless BaseURL points outside it.
type SelfHostedOpenAI struct {
	inner *OpenAI
}

// NewSelfHostedOpenAI validates opts and returns the provider.
func NewSelfHostedOpenAI(opts SelfHostedOpenAIOpts) (*SelfHostedOpenAI, error) {
	base := strings.TrimRight(strings.TrimSpace(opts.BaseURL), "/")
	base = strings.TrimSuffix(base, "/v1")
	if base == "" {
		return nil, errors.New("self-hosted openai tts: BaseURL is required")
	}
	validation := netsec.ValidationOptions{
		AllowLoopback: true,
		AllowPrivate:  true,
		AllowHTTP:     true,
		RequireLocal:  opts.RequireLocal,
	}
	if _, err := netsec.BuildEndpoint(base, openAITTSPath, validation); err != nil {
		return nil, fmt.Errorf("self-hosted openai tts: %w", err)
	}
	inner := NewOpenAI(OpenAIOpts{
		APIKey: strings.TrimSpace(opts.APIKey),
		Model:  firstNonEmptyTTS(opts.Model, selfHostedOpenAIDefaultModel),
		Voice:  firstNonEmptyTTS(opts.Voice, selfHostedOpenAIDefaultVoice),
	})
	inner.BaseURL = base
	inner.Validation = validation
	return &SelfHostedOpenAI{inner: inner}, nil
}

// Synthesize posts to <BaseURL>/v1/audio/speech with the same option
// resolution as [OpenAI.Synthesize] and reports provider "kokoro".
func (s *SelfHostedOpenAI) Synthesize(ctx context.Context, text string, opts SynthesizeOpts) (*Result, error) {
	result, err := s.inner.Synthesize(ctx, text, opts)
	if err != nil {
		return nil, fmt.Errorf("self-hosted openai tts: %w", err)
	}
	result.Provider = selfHostedOpenAIName
	return result, nil
}

// Name returns "kokoro".
func (s *SelfHostedOpenAI) Name() string { return selfHostedOpenAIName }

// Kind reports [ProviderKindLocalProvider].
func (s *SelfHostedOpenAI) Kind() ProviderKind { return ProviderKindLocalProvider }

// Health synthesizes a one-word clip against the local server.
func (s *SelfHostedOpenAI) Health(ctx context.Context) error {
	_, err := s.Synthesize(ctx, "ok", SynthesizeOpts{Format: "mp3"})
	return err
}
