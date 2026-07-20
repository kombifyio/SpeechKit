package stt

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	audiofmt "github.com/kombifyio/SpeechKit/pkg/speechkit/audio"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
)

// syncTestClip returns a WAV clip of the given duration (16 kHz S16 mono).
func syncTestClip(t *testing.T, d time.Duration) []byte {
	t.Helper()
	samples := int(d.Seconds() * 16000)
	return audiofmt.PCMToWAV(make([]byte, samples*2))
}

func newSyncTestProvider(syncURL, asyncURL string) *AssemblyAIProvider {
	p := NewAssemblyAIProvider("assembly-test-key", "")
	p.SyncBaseURL = syncURL
	p.BaseURL = asyncURL
	p.Validation = testValidation
	p.PollInterval = time.Millisecond
	p.PollTimeout = time.Second
	p.client.Timeout = 5 * time.Second
	return p
}

func TestAssemblyAI_SyncTranscribe_SendsPromptContextAndKeyterms(t *testing.T) {
	var gotModelHeader, gotAuth string
	var gotConfig assemblyAISyncConfig
	var gotAudioType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/transcribe" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		gotModelHeader = r.Header.Get("X-AAI-Model")
		gotAuth = r.Header.Get("Authorization")
		if err := r.ParseMultipartForm(64 << 20); err != nil {
			t.Fatalf("parse multipart: %v", err)
		}
		if err := json.Unmarshal([]byte(r.FormValue("config")), &gotConfig); err != nil {
			t.Fatalf("parse config part: %v", err)
		}
		file, header, err := r.FormFile("audio")
		if err != nil {
			t.Fatalf("audio part: %v", err)
		}
		defer file.Close() //nolint:errcheck // test cleanup
		gotAudioType = header.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"text": "termin morgen um zehn",
			"confidence": 0.93,
			"audio_duration_ms": 2500,
			"session_id": "sess-1",
			"words": [
				{"text": "termin", "confidence": 0.95, "start": 100, "end": 480}
			]
		}`))
	}))
	defer server.Close()

	p := newSyncTestProvider(server.URL, server.URL)
	result, err := p.Transcribe(context.Background(), syncTestClip(t, 2*time.Second), TranscribeOpts{
		Language: "de",
		Prompt:   "Homelab development context; speaker is dictating short commands.",
		Keyterms: []string{"Kombify", "SpeechKit"},
		ConversationContext: []string{
			"Timer für die Kaffeemaschine ist gestellt.",
			"Was steht morgen an?",
		},
	})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if gotModelHeader != "universal-3-5-pro" {
		t.Fatalf("X-AAI-Model = %q", gotModelHeader)
	}
	if gotAuth != "Bearer assembly-test-key" {
		t.Fatalf("Authorization = %q, want sync Bearer scheme", gotAuth)
	}
	if gotAudioType != "audio/wav" {
		t.Fatalf("audio part content type = %q", gotAudioType)
	}
	if !strings.Contains(gotConfig.Prompt, "Homelab development context") {
		t.Fatalf("config.prompt = %q", gotConfig.Prompt)
	}
	if len(gotConfig.KeytermsPrompt) != 2 || gotConfig.KeytermsPrompt[0] != "Kombify" {
		t.Fatalf("config.keyterms_prompt = %v", gotConfig.KeytermsPrompt)
	}
	if len(gotConfig.ConversationContext) != 2 || !strings.Contains(gotConfig.ConversationContext[1], "morgen") {
		t.Fatalf("config.conversation_context = %v", gotConfig.ConversationContext)
	}
	if !gotConfig.Timestamps {
		t.Fatal("config.timestamps must be requested for word timings")
	}
	if result.Text != "termin morgen um zehn" || result.Provider != "assemblyai" || result.Model != "universal-3-5-pro" {
		t.Fatalf("result = %+v", result)
	}
	if result.Confidence != 0.93 || len(result.Words) != 1 || result.Words[0].StartMs != 100 {
		t.Fatalf("result words/confidence = %+v", result)
	}
}

func TestAssemblyAI_SyncFailureFallsBackToAsync(t *testing.T) {
	syncCalls := 0
	syncServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		syncCalls++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer syncServer.Close()

	asyncServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/v2/upload"):
			_, _ = w.Write([]byte(`{"upload_url": "https://cdn.example/upload/1"}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/v2/transcript"):
			_, _ = w.Write([]byte(`{"id": "tr-1", "status": "queued"}`))
		default:
			_, _ = w.Write([]byte(`{"id": "tr-1", "status": "completed", "text": "async fallback", "confidence": 0.8}`))
		}
	}))
	defer asyncServer.Close()

	p := newSyncTestProvider(syncServer.URL, asyncServer.URL)
	result, err := p.Transcribe(context.Background(), syncTestClip(t, 2*time.Second), TranscribeOpts{Language: "de"})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if syncCalls != 1 {
		t.Fatalf("sync calls = %d, want 1", syncCalls)
	}
	if result.Text != "async fallback" {
		t.Fatalf("result = %+v, want async fallback", result)
	}
}

