package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestClientAddsBearerAndDecodesStatus(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/readyz" {
			t.Fatalf("path = %s, want /readyz", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "version": "test"})
	}))
	defer srv.Close()

	c, err := New(Options{BaseURL: srv.URL, Token: "secret"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	status, err := c.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.Status != "ok" || status.Version != "test" {
		t.Fatalf("status = %+v", status)
	}
	if gotAuth != "Bearer secret" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
}

func TestClientReturnsHTTPErrorBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusTeapot)
	}))
	defer srv.Close()

	c, err := New(Options{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = c.Status(context.Background())
	if err == nil {
		t.Fatal("Status succeeded, want error")
	}
	httpErr, ok := err.(HTTPError)
	if !ok {
		t.Fatalf("error type = %T, want HTTPError", err)
	}
	if httpErr.StatusCode != http.StatusTeapot || !strings.Contains(httpErr.Body, "nope") {
		t.Fatalf("HTTPError = %+v", httpErr)
	}
}

func TestClientTypedVocabularyAndTTSMethods(t *testing.T) {
	var gotPaths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPaths = append(gotPaths, r.Method+" "+r.URL.RequestURI())
		switch r.URL.Path {
		case "/v1/vocabulary/dictionary":
			_ = json.NewEncoder(w).Encode(map[string]any{"entries": []map[string]any{{"spoken": "codex", "canonical": "Codex", "language": "en", "enabled": true}}})
		case "/v1/tts/synthesize":
			_ = json.NewEncoder(w).Encode(map[string]any{"audio_base64": "YXVkaW8=", "format": "mp3", "provider": "openai", "voice": "nova"})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	c, err := New(Options{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	entries, err := c.VocabularyEntries(context.Background(), "en")
	if err != nil {
		t.Fatalf("VocabularyEntries: %v", err)
	}
	if len(entries) != 1 || entries[0].Canonical != "Codex" {
		t.Fatalf("entries = %+v", entries)
	}
	tts, err := c.TTSSynthesize(context.Background(), TTSSynthesizeRequest{Text: "hello"})
	if err != nil {
		t.Fatalf("TTSSynthesize: %v", err)
	}
	if tts.Format != "mp3" || tts.Provider != "openai" {
		t.Fatalf("tts = %+v", tts)
	}
	want := []string{"GET /v1/vocabulary/dictionary?language=en", "POST /v1/tts/synthesize"}
	if strings.Join(gotPaths, "\n") != strings.Join(want, "\n") {
		t.Fatalf("paths = %v, want %v", gotPaths, want)
	}
}

func TestClientEndpointWrappersUseExpectedHTTPContracts(t *testing.T) {
	type seenRequest struct {
		method string
		uri    string
		auth   string
		ua     string
		body   string
	}
	var seen []seenRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		seen = append(seen, seenRequest{
			method: r.Method,
			uri:    r.URL.RequestURI(),
			auth:   r.Header.Get("Authorization"),
			ua:     r.Header.Get("User-Agent"),
			body:   string(raw),
		})
		w.Header().Set("Content-Type", "application/json")

		switch r.Method + " " + r.URL.RequestURI() {
		case "GET /v1/config":
			_, _ = w.Write([]byte(`{"mode":"test"}`))
		case "GET /v1/catalog/readiness":
			_, _ = w.Write([]byte(`{"readiness":[]}`))
		case "GET /v1/catalog/profiles/profile%2Fid/readiness":
			_, _ = w.Write([]byte(`{"profileId":"profile/id","ready":true}`))
		case "GET /v1/catalog/profiles?mode=assist+mode":
			_, _ = w.Write([]byte(`{"profiles":[]}`))
		case "GET /v1/catalog/contracts":
			_, _ = w.Write([]byte(`{"contracts":[]}`))
		case "GET /v1/personas", "GET /v1/roles", "GET /v1/sequences":
			_, _ = w.Write([]byte(`{"items":[]}`))
		case "GET /v1/personas/persona%2Fid", "GET /v1/roles/role%2Fid", "GET /v1/sequences/sequence%2Fid":
			_, _ = w.Write([]byte(`{"id":"ok"}`))
		case "POST /v1/personas", "POST /v1/roles", "POST /v1/sequences":
			_, _ = w.Write([]byte(`{"created":true}`))
		case "PATCH /v1/personas/persona%2Fid", "PATCH /v1/roles/role%2Fid", "PATCH /v1/sequences/sequence%2Fid":
			_, _ = w.Write([]byte(`{"updated":true}`))
		case "DELETE /v1/personas/persona%2Fid", "DELETE /v1/roles/role%2Fid", "DELETE /v1/sequences/sequence%2Fid":
			w.WriteHeader(http.StatusNoContent)
		case "POST /v1/vocabulary/dictionary":
			_, _ = w.Write([]byte(`{"entries":[{"spoken":"sk","canonical":"SpeechKit","language":"en","enabled":true}]}`))
		case "GET /v1/transcripts?limit=2":
			_, _ = w.Write([]byte(`{"transcripts":[{"id":7,"text":"hello","language":"en","provider":"test","model":"stub","durationMs":1,"latencyMs":2,"createdAt":"2026-05-07T00:00:00Z"}]}`))
		case "GET /v1/transcripts/7":
			_, _ = w.Write([]byte(`{"id":7,"text":"hello","language":"en","provider":"test","model":"stub","durationMs":1,"latencyMs":2,"createdAt":"2026-05-07T00:00:00Z"}`))
		case "GET /v1/voiceagent/sessions/8/transcript":
			_, _ = w.Write([]byte(`{"id":8,"transcript":"hi","language":"en","created_at":"2026-05-07T00:00:00Z"}`))
		case "GET /v1/voiceagent/sessions/9/summary":
			_, _ = w.Write([]byte(`{"id":9,"summary":{"topic":"test"},"language":"en","created_at":"2026-05-07T00:00:00Z"}`))
		case "GET /v1/tts/voices":
			_, _ = w.Write([]byte(`{"voices":[{"provider":"openai","id":"nova","locale":"en","default":true}]}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer srv.Close()

	c, err := New(Options{BaseURL: srv.URL + "/", Token: "tok", UserAgent: "speechkit-test/1"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()

	if cfg, err := c.Config(ctx); err != nil || cfg["mode"] != "test" {
		t.Fatalf("Config = %#v, %v", cfg, err)
	}
	if readiness, err := c.CatalogReadiness(ctx); err != nil || len(readiness) != 0 {
		t.Fatalf("CatalogReadiness = %#v, %v", readiness, err)
	}
	if readiness, err := c.ProviderReadiness(ctx, " profile/id "); err != nil || readiness == nil {
		t.Fatalf("ProviderReadiness = %#v, %v", readiness, err)
	}
	if profiles, err := c.CatalogProfiles(ctx, "assist mode"); err != nil || len(profiles) != 0 {
		t.Fatalf("CatalogProfiles = %#v, %v", profiles, err)
	}
	if contracts, err := c.CatalogContracts(ctx); err != nil || len(contracts) != 0 {
		t.Fatalf("CatalogContracts = %#v, %v", contracts, err)
	}
	rawCalls := []struct {
		name string
		fn   func(context.Context) (json.RawMessage, error)
	}{
		{"PersonasList", c.PersonasList},
		{"Personas", c.Personas},
		{"Persona", func(ctx context.Context) (json.RawMessage, error) { return c.Persona(ctx, " persona/id ") }},
		{"CreatePersona", func(ctx context.Context) (json.RawMessage, error) {
			return c.CreatePersona(ctx, map[string]string{"name": "p"})
		}},
		{"UpdatePersona", func(ctx context.Context) (json.RawMessage, error) {
			return c.UpdatePersona(ctx, " persona/id ", map[string]string{"name": "p2"})
		}},
		{"Roles", c.Roles},
		{"Role", func(ctx context.Context) (json.RawMessage, error) { return c.Role(ctx, " role/id ") }},
		{"CreateRole", func(ctx context.Context) (json.RawMessage, error) {
			return c.CreateRole(ctx, map[string]string{"name": "r"})
		}},
		{"UpdateRole", func(ctx context.Context) (json.RawMessage, error) {
			return c.UpdateRole(ctx, " role/id ", map[string]string{"name": "r2"})
		}},
		{"Sequences", c.Sequences},
		{"Sequence", func(ctx context.Context) (json.RawMessage, error) { return c.Sequence(ctx, " sequence/id ") }},
		{"CreateSequence", func(ctx context.Context) (json.RawMessage, error) {
			return c.CreateSequence(ctx, map[string]string{"name": "s"})
		}},
		{"UpdateSequence", func(ctx context.Context) (json.RawMessage, error) {
			return c.UpdateSequence(ctx, " sequence/id ", map[string]string{"name": "s2"})
		}},
	}
	for _, call := range rawCalls {
		raw, err := call.fn(ctx)
		if err != nil {
			t.Fatalf("%s: %v", call.name, err)
		}
		if len(raw) == 0 {
			t.Fatalf("%s returned empty raw JSON", call.name)
		}
	}
	if err := c.DeletePersona(ctx, " persona/id "); err != nil {
		t.Fatalf("DeletePersona: %v", err)
	}
	if err := c.DeleteRole(ctx, " role/id "); err != nil {
		t.Fatalf("DeleteRole: %v", err)
	}
	if err := c.DeleteSequence(ctx, " sequence/id "); err != nil {
		t.Fatalf("DeleteSequence: %v", err)
	}
	if entries, err := c.ReplaceVocabularyEntries(ctx, " en ", []DictionaryEntry{{Spoken: "sk", Canonical: "SpeechKit", Language: "en", Enabled: true}}); err != nil || len(entries) != 1 {
		t.Fatalf("ReplaceVocabularyEntries = %#v, %v", entries, err)
	}
	if transcripts, err := c.Transcripts(ctx, 2); err != nil || len(transcripts) != 1 || transcripts[0].ID != 7 {
		t.Fatalf("Transcripts = %#v, %v", transcripts, err)
	}
	if transcript, err := c.Transcript(ctx, 7); err != nil || transcript.ID != 7 {
		t.Fatalf("Transcript = %#v, %v", transcript, err)
	}
	if transcript, err := c.VoiceAgentSessionTranscript(ctx, 8); err != nil || transcript.ID != 8 {
		t.Fatalf("VoiceAgentSessionTranscript = %#v, %v", transcript, err)
	}
	if summary, err := c.VoiceAgentSessionSummary(ctx, 9); err != nil || summary.ID != 9 {
		t.Fatalf("VoiceAgentSessionSummary = %#v, %v", summary, err)
	}
	if voices, err := c.TTSVoices(ctx); err != nil || len(voices) != 1 || voices[0].ID != "nova" {
		t.Fatalf("TTSVoices = %#v, %v", voices, err)
	}

	for _, req := range seen {
		if req.auth != "Bearer tok" {
			t.Fatalf("%s %s Authorization = %q", req.method, req.uri, req.auth)
		}
		if req.ua != "speechkit-test/1" {
			t.Fatalf("%s %s User-Agent = %q", req.method, req.uri, req.ua)
		}
	}
}

func TestClientTranscribeFileSendsMultipartFields(t *testing.T) {
	var gotFilename, gotLanguage, gotModel, gotPrompt string
	var gotAudio string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/dictation/transcribe" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data; boundary=") {
			t.Fatalf("Content-Type = %q", r.Header.Get("Content-Type"))
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("ParseMultipartForm: %v", err)
		}
		gotLanguage = r.FormValue("language")
		gotModel = r.FormValue("model")
		gotPrompt = r.FormValue("prompt")
		file, header, err := r.FormFile("audio")
		if err != nil {
			t.Fatalf("FormFile: %v", err)
		}
		defer file.Close()
		raw, _ := io.ReadAll(file)
		gotFilename = header.Filename
		gotAudio = string(raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"ok","duration_ms":10,"latency_ms":2,"provider":"stub"}`))
	}))
	defer srv.Close()

	audioFile, err := os.CreateTemp(t.TempDir(), "clip-*.wav")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	if _, err := audioFile.WriteString("audio-bytes"); err != nil {
		t.Fatalf("write audio file: %v", err)
	}
	if err := audioFile.Close(); err != nil {
		t.Fatalf("close audio file: %v", err)
	}

	c, err := New(Options{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	res, err := c.TranscribeFile(context.Background(), audioFile.Name(), TranscribeOptions{
		Language: "en",
		Model:    "whisper",
		Prompt:   "terms",
	})
	if err != nil {
		t.Fatalf("TranscribeFile: %v", err)
	}
	if res.Text != "ok" || res.DurationMs != 10 {
		t.Fatalf("response = %#v", res)
	}
	if gotFilename == "" || gotLanguage != "en" || gotModel != "whisper" || gotPrompt != "terms" || gotAudio != "audio-bytes" {
		t.Fatalf("multipart filename=%q language=%q model=%q prompt=%q audio=%q", gotFilename, gotLanguage, gotModel, gotPrompt, gotAudio)
	}
}

func TestClientFromEnvAndHelpers(t *testing.T) {
	t.Setenv("SPEECHKIT_SERVER_URL", "https://speechkit.test/api")
	t.Setenv("SPEECHKIT_TOKEN", "")
	t.Setenv("SPEECHKIT_SERVER_TOKEN", " server-token ")

	c, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if got := c.resolve("/readyz"); got != "https://speechkit.test/readyz" {
		t.Fatalf("resolved URL = %q", got)
	}
	if c.token != "server-token" {
		t.Fatalf("token = %q", c.token)
	}
	if got := firstNonEmpty("", "  first  ", "second"); got != "first" {
		t.Fatalf("firstNonEmpty = %q", got)
	}
	if (HTTPError{StatusCode: 500}).Error() != "speechkit: HTTP 500" {
		t.Fatalf("HTTPError without body rendered unexpectedly")
	}
}
