package dictation

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
)

// loadStreamFixture returns the named frame from the golden contract file as
// a generic JSON value.
func loadStreamFixture(t *testing.T, frame string) map[string]any {
	t.Helper()
	path := filepath.Join("..", "..", "..", "docs", "server", "fixtures", "dictation-stream.v1.json")
	raw, err := os.ReadFile(path) // #nosec G304 -- fixed repo-relative test fixture path.
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var doc struct {
		Frames map[string]json.RawMessage `json:"frames"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	entry, ok := doc.Frames[frame]
	if !ok {
		t.Fatalf("fixture has no frame %q", frame)
	}
	var out map[string]any
	if err := json.Unmarshal(entry, &out); err != nil {
		t.Fatalf("parse fixture frame %q: %v", frame, err)
	}
	return out
}

// assertWireEqual marshals v and compares the generic JSON value with the
// golden fixture frame — a byte-order-insensitive producer drift check.
func assertWireEqual(t *testing.T, frame string, v any) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal %q: %v", frame, err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("re-parse %q: %v", frame, err)
	}
	want := loadStreamFixture(t, frame)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("wire drift on %q:\n got: %s\nwant fixture frame", frame, data)
	}
}

func boolPtr(b bool) *bool { return &b }

func TestStreamProtocol_GoldenFixtures(t *testing.T) {
	assertWireEqual(t, "start", StreamStartFrame{
		Type:              StreamMsgStart,
		Language:          "de",
		Model:             "nova-3",
		ProviderProfileID: "stt.deepgram.nova-3",
		InterimResults:    boolPtr(true),
		EndpointingMs:     800,
		Keyterms:          []string{"Kombify", "SpeechKit"},
		Format: &StreamAudioFormat{
			Encoding:     "linear16",
			SampleRateHz: 16000,
			Channels:     1,
		},
	})
	assertWireEqual(t, "ready", StreamReadyFrame{Type: StreamMsgReady, SegmentID: 1})
	assertWireEqual(t, "transcript_draft", StreamTranscriptFrame{
		Type:      StreamMsgTranscript,
		SegmentID: 1,
		Sequence:  3,
		Text:      "hallo wel",
		Done:      false,
		Provider:  "deepgram",
		Model:     "nova-3",
	})
	assertWireEqual(t, "transcript_final", StreamTranscriptFrame{
		Type:       StreamMsgTranscript,
		SegmentID:  1,
		Sequence:   4,
		Text:       "Hallo Welt.",
		Done:       true,
		Language:   "de",
		Confidence: 0.94,
		Provider:   "deepgram",
		Model:      "nova-3",
		Words: []StreamWord{
			{Text: "Hallo", Confidence: 0.97, StartMs: 120, EndMs: 480},
			{Text: "Welt.", Confidence: 0.91, StartMs: 520, EndMs: 900},
		},
	})
	assertWireEqual(t, "segment_done", StreamSegmentDoneFrame{Type: StreamMsgSegmentDone, SegmentID: 1})
	assertWireEqual(t, "error", StreamErrorFrame{
		Type:    StreamMsgError,
		Code:    StreamErrStreamingUnavailable,
		Message: "no streaming-capable STT provider is configured; fall back to POST /v1/dictation/transcribe",
	})
	assertWireEqual(t, "session_end", StreamSessionEndFrame{Type: StreamMsgSessionEnd, Reason: StreamEndReasonClient})
	assertWireEqual(t, "pong", StreamPongFrame{Type: StreamMsgPong})
}

func TestStreamStartFrame_DecodesClientFixtures(t *testing.T) {
	// Consumer direction: the client-authored fixture frames must decode into
	// the Go structs with the documented defaults.
	raw, err := json.Marshal(loadStreamFixture(t, "start"))
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	var start StreamStartFrame
	if err := json.Unmarshal(raw, &start); err != nil {
		t.Fatalf("decode start: %v", err)
	}
	opts := start.Options()
	if !opts.InterimResults || opts.Language != "de" || opts.Model != "nova-3" ||
		opts.ProviderProfileID != "stt.deepgram.nova-3" || opts.EndpointingMs != 800 {
		t.Fatalf("unexpected options: %+v", opts)
	}
	format := start.AudioFormat()
	if format.Encoding != speaker.AudioEncodingLinear16 || format.SampleRateHz != 16000 || format.Channels != 1 {
		t.Fatalf("unexpected format: %+v", format)
	}
}

func TestStreamStartFrame_Defaults(t *testing.T) {
	var start StreamStartFrame
	if err := json.Unmarshal([]byte(`{"type":"start"}`), &start); err != nil {
		t.Fatalf("decode minimal start: %v", err)
	}
	opts := start.Options()
	if !opts.InterimResults {
		t.Fatal("interim_results must default to true when omitted")
	}
	format := start.AudioFormat()
	if format.Encoding != speaker.AudioEncodingLinear16 || format.SampleRateHz != 16000 || format.Channels != 1 {
		t.Fatalf("format defaults wrong: %+v", format)
	}
	if ok, _ := start.ValidFormat(); !ok {
		t.Fatal("nil format must be valid")
	}
}

func TestStreamStartFrame_ValidFormat(t *testing.T) {
	tests := []struct {
		name   string
		format StreamAudioFormat
		wantOK bool
	}{
		{"linear16 16k mono", StreamAudioFormat{Encoding: "linear16", SampleRateHz: 16000, Channels: 1}, true},
		{"pcm16 alias", StreamAudioFormat{Encoding: "pcm16"}, true},
		{"zero values", StreamAudioFormat{}, true},
		{"bad encoding", StreamAudioFormat{Encoding: "opus"}, false},
		{"rate too low", StreamAudioFormat{SampleRateHz: 4000}, false},
		{"rate too high", StreamAudioFormat{SampleRateHz: 96000}, false},
		{"too many channels", StreamAudioFormat{Channels: 3}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start := StreamStartFrame{Type: StreamMsgStart, Format: &tt.format}
			ok, reason := start.ValidFormat()
			if ok != tt.wantOK {
				t.Fatalf("ValidFormat = (%v, %q), want ok=%v", ok, reason, tt.wantOK)
			}
		})
	}
}

func TestStreamTranscriptFromEvent(t *testing.T) {
	event := speechkit.DictationStreamEvent{
		Sequence:   7,
		Text:       "guten morgen",
		IsFinal:    true,
		Language:   "de",
		Provider:   "deepgram",
		Model:      "nova-3",
		Confidence: 0.9,
		Words:      []speechkit.WordConfidence{{Text: "guten", Confidence: 0.95, StartMs: 10, EndMs: 300}},
	}
	frame := streamTranscriptFromEvent(2, event)
	if frame.Type != StreamMsgTranscript || frame.SegmentID != 2 || !frame.Done ||
		frame.Text != "guten morgen" || frame.Sequence != 7 {
		t.Fatalf("unexpected frame: %+v", frame)
	}
	if len(frame.Words) != 1 || frame.Words[0].Text != "guten" || frame.Words[0].EndMs != 300 {
		t.Fatalf("unexpected words: %+v", frame.Words)
	}
}