func TestAssemblyAI_SyncSkippedForAsyncOnlyFeatures(t *testing.T) {
	syncCalls := 0
	syncServer := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		syncCalls++
	}))
	defer syncServer.Close()
	asyncServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/v2/upload"):
			_, _ = w.Write([]byte(`{"upload_url": "https://cdn.example/upload/1"}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/v2/transcript"):
			_, _ = w.Write([]byte(`{"id": "tr-1", "status": "queued"}`))
		default:
			_, _ = w.Write([]byte(`{"id": "tr-1", "status": "completed", "text": "diarized", "confidence": 0.8}`))
		}
	}))
	defer asyncServer.Close()

	p := newSyncTestProvider(syncServer.URL, asyncServer.URL)
	_, err := p.Transcribe(context.Background(), syncTestClip(t, 2*time.Second), TranscribeOpts{
		Language: "de",
		Speaker:  speaker.Options{Diarization: true},
	})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if syncCalls != 0 {
		t.Fatalf("sync must not be called for diarization requests; calls = %d", syncCalls)
	}
}

func TestAssemblyAI_SyncSkippedWhenDisabledOrTooLong(t *testing.T) {
	p := newSyncTestProvider("https://sync.invalid", "https://api.invalid")

	resolved := ResolvedTranscribeOptions{Language: "de"}
	if p.syncEligible(syncTestClip(t, 2*time.Second), TranscribeOpts{}, resolved) != true {
		t.Fatal("short clip should be sync-eligible")
	}
	if p.syncEligible(syncTestClip(t, 2*time.Second), TranscribeOpts{}, ResolvedTranscribeOptions{}) {
		t.Fatal("auto/unset language must keep the async flow (sync has no language_detection)")
	}
	if p.syncEligible(syncTestClip(t, 130*time.Second), TranscribeOpts{}, resolved) {
		t.Fatal("clips over 120s must not be sync-eligible")
	}
	if p.syncEligible(syncTestClip(t, 30*time.Millisecond), TranscribeOpts{}, resolved) {
		t.Fatal("clips under 80ms must not be sync-eligible")
	}
	p.DisableSync = true
	if p.syncEligible(syncTestClip(t, 2*time.Second), TranscribeOpts{}, resolved) {
		t.Fatal("DisableSync must force the async flow")
	}
	p.DisableSync = false
	if p.syncEligible(syncTestClip(t, 2*time.Second), TranscribeOpts{Model: "universal-2"}, resolved) {
		t.Fatal("explicit non-flagship model pins must keep the async flow")
	}
}

func TestAssemblyAI_Warm(t *testing.T) {
	var gotModel string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/warm" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		gotModel = r.Header.Get("X-AAI-Model")
		_, _ = w.Write([]byte(`{"warm":"toasty"}`))
	}))
	defer server.Close()

	p := newSyncTestProvider(server.URL, server.URL)
	if err := p.Warm(context.Background()); err != nil {
		t.Fatalf("Warm: %v", err)
	}
	if gotModel != "universal-3-5-pro" {
		t.Fatalf("X-AAI-Model = %q", gotModel)
	}
}

func TestWavDuration(t *testing.T) {
	d, ok := wavDuration(syncTestClip(t, 3*time.Second))
	if !ok {
		t.Fatal("wavDuration should parse the generated WAV")
	}
	if d < 2900*time.Millisecond || d > 3100*time.Millisecond {
		t.Fatalf("duration = %v, want ~3s", d)
	}
	if _, ok := wavDuration([]byte("not a wav")); ok {
		t.Fatal("garbage must not parse")
	}
}

func TestCapConversationContext(t *testing.T) {
	turns := capConversationContext([]string{" one ", "", "two", "three"}, 2, 0)
	if len(turns) != 2 || turns[0] != "two" || turns[1] != "three" {
		t.Fatalf("turn cap keeps most recent: %v", turns)
	}
	turns = capConversationContext([]string{"aaaa", "bbbb", "cccc"}, 0, 8)
	if len(turns) != 2 || turns[0] != "bbbb" {
		t.Fatalf("char cap keeps most recent: %v", turns)
	}
}
