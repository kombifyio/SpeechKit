package stt

import (
	"bufio"
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
)

// The fixture holds frames captured from a live flux-general-multi session
// (tools/fluxprobe) so the decoder is tested against the wire, not against a
// hand-written guess at the schema.
const fluxFixture = "testdata/flux/turn-events.jsonl"

func decodeFluxFixture(t *testing.T) []deepgramFluxEvent {
	t.Helper()
	file, err := os.Open(fluxFixture)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer func() { _ = file.Close() }()

	var events []deepgramFluxEvent
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event deepgramFluxEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("decode fixture line: %v", err)
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan fixture: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("fixture is empty")
	}
	return events
}

func TestFluxDecoderAgainstCapturedFrames(t *testing.T) {
	t.Parallel()
	events := decodeFluxFixture(t)

	var kinds []string
	var final FluxTurn
	var eager FluxTurn
	for _, event := range events {
		if err := event.err(); err != nil {
			t.Fatalf("captured frame decoded as an error: %v", err)
		}
		if event.Type != "TurnInfo" || event.Event == "" {
			// The Connected handshake carries no turn.
			continue
		}
		turn := event.turn(42)
		kinds = append(kinds, turn.Event)
		if turn.IsFinal() {
			final = turn
		}
		if turn.IsSpeculative() {
			eager = turn
		}
		for _, word := range turn.Words {
			if word.StartMs < 0 || word.EndMs < 0 {
				t.Errorf("negative word timing in %s: %+v", turn.Event, word)
			}
		}
	}

	wantKinds := []string{FluxEventStartOfTurn, FluxEventUpdate, FluxEventUpdate, FluxEventEagerEndOfTurn, FluxEventEndOfTurn}
	if strings.Join(kinds, ",") != strings.Join(wantKinds, ",") {
		t.Fatalf("event kinds = %v, want %v", kinds, wantKinds)
	}
	if final.Event == "" {
		t.Fatal("no EndOfTurn decoded")
	}
	if !strings.HasPrefix(final.Transcript, "Guten Tag, ich bin der erste Sprecher") {
		t.Errorf("final transcript = %q", final.Transcript)
	}
	if len(final.Languages) != 1 || final.Languages[0] != "de" {
		t.Errorf("final languages = %v, want [de]", final.Languages)
	}
	if len(final.Words) == 0 {
		t.Error("final turn carries no words")
	}
	if final.AudioWindowEndMs <= final.AudioWindowStartMs {
		t.Errorf("audio window = %d..%d", final.AudioWindowStartMs, final.AudioWindowEndMs)
	}
	if final.LatencyMs != 42 {
		t.Errorf("latency not carried through: %d", final.LatencyMs)
	}
	// The eager signal precedes the close and is retractable, so it must not
	// look final to a consumer.
	if eager.IsFinal() {
		t.Error("EagerEndOfTurn must not report IsFinal")
	}
	if eager.EndOfTurnConfidence <= 0 {
		t.Errorf("eager end-of-turn confidence = %v", eager.EndOfTurnConfidence)
	}
}

func TestFluxEndpointQuery(t *testing.T) {
	t.Parallel()
	p := &DeepgramProvider{APIKey: "test"}
	format := speaker.AudioFormat{Encoding: speaker.AudioEncodingLinear16, SampleRateHz: 16000, Channels: 1}

	raw, err := p.deepgramFluxEndpoint(DeepgramFluxModelMulti, FluxStreamOptions{
		LanguageHints:     []string{"de-DE", "en", "de", " "},
		Keyterms:          []string{"kombify", "kombify", "SpeechKit"},
		EOTThreshold:      0.95, // above range: must clamp
		EagerEOTThreshold: 0.4,
		EOTTimeoutMs:      100, // below range: must clamp
	}, format)
	if err != nil {
		t.Fatalf("build endpoint: %v", err)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse endpoint: %v", err)
	}
	if u.Scheme != "wss" || u.Path != "/v2/listen" {
		t.Fatalf("endpoint = %s, want wss .../v2/listen", raw)
	}
	q := u.Query()
	if got := q["language_hint"]; strings.Join(got, ",") != "de,en" {
		t.Errorf("language_hint = %v, want [de en] (deduped to base subtags)", got)
	}
	if got := q["keyterm"]; strings.Join(got, ",") != "kombify,SpeechKit" {
		t.Errorf("keyterm = %v", got)
	}
	if q.Get("eot_threshold") != "0.9" {
		t.Errorf("eot_threshold = %q, want the clamped 0.9", q.Get("eot_threshold"))
	}
	if q.Get("eot_timeout_ms") != "500" {
		t.Errorf("eot_timeout_ms = %q, want the clamped 500", q.Get("eot_timeout_ms"))
	}
	if q.Get("eager_eot_threshold") != "0.4" {
		t.Errorf("eager_eot_threshold = %q", q.Get("eager_eot_threshold"))
	}
	if q.Has("language") {
		t.Error("Flux takes no language parameter")
	}

	// The English model is pinned by name and rejects hints.
	raw, err = p.deepgramFluxEndpoint(DeepgramFluxModelEN, FluxStreamOptions{LanguageHints: []string{"de"}}, format)
	if err != nil {
		t.Fatalf("build english endpoint: %v", err)
	}
	u, _ = url.Parse(raw)
	if u.Query().Has("language_hint") {
		t.Error("flux-general-en must not carry language hints")
	}
	for _, key := range []string{"eot_threshold", "eager_eot_threshold", "eot_timeout_ms"} {
		if u.Query().Has(key) {
			t.Errorf("unset tuning must be omitted, %s present", key)
		}
	}
}

// TurnResumed retracts an earlier EagerEndOfTurn, so a consumer that started
// speculative work on the eager signal must see it as neither final nor
// speculative and cancel that work. Observed live (fluxprobe, flux-general-multi:
// EagerEndOfTurn eot_conf=0.5503 -> TurnResumed eot_conf=0.0018 -> EndOfTurn).
func TestFluxTurnResumedSemantics(t *testing.T) {
	t.Parallel()
	resumed := deepgramFluxEvent{Type: "TurnInfo", Event: FluxEventTurnResumed, Transcript: "Guten Tag, ich"}.turn(0)
	if resumed.IsFinal() {
		t.Error("TurnResumed must not report IsFinal")
	}
	if resumed.IsSpeculative() {
		t.Error("TurnResumed retracts the speculative signal; it is not itself speculative")
	}
	if resumed.Transcript != "Guten Tag, ich" {
		t.Errorf("transcript = %q", resumed.Transcript)
	}
}

func TestFluxErrorFrame(t *testing.T) {
	t.Parallel()
	var event deepgramFluxEvent
	if err := json.Unmarshal([]byte(`{"type":"Error","description":"invalid model"}`), &event); err != nil {
		t.Fatalf("decode: %v", err)
	}
	err := event.err()
	if err == nil || !strings.Contains(err.Error(), "invalid model") {
		t.Fatalf("error frame = %v", err)
	}
}
