package tts

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// xAI rejects a request without a voice and a language, takes speed only
// between 0.7 and 1.5, and has no Opus. The locale maps to the closest
// documented code (or auto), the voice and codec travel as xAI names them, and
// a format xAI cannot produce is reported as the mp3 it fell back to.
func TestXAISynthesizeMapsLocaleVoiceSpeedAndCodec(t *testing.T) {
	var bodies []xaiTTSRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/tts" || r.Header.Get("Authorization") != "Bearer xai-test" {
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		var body xaiTTSRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		bodies = append(bodies, body)
		_, _ = w.Write([]byte("audio-bytes"))
	}))
	defer server.Close()

	p := NewXAI(XAIOpts{APIKey: "xai-test"})
	p.BaseURL = server.URL
	p.Validation.AllowLoopback, p.Validation.AllowHTTP, p.Validation.AllowPrivate = true, true, true

	pcm, err := p.Synthesize(context.Background(), "Hallo Welt", SynthesizeOpts{Locale: "de-DE", Voice: "rex", Speed: 3, Format: "pcm"})
	if err != nil {
		t.Fatalf("Synthesize(pcm): %v", err)
	}
	fallback, err := p.Synthesize(context.Background(), "Hello", SynthesizeOpts{Locale: "sw", Format: "opus"})
	if err != nil {
		t.Fatalf("Synthesize(opus): %v", err)
	}
	if len(bodies) != 2 {
		t.Fatalf("server saw %d requests, want 2", len(bodies))
	}

	first := bodies[0]
	if first.VoiceID != "rex" || first.Language != "de" || first.Speed != xaiTTSMaxSpeed ||
		first.OutputFormat.Codec != "pcm" || first.OutputFormat.SampleRate != xaiTTSSampleRate || first.OutputFormat.BitRate != 0 {
		t.Errorf("pcm request = %+v", first)
	}
	if pcm.Format != "pcm" || pcm.SampleRate != xaiTTSSampleRate || pcm.Voice != "rex" || pcm.Provider != "xai" || string(pcm.Audio) != "audio-bytes" {
		t.Errorf("pcm result = %+v", pcm)
	}

	second := bodies[1]
	if second.VoiceID != xaiTTSDefaultVoice || second.Language != "auto" || second.OutputFormat.Codec != "mp3" || second.OutputFormat.BitRate != xaiTTSMP3BitRate {
		t.Errorf("fallback request = %+v", second)
	}
	if fallback.Format != "mp3" {
		t.Errorf("an opus request must report the mp3 it fell back to, got %q", fallback.Format)
	}
}
