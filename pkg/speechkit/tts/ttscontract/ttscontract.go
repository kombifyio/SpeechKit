// Package ttscontract provides a reusable conformance suite that every
// [tts.Provider] implementation is expected to satisfy.
//
// Like its STT sibling, it centralises the cross-provider invariants —
// stable identity, a valid deployment Kind, provider-stamped non-empty audio
// results, HTTP-error propagation, and context cancellation — so a provider
// test only supplies a constructor and a success-response handler, then calls
// [RunContract].
//
// The harness lives in a non-test package so it can be shared across provider
// packages. Conformance tests must live in an external test package
// (e.g. `package tts_test`) to avoid the import cycle that would form
// (ttscontract imports tts).
package ttscontract

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/tts"
)

// validKinds is the closed set of deployment classes a provider may report.
var validKinds = map[tts.ProviderKind]bool{
	tts.ProviderKindLocalBuiltIn:   true,
	tts.ProviderKindLocalProvider:  true,
	tts.ProviderKindCloudProvider:  true,
	tts.ProviderKindDirectProvider: true,
}

// Case describes a single TTS provider under conformance test.
type Case struct {
	// Name is a short label used to namespace the subtests (e.g. "openai").
	Name string

	// ExpectedName is the stable identifier the provider must return from
	// Name() and stamp onto Result.Provider for a successful synthesis.
	ExpectedName string

	// NewProvider builds the provider bound to baseURL, an httptest server URL.
	// The provider must be configured to accept that loopback URL (relaxed
	// netsec validation).
	NewProvider func(baseURL string) tts.Provider

	// Success serves exactly one successful synthesis response in the
	// provider's wire format (raw audio bytes or a JSON envelope).
	Success http.HandlerFunc
}

// RunContract executes the full TTS conformance suite for c.
func RunContract(t *testing.T, c Case) {
	t.Helper()
	if c.NewProvider == nil {
		t.Fatalf("ttscontract: Case %q has nil NewProvider", c.Name)
	}
	if c.Success == nil {
		t.Fatalf("ttscontract: Case %q has nil Success handler", c.Name)
	}
	if c.ExpectedName == "" {
		t.Fatalf("ttscontract: Case %q has empty ExpectedName", c.Name)
	}

	t.Run(c.Name+"/identity", func(t *testing.T) {
		// Name()/Kind() are pure metadata: stable, non-empty, no network.
		p := c.NewProvider("http://127.0.0.1:0")
		first := p.Name()
		if first != c.ExpectedName {
			t.Fatalf("Name() = %q, want %q", first, c.ExpectedName)
		}
		if second := p.Name(); second != first {
			t.Fatalf("Name() is not stable across calls: %q then %q", first, second)
		}
		if k := p.Kind(); !validKinds[k] {
			t.Errorf("Kind() = %q, want one of the known tts.ProviderKind values", k)
		}
	})

	t.Run(c.Name+"/capabilities", func(t *testing.T) {
		// Capabilities() is optional (tts.CapabilityReporter). When present it
		// must be a non-empty, non-blank set that includes the TTS modality.
		cr, ok := c.NewProvider("http://127.0.0.1:0").(tts.CapabilityReporter)
		if !ok {
			t.Skip("provider does not implement tts.CapabilityReporter")
		}
		caps := cr.Capabilities()
		if len(caps) == 0 {
			t.Fatal("Capabilities() returned no capabilities")
		}
		hasTTS := false
		for _, capability := range caps {
			if capability == "" {
				t.Error("Capabilities() contains an empty capability")
			}
			if capability == speechkit.CapabilityTTS {
				hasTTS = true
			}
		}
		if !hasTTS {
			t.Errorf("a TTS provider must report %q; got %v", speechkit.CapabilityTTS, caps)
		}
	})

	t.Run(c.Name+"/synthesize_success", func(t *testing.T) {
		server := httptest.NewServer(c.Success)
		defer server.Close()

		p := c.NewProvider(server.URL)
		res, err := p.Synthesize(context.Background(), "hello from the contract suite", tts.SynthesizeOpts{Format: "mp3"})
		if err != nil {
			t.Fatalf("Synthesize: unexpected error: %v", err)
		}
		if res == nil {
			t.Fatal("Synthesize returned nil Result without error")
		}
		if len(res.Audio) == 0 {
			t.Error("Result.Audio is empty on a successful synthesis")
		}
		if res.Format == "" {
			t.Error("Result.Format must be set on a successful synthesis")
		}
		if res.Provider != c.ExpectedName {
			t.Errorf("Result.Provider = %q, want %q (providers must stamp their identity)", res.Provider, c.ExpectedName)
		}
	})

	t.Run(c.Name+"/server_error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		}))
		defer server.Close()

		p := c.NewProvider(server.URL)
		if _, err := p.Synthesize(context.Background(), "x", tts.SynthesizeOpts{}); err == nil {
			t.Error("Synthesize must return an error on HTTP 500, got nil")
		}
	})

	t.Run(c.Name+"/context_canceled", func(t *testing.T) {
		server := httptest.NewServer(c.Success)
		defer server.Close()

		p := c.NewProvider(server.URL)
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // already canceled before the call

		if _, err := p.Synthesize(ctx, "x", tts.SynthesizeOpts{}); err == nil {
			t.Error("Synthesize must return an error for a canceled context, got nil")
		}
	})
}
