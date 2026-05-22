package wakeword

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// generatePCM produces durationMs of silent S16LE PCM at the
// canonical 16 kHz rate. Silent (all-zero) audio is sufficient for
// the capture machinery — we only assert structure, not content.
func generatePCM(durationMs int) []byte {
	samples := (durationMs * SampleRate) / 1000
	out := make([]byte, samples*BytesPerSample)
	return out
}

// generatePCMWithMarker produces PCM where every sample = value. Used
// to assert that the ring buffer is rotating correctly — the values
// in the output WAV reflect the most recent ingest.
func generatePCMWithMarker(durationMs int, value int16) []byte {
	samples := (durationMs * SampleRate) / 1000
	out := make([]byte, samples*BytesPerSample)
	for i := 0; i < samples; i++ {
		binary.LittleEndian.PutUint16(out[i*BytesPerSample:], uint16(value))
	}
	return out
}

func TestTrainingCapture_DisabledIsNoOp(t *testing.T) {
	dir := t.TempDir()
	c, err := NewTrainingCapture(TrainingCaptureConfig{
		Enabled:    false,
		Dir:        dir,
		PreRollMs:  1500,
		PostRollMs: 500,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Enabled() {
		t.Fatal("expected Enabled=false")
	}
	c.Ingest(generatePCM(2000))
	c.Trigger(DetectionEvent{Phrase: "Hey Quby", Keyword: "hey_quby", Probability: 0.9, At: time.Now()})
	_ = c.Close()
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("disabled capture wrote %d files, expected 0", len(entries))
	}
}

func TestTrainingCapture_RejectsMissingDir(t *testing.T) {
	_, err := NewTrainingCapture(TrainingCaptureConfig{
		Enabled:    true,
		Dir:        "/definitely/does/not/exist/here",
		PreRollMs:  1500,
		PostRollMs: 500,
	})
	if err == nil {
		t.Fatal("expected error for missing dir")
	}
}

func TestTrainingCapture_RejectsZeroPreRoll(t *testing.T) {
	dir := t.TempDir()
	_, err := NewTrainingCapture(TrainingCaptureConfig{
		Enabled:    true,
		Dir:        dir,
		PreRollMs:  0,
		PostRollMs: 500,
	})
	if err == nil {
		t.Fatal("expected error for zero pre-roll")
	}
}

func TestTrainingCapture_FlushesAfterPostRoll(t *testing.T) {
	dir := t.TempDir()
	var captured []TrainingRecord
	c, err := NewTrainingCapture(TrainingCaptureConfig{
		Enabled:    true,
		Dir:        dir,
		PreRollMs:  500,
		PostRollMs: 250,
		Backend:    "test_backend",
		OnWrite:    func(rec TrainingRecord) { captured = append(captured, rec) },
		TimeSource: func() time.Time { return time.Date(2026, 5, 21, 14, 7, 3, 421_000_000, time.UTC) },
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer c.Close()

	// Pre-fill ring with 500 ms of audio.
	c.Ingest(generatePCM(500))

	// Fire detection — capture starts collecting post-roll.
	c.Trigger(DetectionEvent{
		Phrase:      "Hey Quby",
		Keyword:     "hey_quby",
		Probability: 0.87,
		At:          time.Date(2026, 5, 21, 14, 7, 3, 421_000_000, time.UTC),
	})

	if len(captured) != 0 {
		t.Errorf("expected no flush before post-roll arrives, got %d", len(captured))
	}

	// Feed 250 ms of post-roll → flush.
	c.Ingest(generatePCM(250))

	if len(captured) != 1 {
		t.Fatalf("expected exactly 1 record, got %d", len(captured))
	}
	rec := captured[0]
	if rec.PhraseID != "hey_quby" {
		t.Errorf("PhraseID = %q, want hey_quby", rec.PhraseID)
	}
	if rec.Phrase != "Hey Quby" {
		t.Errorf("Phrase = %q", rec.Phrase)
	}
	if rec.Backend != "test_backend" {
		t.Errorf("Backend = %q", rec.Backend)
	}
	if rec.Score != 0.87 {
		t.Errorf("Score = %v", rec.Score)
	}
	if rec.SampleRate != SampleRate {
		t.Errorf("SampleRate = %d", rec.SampleRate)
	}
	if rec.PreRollMs != 500 || rec.PostRollMs != 250 {
		t.Errorf("PreRollMs=%d PostRollMs=%d", rec.PreRollMs, rec.PostRollMs)
	}

	// WAV file must exist on disk with the expected payload size.
	wavPath := filepath.Join(dir, rec.AudioPath)
	wavBytes, err := os.ReadFile(wavPath)
	if err != nil {
		t.Fatalf("WAV not written: %v", err)
	}
	if len(wavBytes) < 44 {
		t.Fatalf("WAV too small: %d bytes", len(wavBytes))
	}
	if !bytes.Equal(wavBytes[:4], []byte("RIFF")) {
		t.Errorf("WAV header missing RIFF magic")
	}
	if !bytes.Equal(wavBytes[8:12], []byte("WAVE")) {
		t.Errorf("WAV header missing WAVE marker")
	}
	// 750 ms at 16 kHz mono S16 = 12000 samples × 2 bytes = 24000 data bytes.
	wantData := uint32(24000)
	gotData := binary.LittleEndian.Uint32(wavBytes[40:44])
	if gotData != wantData {
		t.Errorf("WAV data size = %d, want %d", gotData, wantData)
	}

	// JSON sidecar must exist and round-trip.
	jsonPath := strings.TrimSuffix(wavPath, ".wav") + ".json"
	jsonBytes, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("JSON sidecar not written: %v", err)
	}
	var parsed TrainingRecord
	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("JSON sidecar parse failed: %v", err)
	}
	if parsed.ID != rec.ID || parsed.Phrase != rec.Phrase || parsed.AudioPath != rec.AudioPath {
		t.Errorf("JSON sidecar mismatches in-memory record: %+v vs %+v", parsed, rec)
	}
}

func TestTrainingCapture_PostRollZero_FlushesImmediately(t *testing.T) {
	dir := t.TempDir()
	var captured []TrainingRecord
	c, err := NewTrainingCapture(TrainingCaptureConfig{
		Enabled:    true,
		Dir:        dir,
		PreRollMs:  200,
		PostRollMs: 0,
		OnWrite:    func(rec TrainingRecord) { captured = append(captured, rec) },
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer c.Close()

	c.Ingest(generatePCM(200))
	c.Trigger(DetectionEvent{Phrase: "Hey Quby", Keyword: "hey_quby", Probability: 0.9, At: time.Now()})
	if len(captured) != 1 {
		t.Fatalf("expected immediate flush, got %d records", len(captured))
	}
}

func TestTrainingCapture_RingRotatesCorrectly(t *testing.T) {
	dir := t.TempDir()
	var captured []TrainingRecord
	c, err := NewTrainingCapture(TrainingCaptureConfig{
		Enabled:    true,
		Dir:        dir,
		PreRollMs:  100,
		PostRollMs: 0,
		OnWrite:    func(rec TrainingRecord) { captured = append(captured, rec) },
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer c.Close()

	// Feed 500 ms of marker=1, then 100 ms of marker=42. The ring
	// should retain only the most recent 100 ms (marker=42).
	c.Ingest(generatePCMWithMarker(500, 1))
	c.Ingest(generatePCMWithMarker(100, 42))
	c.Trigger(DetectionEvent{Phrase: "X", Keyword: "x", Probability: 1.0, At: time.Now()})

	if len(captured) != 1 {
		t.Fatalf("expected one capture, got %d", len(captured))
	}
	wavBytes, err := os.ReadFile(filepath.Join(dir, captured[0].AudioPath))
	if err != nil {
		t.Fatalf("read wav: %v", err)
	}
	// Skip 44-byte header. Inspect first and last samples for marker=42.
	if len(wavBytes) < 44+4 {
		t.Fatal("WAV too short to inspect")
	}
	first := int16(binary.LittleEndian.Uint16(wavBytes[44:46]))
	last := int16(binary.LittleEndian.Uint16(wavBytes[len(wavBytes)-2:]))
	if first != 42 || last != 42 {
		t.Errorf("ring did not retain marker=42 only — first=%d last=%d", first, last)
	}
}

func TestTrainingCapture_ConcurrentIngestAndTrigger(t *testing.T) {
	dir := t.TempDir()
	c, err := NewTrainingCapture(TrainingCaptureConfig{
		Enabled:    true,
		Dir:        dir,
		PreRollMs:  100,
		PostRollMs: 100,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer c.Close()
	// Run Ingest + Trigger in parallel to surface races under -race.
	done := make(chan struct{}, 2)
	go func() {
		for i := 0; i < 50; i++ {
			c.Ingest(generatePCM(20))
		}
		done <- struct{}{}
	}()
	go func() {
		for i := 0; i < 20; i++ {
			c.Trigger(DetectionEvent{Phrase: "Hey", Keyword: "hey", Probability: 0.5, At: time.Now()})
		}
		done <- struct{}{}
	}()
	<-done
	<-done
}

func TestTrainingCapture_FilenamesAreSortable(t *testing.T) {
	t1 := time.Date(2026, 5, 21, 14, 7, 3, 421_000_000, time.UTC)
	t2 := time.Date(2026, 5, 21, 14, 7, 4, 0, time.UTC)
	p1 := &pendingCapture{phraseID: "hey_quby", score: 0.87, capturedAt: t1}
	p2 := &pendingCapture{phraseID: "hey_quby", score: 0.92, capturedAt: t2}
	f1 := captureFilename(p1)
	f2 := captureFilename(p2)
	if f1 >= f2 {
		t.Errorf("filenames not sortable: %q >= %q", f1, f2)
	}
	if !strings.Contains(f1, "hey_quby") || !strings.Contains(f1, "0.87") {
		t.Errorf("filename missing phrase/score: %q", f1)
	}
}

func TestNormalisePhraseID_StripsUnsafeChars(t *testing.T) {
	cases := map[string]string{
		"Hey Quby":     "hey_quby",
		"HEY QUBY!":    "hey_quby",
		"a/b\\c":       "abc",
		"hey-quby_v2":  "hey-quby_v2",
		"  spaces  ":   "__spaces__",
		"":             "",
		"hey?computer": "heycomputer",
	}
	for in, want := range cases {
		got := normalisePhraseID(in)
		if got != want {
			t.Errorf("normalisePhraseID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMsToSamples_RoundTripsAtCanonicalRate(t *testing.T) {
	cases := map[int]int{
		0:    0,
		1:    16,
		1000: 16000,
		1500: 24000,
		500:  8000,
	}
	for ms, want := range cases {
		got := msToSamples(ms)
		if got != want {
			t.Errorf("msToSamples(%d) = %d, want %d", ms, got, want)
		}
	}
	// Round-trip back to ms with rounding tolerance.
	for ms := range cases {
		if ms == 0 {
			continue
		}
		round := samplesToMs(msToSamples(ms))
		if round != ms {
			t.Errorf("ms %d -> samples -> ms = %d", ms, round)
		}
	}
}

func TestEncodeWAV_HeaderShape(t *testing.T) {
	samples := make([]int16, 100)
	wav := encodeWAV(samples)
	if len(wav) != 44+200 {
		t.Fatalf("WAV size = %d, want %d", len(wav), 44+200)
	}
	if !bytes.Equal(wav[:4], []byte("RIFF")) {
		t.Error("missing RIFF")
	}
	if !bytes.Equal(wav[8:12], []byte("WAVE")) {
		t.Error("missing WAVE")
	}
	if !bytes.Equal(wav[12:16], []byte("fmt ")) {
		t.Error("missing fmt chunk")
	}
	if !bytes.Equal(wav[36:40], []byte("data")) {
		t.Error("missing data chunk")
	}
	if binary.LittleEndian.Uint16(wav[20:22]) != 1 {
		t.Error("audio format != 1 (PCM)")
	}
	if binary.LittleEndian.Uint16(wav[22:24]) != uint16(Channels) {
		t.Error("channels mismatch")
	}
	if binary.LittleEndian.Uint32(wav[24:28]) != uint32(SampleRate) {
		t.Error("sample-rate mismatch")
	}
}
