package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
)

func TestSQLiteTranscriptionSpeakerRoundTrip(t *testing.T) {
	s, err := NewSQLiteStore(StoreConfig{SQLitePath: filepath.Join(t.TempDir(), "speakers.db"), MaxAudioStorageMB: 100})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	result := &speaker.DiarizationResult{
		Provider: "deepgram",
		Level:    speaker.IdentificationDiarization,
		Segments: []speaker.SpeakerSegment{
			{Text: "hello", SpeakerLabel: "speaker_0", StartMs: 0, EndMs: 400},
		},
	}
	if err := s.SaveTranscriptionWithAudioAndSpeakers(context.Background(), "hello", "en", "deepgram", "nova-3", 400, 80, AudioAssetInput{}, result); err != nil {
		t.Fatalf("SaveTranscriptionWithAudioAndSpeakers: %v", err)
	}

	items, err := s.ListTranscriptions(context.Background(), ListOpts{Limit: 1})
	if err != nil {
		t.Fatalf("ListTranscriptions: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	if items[0].Speakers == nil || len(items[0].Speakers.Segments) != 1 {
		t.Fatalf("speakers = %+v", items[0].Speakers)
	}
	if items[0].Speakers.Segments[0].SpeakerLabel != "speaker_0" {
		t.Fatalf("speaker label = %q", items[0].Speakers.Segments[0].SpeakerLabel)
	}
}
