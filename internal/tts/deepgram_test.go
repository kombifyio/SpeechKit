package tts

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/netsec"
)

var deepgramTestValidation = netsec.ValidationOptions{AllowLoopback: true, AllowHTTP: true}

func newTestDeepgramTTS(baseURL, model string) *Deepgram {
	d := NewDeepgram(DeepgramOpts{APIKey: "test-key", Model: model})
	d.BaseURL = baseURL
	d.Validation = deepgramTestValidation
	return d
}

func TestDeepgramSynthesizeMP3(t *testing.T) {
	fakeAudio := []byte("fake-aura-mp3")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Token test-key" {
			t.Errorf("auth header = %q, want %q", got, "Token test-key")
		}
		if got := r.URL.Query().Get("model"); got != "aura-2-thalia-en" {
			t.Errorf("model = %q, want aura-2-thalia-en", got)
		}
		if got := r.URL.Query().Get("encoding"); got != "mp3" {
			t.Errorf("encoding = %q, want mp3", got)
		}
		var body struct {
			Text string `json:"text"`
		}
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.Text != "Hello there" {
			t.Errorf("text = %q, want 'Hello there'", body.Text)
		}
		_, _ = w.Write(fakeAudio)
	}))
	defer srv.Close()

	d := newTestDeepgramTTS(srv.URL, "aura-2-thalia-en")
	result, err := d.Synthesize(context.Background(), "Hello there", SynthesizeOpts{Format: "mp3"})
	if err != nil {
		t.Fatalf("synthesize: %v", err)
	}
	if !bytes.Equal(result.Audio, fakeAudio) {
		t.Errorf("audio mismatch: got %d bytes", len(result.Audio))
	}
	if result.Format != "mp3" {
		t.Errorf("format = %q, want mp3", result.Format)
	}
	if result.Provider != "deepgram" {
		t.Errorf("provider = %q, want deepgram", result.Provider)
	}
	if result.Voice != "aura-2-thalia-en" {
		t.Errorf("voice = %q, want aura-2-thalia-en", result.Voice)
	}
}

func TestDeepgramSynthesizeWAVParams(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("encoding") != "linear16" {
			t.Errorf("encoding = %q, want linear16", q.Get("encoding"))
		}
		if q.Get("container") != "wav" {
			t.Errorf("container = %q, want wav", q.Get("container"))
		}
		if q.Get("sample_rate") != "24000" {
			t.Errorf("sample_rate = %q, want 24000", q.Get("sample_rate"))
		}
		_, _ = w.Write([]byte("wav-bytes"))
	}))
	defer srv.Close()

	d := newTestDeepgramTTS(srv.URL, "aura-2-thalia-en")
	result, err := d.Synthesize(context.Background(), "hi", SynthesizeOpts{Format: "wav"})
	if err != nil {
		t.Fatalf("synthesize: %v", err)
	}
	if result.Format != "wav" {
		t.Errorf("format = %q, want wav", result.Format)
	}
	if result.SampleRate != 24000 {
		t.Errorf("sample_rate = %d, want 24000", result.SampleRate)
	}
}

func TestDeepgramLocaleDefaultsGermanVoice(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("model"); got != "aura-2-viktoria-de" {
			t.Errorf("model = %q, want aura-2-viktoria-de (German locale default)", got)
		}
		_, _ = w.Write([]byte("de-audio"))
	}))
	defer srv.Close()

	// No configured model => locale drives the voice choice.
	d := newTestDeepgramTTS(srv.URL, "")
	if _, err := d.Synthesize(context.Background(), "Hallo", SynthesizeOpts{Locale: "de-DE"}); err != nil {
		t.Fatalf("synthesize: %v", err)
	}
}

func TestDeepgramVoiceOverride(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("model"); got != "aura-2-apollo-en" {
			t.Errorf("model = %q, want aura-2-apollo-en (per-request override)", got)
		}
		_, _ = w.Write([]byte("x"))
	}))
	defer srv.Close()

	d := newTestDeepgramTTS(srv.URL, "aura-2-thalia-en")
	if _, err := d.Synthesize(context.Background(), "hi", SynthesizeOpts{Voice: "aura-2-apollo-en"}); err != nil {
		t.Fatalf("synthesize: %v", err)
	}
}

