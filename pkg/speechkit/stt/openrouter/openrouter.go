// Package openrouter is the OpenRouter provider for SpeechKit.
//
// In v0.64.0 the identifiers below alias the implementation that still lives
// in pkg/speechkit/stt. v0.65.0 moves the implementation here and deletes the
// old names, so code written against this package needs no further change.
package openrouter

//lint:file-ignore SA1019 This package exists to alias the names deprecated in
// pkg/speechkit/stt; flagging its own aliases would defeat the migration window.

import "github.com/kombifyio/SpeechKit/pkg/speechkit/stt"

// Provider transcribes through OpenRouter.
type Provider = stt.OpenRouterSTTProvider

// New returns an OpenRouter provider for apiKey. An empty model uses the
// provider default.
func New(apiKey, model string) *Provider { return stt.NewOpenRouterSTTProvider(apiKey, model) }
