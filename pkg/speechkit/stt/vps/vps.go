// Package vps is the self-hosted whisper-server provider for SpeechKit: an
// OpenAI-compatible endpoint a user runs themselves, so audio never reaches a
// commercial provider.
//
// It is the same adapter as pkg/speechkit/stt/openaicompat with two
// differences that only make sense for a server the user owns: network
// validation accepts loopback and private addresses, and the request timeout
// tolerates a cold start.
//
// Stability: Beta — API-checked; may change with a changelog callout.
package vps

import (
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt/openaicompat"
)

// Provider is the OpenAI-compatible adapter pointed at a self-hosted server.
type Provider = openaicompat.Provider

// Options configures [New].
type Options struct {
	// BaseURL is the whisper-server endpoint root.
	BaseURL string
	// APIKey is sent as a bearer token. Empty for unauthenticated servers.
	APIKey string
	// Model pins the served model. Empty selects "whisper-1".
	Model string
}

// New creates a provider for a self-hosted whisper-server. Loopback, private
// ranges and plain http are allowed because these deployments live inside a
// VPN, on a home LAN, or on localhost.
func New(opts Options) *Provider {
	model := strings.TrimSpace(opts.Model)
	if model == "" {
		model = "whisper-1"
	}
	p := openaicompat.New(openaicompat.Options{Name: "vps", BaseURL: opts.BaseURL, APIKey: opts.APIKey, Model: model})
	p.Validation = netsec.ValidationOptions{
		AllowLoopback: true,
		AllowPrivate:  true,
		AllowHTTP:     true,
	}
	// Rebuild the HTTP client with a longer timeout — self-hosted whisper
	// may take longer to cold-start than managed cloud APIs. 5 min covers
	// Whisper Large v3 Turbo CPU-only on small VMs (2-core GH-hosted
	// runners take ~70 s for a 2 s clip with the Turbo model).
	p.SetHTTPClient(netsec.NewSafeHTTPClient(netsec.ClientOptions{
		Timeout:        5 * time.Minute,
		DialValidation: &p.Validation,
	}))
	return p
}
