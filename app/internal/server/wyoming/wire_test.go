package wyoming

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestRoundTripEventShapes(t *testing.T) {
	pcm := []byte{0x01, 0x02, 0x03, 0x04, 0x05}
	chunk, err := audioChunkEvent(16000, 2, 1, pcm)
	if err != nil {
		t.Fatalf("build chunk: %v", err)
	}
	transcribe, _ := eventWithData(TypeTranscribe, Transcribe{Language: "de"})
	transcript, _ := transcriptEvent("hallo welt")

	events := []*Event{
		{Type: TypeDescribe}, // no data, no payload
		transcribe,           // data only
		{Type: TypeAudioStart, Data: mustJSON(t, AudioStart{Rate: 16000, Width: 2, Channels: 1})},
		chunk,                 // data + payload
		{Type: TypeAudioStop}, // bare
		transcript,            // data only
	}

	var buf bytes.Buffer
	for _, ev := range events {
		if err := WriteEvent(&buf, ev); err != nil {
			t.Fatalf("write %s: %v", ev.Type, err)
		}
	}

	rd := NewReader(&buf, DefaultMaxSegment)
	for i, want := range events {
		got, err := rd.ReadEvent()
		if err != nil {
			t.Fatalf("read #%d (%s): %v", i, want.Type, err)
		}
		if got.Type != want.Type {
			t.Errorf("#%d type = %q, want %q", i, got.Type, want.Type)
		}
		if !bytes.Equal(got.Payload, want.Payload) {
			t.Errorf("#%d payload = %v, want %v", i, got.Payload, want.Payload)
		}
		if !jsonEqual(got.Data, want.Data) {
			t.Errorf("#%d data = %s, want %s", i, got.Data, want.Data)
		}
	}
	if _, err := rd.ReadEvent(); err != io.EOF {
		t.Errorf("trailing read = %v, want io.EOF", err)
	}
}

func TestWriteEventGoldenFrame(t *testing.T) {
	data := []byte(`{"rate":16000,"width":2,"channels":1}`)
	payload := []byte{0xAA, 0xBB, 0xCC, 0xDD}
	var buf bytes.Buffer
	if err := WriteEvent(&buf, &Event{Type: TypeAudioChunk, Data: data, Payload: payload}); err != nil {
		t.Fatalf("write: %v", err)
	}
	expected := []byte(fmt.Sprintf(`{"type":"audio-chunk","data_length":%d,"payload_length":%d}`, len(data), len(payload)) + "\n")
	expected = append(expected, data...)
	expected = append(expected, payload...)
	if !bytes.Equal(buf.Bytes(), expected) {
		t.Fatalf("frame bytes mismatch:\n got: %q\nwant: %q", buf.Bytes(), expected)
	}
}

// TestReadToleratesInlineData pins the interop tolerance: a producer that puts
// the data object INLINE in the header line (rather than as a length-delimited
// segment) is still parsed correctly.
func TestReadToleratesInlineData(t *testing.T) {
	frame := `{"type":"transcript","data":{"text":"inline works"}}` + "\n"
	rd := NewReader(strings.NewReader(frame), DefaultMaxSegment)
	ev, err := rd.ReadEvent()
	if err != nil {
		t.Fatalf("read inline: %v", err)
	}
	var tr Transcript
	if err := decodeData(ev, &tr); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if tr.Text != "inline works" {
		t.Errorf("text = %q, want %q", tr.Text, "inline works")
	}
}

// TestReadInlineDataWithPayload covers the mixed form: inline data + a binary
// payload segment.
func TestReadInlineDataWithPayload(t *testing.T) {
	pcm := []byte{1, 2, 3}
	frame := fmt.Sprintf(`{"type":"audio-chunk","data":{"rate":16000,"width":2,"channels":1},"payload_length":%d}`+"\n", len(pcm))
	rd := NewReader(strings.NewReader(frame+string(pcm)), DefaultMaxSegment)
	ev, err := rd.ReadEvent()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(ev.Payload, pcm) {
		t.Errorf("payload = %v, want %v", ev.Payload, pcm)
	}
}

func TestReadRejectsOversizedSegment(t *testing.T) {
	frame := `{"type":"transcript","data_length":1000000}` + "\n"
	rd := NewReader(strings.NewReader(frame), 1024) // small cap
	if _, err := rd.ReadEvent(); err == nil {
		t.Fatal("expected error for data_length over cap")
	}
}

func TestReadTruncatedPayloadErrors(t *testing.T) {
	frame := `{"type":"audio-chunk","payload_length":10}` + "\n" + "xyz" // only 3 of 10
	rd := NewReader(strings.NewReader(frame), DefaultMaxSegment)
	if _, err := rd.ReadEvent(); err == nil {
		t.Fatal("expected error for truncated payload")
	}
}

func TestReadEmptyStreamIsEOF(t *testing.T) {
	rd := NewReader(strings.NewReader(""), DefaultMaxSegment)
	if _, err := rd.ReadEvent(); err != io.EOF {
		t.Fatalf("empty stream read = %v, want io.EOF", err)
	}
}

func TestReadMissingTypeErrors(t *testing.T) {
	rd := NewReader(strings.NewReader(`{"data_length":0}`+"\n"), DefaultMaxSegment)
	if _, err := rd.ReadEvent(); err == nil {
		t.Fatal("expected error for header missing type")
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func jsonEqual(a, b json.RawMessage) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	var av, bv any
	if err := json.Unmarshal(a, &av); err != nil {
		return false
	}
	if err := json.Unmarshal(b, &bv); err != nil {
		return false
	}
	return fmt.Sprint(av) == fmt.Sprint(bv)
}
