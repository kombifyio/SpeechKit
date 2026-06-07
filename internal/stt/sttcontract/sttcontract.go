// Package sttcontract provides a reusable conformance suite that every
// [stt.STTProvider] implementation is expected to satisfy.
//
// Provider tests historically re-derived the same invariants (stable
// identity, provider-stamped results, HTTP-error propagation, context
// cancellation) by hand, which let subtle drift creep in — e.g. a provider
// that forgot to stamp Result.Provider, or one that swallowed a 5xx. This
// package centralises those invariants so a provider's test only has to
// supply a constructor and a success-response handler, then call [RunContract].
//
// The harness is deliberately transport-agnostic: each [Case] owns the
// wire format of its own success response via the Success handler, and the
// harness only asserts behaviour that must hold across every provider.
//
// It lives in a non-test package (rather than a _test.go helper) so it can
// be shared across provider packages. Provider conformance tests must live
// in an external test package (e.g. `package stt_test`) to avoid the import
// cycle that would otherwise form (sttcontract imports stt).
package sttcontract

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/models"
	"github.com/kombifyio/SpeechKit/internal/stt"
)

// Case describes a single STT provider under conformance test.
type Case struct {
	// Name is a short label used to namespace the subtests (e.g. "vps").
	Name string

	// ExpectedName is the stable identifier the provider must return from
	// Name() and stamp onto Result.Provider for a successful transcription.
	ExpectedName string

	// WantText is the transcription text that Success yields, decoded through
	// the provider's own response parsing.
	WantText string

	// NewProvider builds the provider bound to baseURL, which is an httptest
	// server URL. The provider must be configured to accept that loopback URL
	// (relaxed netsec validation) so the success and error paths are exercised
	// without real network access.
	NewProvider func(baseURL string) stt.STTProvider

	// Success serves exactly one successful transcription response in the
	// provider's wire format such that the provider parses WantText out of it.
	Success http.HandlerFunc
}

// contractAudio is the dummy payload fed to Transcribe. It is never inspected
// by the harness; providers that validate/normalise audio still accept bytes.
var contractAudio = []byte("sttcontract-audio")

// RunContract executes the full STT conformance suite for c. It fails the test
// if the case is under-specified, then runs the identity, success, server-error,
// and context-cancellation invariants as subtests.
func RunContract(t *testing.T, c Case) {
	t.Helper()
	if c.NewProvider == nil {
		t.Fatalf("sttcontract: Case %q has nil NewProvider", c.Name)
	}
	if c.Success == nil {
		t.Fatalf("sttcontract: Case %q has nil Success handler", c.Name)
	}
	if c.ExpectedName == "" {
		t.Fatalf("sttcontract: Case %q has empty ExpectedName", c.Name)
	}

	t.Run(c.Name+"/identity", func(t *testing.T) {
		// Name() must be pure metadata: stable, non-empty, and never touch the
		// network. Build against an unused loopback address and never call it.
		p := c.NewProvider("http://127.0.0.1:0")
		first := p.Name()
		if first != c.ExpectedName {
			t.Fatalf("Name() = %q, want %q", first, c.ExpectedName)
		}
		if second := p.Name(); second != first {
			t.Fatalf("Name() is not stable across calls: %q then %q", first, second)
		}
	})

	t.Run(c.Name+"/capabilities", func(t *testing.T) {
		// Capabilities() is optional (stt.CapabilityReporter). When present it
		// must be a non-empty, non-blank set that includes the STT modality.
		cr, ok := c.NewProvider("http://127.0.0.1:0").(stt.CapabilityReporter)
		if !ok {
			t.Skip("provider does not implement stt.CapabilityReporter")
		}
		caps := cr.Capabilities()
		if len(caps) == 0 {
			t.Fatal("Capabilities() returned no capabilities")
		}
		hasSTT := false
		for _, capability := range caps {
			if capability == "" {
				t.Error("Capabilities() contains an empty capability")
			}
			if capability == models.CapabilitySTT {
				hasSTT = true
			}
		}
		if !hasSTT {
			t.Errorf("an STT provider must report %q; got %v", models.CapabilitySTT, caps)
		}
	})

	t.Run(c.Name+"/transcribe_success", func(t *testing.T) {
		server := httptest.NewServer(c.Success)
		defer server.Close()

		p := c.NewProvider(server.URL)
		res, err := p.Transcribe(context.Background(), contractAudio, stt.TranscribeOpts{Language: "en"})
		if err != nil {
			t.Fatalf("Transcribe: unexpected error: %v", err)
		}
		if res == nil {
			t.Fatal("Transcribe returned nil Result without error")
		}
		if res.Text != c.WantText {
			t.Errorf("Result.Text = %q, want %q", res.Text, c.WantText)
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
		if _, err := p.Transcribe(context.Background(), contractAudio, stt.TranscribeOpts{}); err == nil {
			t.Error("Transcribe must return an error on HTTP 500, got nil")
		}
	})

	t.Run(c.Name+"/context_canceled", func(t *testing.T) {
		server := httptest.NewServer(c.Success)
		defer server.Close()

		p := c.NewProvider(server.URL)
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // already canceled before the call

		if _, err := p.Transcribe(ctx, contractAudio, stt.TranscribeOpts{}); err == nil {
			t.Error("Transcribe must return an error for a canceled context, got nil")
		}
	})
}
