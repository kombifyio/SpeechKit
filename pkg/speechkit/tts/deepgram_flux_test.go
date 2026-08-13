package tts

import (
	"encoding/json"
	"net/url"
	"testing"
)

func TestDeepgramFluxTTSEndpoint(t *testing.T) {
	t.Parallel()
	d := NewDeepgramFluxTTS("test")

	raw, err := d.endpoint("flux-haley-en", 24000, 1.07)
	if err != nil {
		t.Fatalf("build endpoint: %v", err)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse endpoint: %v", err)
	}
	if u.Scheme != "wss" || u.Path != "/v2/speak" {
		t.Fatalf("endpoint = %s, want wss .../v2/speak", raw)
	}
	q := u.Query()
	if q.Get("model") != "flux-haley-en" {
		t.Errorf("model = %q", q.Get("model"))
	}
	// The streaming leg is raw audio only; a container would be rejected.
	if q.Get("encoding") != "linear16" || q.Get("sample_rate") != "24000" {
		t.Errorf("audio params = %v", q)
	}
	if q.Has("container") {
		t.Error("the streaming leg takes no container")
	}
	if q.Get("speed") != "1.05" {
		t.Errorf("speed = %q, want the nearest accepted step 1.05", q.Get("speed"))
	}

	// An unset speed is omitted rather than sent as zero.
	raw, err = d.endpoint("flux-kit-en", 24000, 0)
	if err != nil {
		t.Fatalf("build endpoint: %v", err)
	}
	u, _ = url.Parse(raw)
	if u.Query().Has("speed") {
		t.Error("unset speed must be omitted")
	}
}

func TestSnapDeepgramFluxSpeed(t *testing.T) {
	t.Parallel()
	cases := map[float64]float64{
		0:    0,    // unset
		-1:   0,    // unset
		1.0:  1.0,  // exact
		1.07: 1.05, // between steps, rounds down
		1.13: 1.15, // between steps, rounds up
		3.0:  1.15, // above range, clamps
		0.1:  0.85, // below range, clamps
	}
	for in, want := range cases {
		if got := SnapDeepgramFluxSpeed(in); got != want {
			t.Errorf("SnapDeepgramFluxSpeed(%v) = %v, want %v", in, got, want)
		}
	}
}

// The control-frame shapes below are verbatim captures from a live
// flux-kit-en session (tools/fluxprobe -speak); the docs do not publish them.
func TestDeepgramFluxTTSEventDecoding(t *testing.T) {
	t.Parallel()
	const metadata = `{"type":"SpeechMetadata","speech_id":"dg_sp_7d3c1b25c1c7","audio_duration_ms":1360,` +
		`"input_character_count":21,"billable_character_count":21,` +
		`"controls_applied":{"pronunciations_applied":0,"breaks_applied":0,"pronunciation_warnings":0}}`

	var event deepgramFluxTTSEvent
	if err := json.Unmarshal([]byte(metadata), &event); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	decoded := event.event()
	if decoded.Type != FluxSpeechMetadata {
		t.Errorf("type = %q", decoded.Type)
	}
	if decoded.SpeechID != "dg_sp_7d3c1b25c1c7" {
		t.Errorf("speech_id = %q", decoded.SpeechID)
	}
	if decoded.AudioDurationMs != 1360 {
		t.Errorf("audio_duration_ms = %d", decoded.AudioDurationMs)
	}
	if decoded.BillableCharacterCount != 21 || decoded.InputCharacterCount != 21 {
		t.Errorf("character counts = %d/%d", decoded.InputCharacterCount, decoded.BillableCharacterCount)
	}
	if decoded.IsAudio() {
		t.Error("a control frame carries no audio")
	}

	// Interrupt is what makes barge-in bookkeeping honest: it reports the text
	// the listener actually heard, so the transcript can be truncated to it.
	const interrupt = `{"type":"Interrupt","speech_id":"dg_sp_1","text_spoken":"Here is the","text_remaining":" rest of the answer."}`
	if err := json.Unmarshal([]byte(interrupt), &event); err != nil {
		t.Fatalf("decode interrupt: %v", err)
	}
	decoded = event.event()
	if decoded.TextSpoken != "Here is the" || decoded.TextRemaining != " rest of the answer." {
		t.Errorf("interrupt texts = %q / %q", decoded.TextSpoken, decoded.TextRemaining)
	}
}

func TestDeepgramFluxTTSErrorFrame(t *testing.T) {
	t.Parallel()
	var event deepgramFluxTTSEvent
	if err := json.Unmarshal([]byte(`{"type":"Fatal","description":"unknown model"}`), &event); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if err := event.err(); err == nil {
		t.Fatal("a Fatal frame must decode as an error")
	}
	if err := json.Unmarshal([]byte(`{"type":"SpeechStarted"}`), &event); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if err := event.err(); err != nil {
		t.Fatalf("a normal frame must not be an error: %v", err)
	}
}

func TestIsDeepgramFluxVoice(t *testing.T) {
	t.Parallel()
	for voice, want := range map[string]bool{
		"flux-kit-en":      true,
		"FLUX-HALEY-EN":    true,
		"aura-2-thalia-en": false,
		"aura-asteria-en":  false,
		"":                 false,
		"fluxus-something": false,
	} {
		if got := IsDeepgramFluxVoice(voice); got != want {
			t.Errorf("IsDeepgramFluxVoice(%q) = %v, want %v", voice, got, want)
		}
	}
}
