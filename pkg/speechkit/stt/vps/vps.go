// Package vps is the self-hosted whisper-server provider for SpeechKit: an
// OpenAI-compatible endpoint a user runs themselves, so audio never reaches a
// commercial provider.
//
// In v0.64.0 the identifiers below alias the implementation that still lives
// in pkg/speechkit/stt. v0.65.0 moves the implementation here and deletes the
// old names, so code written against this package needs no further change.
package vps

//lint:file-ignore SA1019 This package exists to alias the names deprecated in
// pkg/speechkit/stt; flagging its own aliases would defeat the migration window.

import (
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt/openaicompat"
)

// Provider is the OpenAI-compatible adapter pointed at a self-hosted server.
type Provider = openaicompat.Provider

// New returns a VPS provider on the server's default model.
func New(baseURL, apiKey string) *Provider { return stt.NewVPSProvider(baseURL, apiKey) }

// NewWithModel returns a VPS provider pinned to model. An empty model
// defaults to "whisper-1".
func NewWithModel(baseURL, apiKey, model string) *Provider {
	return stt.NewVPSProviderWithModel(baseURL, apiKey, model)
}
