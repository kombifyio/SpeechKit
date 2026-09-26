package azurespeech

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
)

// testValidation permits httptest.Server URLs (http://127.0.0.1:RAND). The
// production constructor keeps the strict netsec default (public https only).
var testValidation = netsec.ValidationOptions{AllowLoopback: true, AllowHTTP: true}

// successBody is the verified fast-transcription response shape with two
// speakers, phrase confidences and word timings on the first phrase.
const successBody = `{
  "durationMilliseconds": 2400,
  "combinedPhrases": [{"channel": 0, "text": "Hallo Welt. Guten Tag."}],
  "phrases": [
    {"channel": 0, "speaker": 1, "offsetMilliseconds": 0, "durationMilliseconds": 1000,
     "text": "Hallo Welt.", "locale": "de", "confidence": 0.9,
     "words": [
       {"text": "Hallo", "offsetMilliseconds": 0, "durationMilliseconds": 400},
       {"text": "Welt.", "offsetMilliseconds": 400, "durationMilliseconds": 600}
     ]},
    {"channel": 0, "speaker": 2, "offsetMilliseconds": 1200, "durationMilliseconds": 1200,
     "text": "Guten Tag.", "locale": "de", "confidence": 0.7}
  ]
}`

// capturedRequest is what the fake service saw for the last call.
type capturedRequest struct {
	Method     string
	Path       string
	Query      map[string]string
	Header     http.Header
	Audio      []byte
	Definition map[string]any
	Calls      atomic.Int32
}

// fakeService answers every request with status/body and records the last
// request, decoding the multipart transcription payload when present.
func fakeService(t *testing.T, status int, body string) (*httptest.Server, *capturedRequest) {
	t.Helper()
	captured := &capturedRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.Calls.Add(1)
		captured.Method = r.Method
		captured.Path = r.URL.Path
		captured.Query = map[string]string{}
		for key := range r.URL.Query() {
			captured.Query[key] = r.URL.Query().Get(key)
		}
		captured.Header = r.Header.Clone()
		captured.Audio = nil
		captured.Definition = nil
		if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
			if err := r.ParseMultipartForm(4 << 20); err != nil {
				t.Errorf("parse multipart: %v", err)
			} else {
				if file, _, err := r.FormFile("audio"); err == nil {
					captured.Audio, _ = io.ReadAll(file)
					_ = file.Close()
				}
				if def := r.FormValue("definition"); def != "" {
					if err := json.Unmarshal([]byte(def), &captured.Definition); err != nil {
						t.Errorf("definition is not JSON: %v", err)
					}
				}
			}
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	return server, captured
}

func newTestProvider(server *httptest.Server, opts Options) *Provider {
	opts.Host = server.URL
	p := New(opts)
	p.Validation = testValidation
	return p
}

// lookup walks a decoded JSON object; ok is false when any step is missing.
func lookup(m map[string]any, path ...string) (any, bool) {
	var current any = m
	for _, key := range path {
		obj, isObj := current.(map[string]any)
		if !isObj {
			return nil, false
		}
		next, found := obj[key]
		if !found {
			return nil, false
		}
		current = next
	}
	return current, true
}

func stringList(v any) []string {
	items, _ := v.([]any)
	out := make([]string, 0, len(items))
	for _, item := range items {
		s, _ := item.(string)
		out = append(out, s)
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func mustLookup(t *testing.T, m map[string]any, path ...string) any {
	t.Helper()
	v, ok := lookup(m, path...)
	if !ok {
		t.Fatalf("definition is missing %s: %v", strings.Join(path, "."), m)
	}
	return v
}
