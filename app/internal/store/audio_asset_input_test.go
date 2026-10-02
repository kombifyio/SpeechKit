package store

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSQLiteTranscriptionAudioAssetPreservesMimeAndExtension(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "feedback.db")
	s, err := NewSQLiteStore(StoreConfig{Backend: "sqlite", SQLitePath: dbPath, SaveAudio: true})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	audio := AudioAssetInput{
		Data:      []byte("mp3-bytes"),
		MimeType:  "audio/mpeg",
		Extension: ".mp3",
	}
	if err := s.SaveTranscriptionWithAudio(context.Background(), "hello", "en", "openai", "whisper-1", 1000, 50, audio); err != nil {
		t.Fatalf("SaveTranscriptionWithAudio: %v", err)
	}

	items, err := s.ListTranscriptions(context.Background(), ListOpts{Limit: 1})
	if err != nil {
		t.Fatalf("ListTranscriptions: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if !strings.HasSuffix(items[0].AudioPath, ".mp3") {
		t.Fatalf("AudioPath = %q, want .mp3 extension", items[0].AudioPath)
	}
	if items[0].Audio == nil || items[0].Audio.MimeType != "audio/mpeg" {
		t.Fatalf("Audio = %+v, want audio/mpeg metadata", items[0].Audio)
	}
	got, err := os.ReadFile(items[0].AudioPath)
	if err != nil {
		t.Fatalf("read saved audio: %v", err)
	}
	if !bytes.Equal(got, audio.Data) {
		t.Fatalf("saved audio = %q, want %q", string(got), string(audio.Data))
	}
}
