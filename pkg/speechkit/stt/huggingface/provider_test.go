package huggingface

import (
	"context"
	"encoding/json"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newTestHFProvider(serverURL string) *Provider {
	p := New("test-model", "test-token")
	p.BaseURL = serverURL
	p.Validation = testValidation
	p.client.Timeout = 5 * time.Second
	return p
}

func TestHF_Transcribe_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "audio/wav" {
			t.Errorf("expected audio/wav content type, got %q", r.Header.Get("Content-Type"))
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("expected Bearer test-token")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if string(body) != "fake-wav" {
			t.Errorf("unexpected request body %q", string(body))
		}
		json.NewEncoder(w).Encode(map[string]string{"text": "Hallo Welt"})
	}))
	defer server.Close()

	p := newTestHFProvider(server.URL)
	result, err := p.Transcribe(context.Background(), []byte("fake-wav"), stt.TranscribeOpts{Language: "de"})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if result.Text != "Hallo Welt" {
		t.Errorf("text = %q, want 'Hallo Welt'", result.Text)
	}
	if result.Provider != "huggingface" {
		t.Errorf("provider = %q", result.Provider)
	}
	if result.Duration < 0 {
		t.Error("duration should not be negative")
	}
}

func TestHF_Transcribe_503ModelLoading(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
		w.Write([]byte(`{"error":"Model is loading"}`))
	}))
	defer server.Close()

	p := newTestHFProvider(server.URL)
	_, err := p.Transcribe(context.Background(), []byte("wav"), stt.TranscribeOpts{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("expected 503 in error: %v", err)
	}
}

func TestHF_Transcribe_429RateLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		w.Write([]byte(`{"error":"Rate limit exceeded for secret-prompt"}`))
	}))
	defer server.Close()

	p := newTestHFProvider(server.URL)
	_, err := p.Transcribe(context.Background(), []byte("wav"), stt.TranscribeOpts{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("expected 429 in error: %v", err)
	}
	if strings.Contains(err.Error(), "secret-prompt") || strings.Contains(err.Error(), "Rate limit exceeded") {
		t.Fatalf("provider response body leaked in error: %v", err)
	}
}

func TestHF_Transcribe_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`not json secret-body`))
	}))
	defer server.Close()

	p := newTestHFProvider(server.URL)
	_, err := p.Transcribe(context.Background(), []byte("wav"), stt.TranscribeOpts{})
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if strings.Contains(err.Error(), "secret-body") || strings.Contains(err.Error(), "not json") {
		t.Fatalf("provider response body leaked in parse error: %v", err)
	}
}

func TestHF_Transcribe_EmptyText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"text": ""})
	}))
	defer server.Close()

	p := newTestHFProvider(server.URL)
	result, err := p.Transcribe(context.Background(), []byte("wav"), stt.TranscribeOpts{Language: "de"})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if result.Text != "" {
		t.Errorf("expected empty text, got %q", result.Text)
	}
}

func TestHF_Transcribe_ContextCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
	}))
	defer server.Close()

	p := newTestHFProvider(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := p.Transcribe(ctx, []byte("wav"), stt.TranscribeOpts{})
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestHF_Health_OK(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("expected GET, got %s", r.Method)
		}
		w.WriteHeader(200)
	}))
	defer server.Close()

	p := newTestHFProvider(server.URL)
	if err := p.Health(context.Background()); err != nil {
		t.Errorf("Health: %v", err)
	}
}

func TestHF_Health_503(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
	}))
	defer server.Close()

	p := newTestHFProvider(server.URL)
	if err := p.Health(context.Background()); err == nil {
		t.Error("expected error for 503")
	}
}

func TestHF_Health_404(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	p := newTestHFProvider(server.URL)
	if err := p.Health(context.Background()); err == nil {
		t.Error("expected error for 404")
	}
}

func TestHF_Name(t *testing.T) {
	p := New("model", "token")
	if p.Name() != "huggingface" {
		t.Errorf("Name() = %q", p.Name())
	}
	if p.BaseURL != hfBaseURL {
		t.Errorf("BaseURL = %q, want %q", p.BaseURL, hfBaseURL)
	}
}

// TestHF_Transcribe_ReportedLanguage is a contract test, not a coverage test.
// It previously asserted that an unpinned session reports "de", which encoded
// the very defect it looked like a guard against: HF never receives or returns
// a language, so labelling the result German was pure inference and downstream
// consumers then applied a German customization dictionary to speech in any
// language. An unpinned session must report the multilanguage sentinel, and a
// deliberate pin must survive to the label.
func TestHF_Transcribe_ReportedLanguage(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts stt.TranscribeOpts
		want string
	}{
		{name: "unpinned reports multilanguage", opts: stt.TranscribeOpts{}, want: stt.LanguageMulti},
		{name: "auto reports multilanguage", opts: stt.TranscribeOpts{Language: "auto"}, want: stt.LanguageMulti},
		{name: "explicit pin survives", opts: stt.TranscribeOpts{Language: "de"}, want: "de"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(map[string]string{"text": "test"}) //nolint:errcheck // test server write
			}))
			defer server.Close()

			p := newTestHFProvider(server.URL)
			result, err := p.Transcribe(context.Background(), []byte("wav"), tc.opts)
			if err != nil {
				t.Fatalf("Transcribe: %v", err)
			}
			if result.Language != tc.want {
				t.Errorf("reported language = %q, want %q", result.Language, tc.want)
			}
		})
	}
}