func TestDeepgramChunksLongText(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		_, _ = w.Write([]byte("part"))
	}))
	defer srv.Close()

	long := strings.Repeat("Dies ist ein Satz. ", 300) // ~5700 chars > 1900 cap
	d := newTestDeepgramTTS(srv.URL, "aura-2-thalia-en")
	result, err := d.Synthesize(context.Background(), long, SynthesizeOpts{Format: "mp3"})
	if err != nil {
		t.Fatalf("synthesize: %v", err)
	}
	if atomic.LoadInt32(&calls) < 2 {
		t.Errorf("expected the long text to be chunked into >=2 requests, got %d", calls)
	}
	if !bytes.HasPrefix(result.Audio, []byte("part")) || len(result.Audio) < 8 {
		t.Errorf("expected concatenated audio from multiple chunks, got %d bytes", len(result.Audio))
	}
}

func TestDeepgramMultiChunkWAVSingleHeader(t *testing.T) {
	// Each chunk returns fake linear16 PCM. The handler asserts the multi-chunk
	// WAV path fetches PCM (container=none), not a per-chunk WAV container.
	const pcmPerChunk = 100
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		q := r.URL.Query()
		if q.Get("container") != "none" || q.Get("encoding") != "linear16" {
			t.Errorf("multi-chunk WAV must fetch PCM: container=%q encoding=%q", q.Get("container"), q.Get("encoding"))
		}
		_, _ = w.Write(bytes.Repeat([]byte{0x01}, pcmPerChunk))
	}))
	defer srv.Close()

	long := strings.Repeat("Dies ist ein Satz. ", 300) // > 1900 chars => multiple chunks
	d := newTestDeepgramTTS(srv.URL, "aura-2-thalia-en")
	result, err := d.Synthesize(context.Background(), long, SynthesizeOpts{Format: "wav"})
	if err != nil {
		t.Fatalf("synthesize: %v", err)
	}
	n := int(atomic.LoadInt32(&calls))
	if n < 2 {
		t.Fatalf("expected multi-chunk synthesis, got %d calls", n)
	}
	if result.Format != "wav" {
		t.Errorf("format = %q, want wav", result.Format)
	}

	// Exactly one RIFF/WAVE header regardless of chunk count — the bug was
	// stacked headers from concatenating per-chunk WAV responses.
	if got := bytes.Count(result.Audio, []byte("RIFF")); got != 1 {
		t.Errorf("expected exactly 1 RIFF header, got %d", got)
	}
	if got := bytes.Count(result.Audio, []byte("WAVE")); got != 1 {
		t.Errorf("expected exactly 1 WAVE marker, got %d", got)
	}
	if !bytes.HasPrefix(result.Audio, []byte("RIFF")) {
		t.Fatalf("WAV must start with RIFF, got %x", result.Audio[:4])
	}

	// Header sizes must match the payload: data == n*pcmPerChunk, RIFF == 36+data.
	wantData := n * pcmPerChunk
	if len(result.Audio) != 44+wantData {
		t.Fatalf("total WAV length = %d, want %d (44 header + %d PCM)", len(result.Audio), 44+wantData, wantData)
	}
	if gotData := int(binary.LittleEndian.Uint32(result.Audio[40:44])); gotData != wantData {
		t.Errorf("data chunk size = %d, want %d", gotData, wantData)
	}
	if gotRIFF := int(binary.LittleEndian.Uint32(result.Audio[4:8])); gotRIFF != 36+wantData {
		t.Errorf("RIFF size = %d, want %d", gotRIFF, 36+wantData)
	}
	// Format chunk: PCM (1), mono (1), 24 kHz, 16-bit.
	if af := binary.LittleEndian.Uint16(result.Audio[20:22]); af != 1 {
		t.Errorf("audio format = %d, want 1 (PCM)", af)
	}
	if ch := binary.LittleEndian.Uint16(result.Audio[22:24]); ch != 1 {
		t.Errorf("channels = %d, want 1", ch)
	}
	if sr := binary.LittleEndian.Uint32(result.Audio[24:28]); sr != deepgramTTSSampleRate {
		t.Errorf("sample rate = %d, want %d", sr, deepgramTTSSampleRate)
	}
	if bps := binary.LittleEndian.Uint16(result.Audio[34:36]); bps != 16 {
		t.Errorf("bits per sample = %d, want 16", bps)
	}
}

func TestSplitForDeepgramTTS(t *testing.T) {
	if got := splitForDeepgramTTS("short", 100); len(got) != 1 || got[0] != "short" {
		t.Errorf("short text should be a single chunk, got %v", got)
	}
	chunks := splitForDeepgramTTS("Sentence one. Sentence two. Sentence three.", 20)
	if len(chunks) < 2 {
		t.Errorf("expected multiple chunks, got %v", chunks)
	}
	for _, c := range chunks {
		if len(c) > 20 {
			t.Errorf("chunk %q exceeds max length 20", c)
		}
	}
}
