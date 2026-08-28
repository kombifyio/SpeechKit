// Package huggingface is the HuggingFace Inference API provider for SpeechKit.
//
// In v0.64.0 the identifiers below alias the implementation that still lives
// in pkg/speechkit/stt. v0.65.0 moves the implementation here and deletes the
// old names, so code written against this package needs no further change.
package huggingface

//lint:file-ignore SA1019 This package exists to alias the names deprecated in
// pkg/speechkit/stt; flagging its own aliases would defeat the migration window.

import "github.com/kombifyio/SpeechKit/pkg/speechkit/stt"

// Provider transcribes through a HuggingFace-hosted model.
type Provider = stt.HuggingFaceProvider

// New returns a HuggingFace provider for model, authenticated with token.
func New(model, token string) *Provider { return stt.NewHuggingFaceProvider(model, token) }
