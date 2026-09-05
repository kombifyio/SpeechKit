package local

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
)

// Registered provider-control boundary: even a local server redirect cannot
// forward microphone audio to a public endpoint. No public connection is made.
func TestLocalTranscriptionRejectsPublicRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://203.0.113.1/transcribe", http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	p := New(8080, "", "cpu")
	p.BaseURL = server.URL
	p.ready.Store(true) // Inject runtime readiness; no model subprocess is started.
	if result, err := p.Transcribe(context.Background(), []byte{1, 2}, stt.TranscribeOpts{}); err == nil || result != nil {
		t.Fatal("public redirect must fail rather than forward captured audio")
	}
}
