// Package google is the Google Cloud Speech-to-Text provider for SpeechKit.
//
// Importing it pulls the Google Cloud client and gRPC stack; a host that
// speaks to a different backend should not import this package. That
// separation is the point of the per-provider split — see the migration table
// in CHANGELOG.md for v0.64.0.
//
// In v0.64.0 the identifiers below alias the implementation that still lives
// in pkg/speechkit/stt. v0.65.0 moves the implementation here and deletes the
// old names, so code written against this package needs no further change.
package google

//lint:file-ignore SA1019 This package exists to alias the names deprecated in
// pkg/speechkit/stt; flagging its own aliases would defeat the migration window.

import "github.com/kombifyio/SpeechKit/pkg/speechkit/stt"

// Provider transcribes through Google Cloud Speech-to-Text v1 and, with
// streaming credentials configured, through the streaming API.
type Provider = stt.GoogleSTTProvider

// New returns a Google provider for apiKey. An empty model uses the
// provider default.
func New(apiKey, model string) *Provider { return stt.NewGoogleSTTProvider(apiKey, model) }
