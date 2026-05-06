package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuildLocalAudioAssetReadsFileMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.wav")
	payload := []byte("fake wav")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatalf("write audio fixture: %v", err)
	}

	asset := buildLocalAudioAsset(path, 1234)
	if asset == nil {
		t.Fatal("expected audio asset metadata")
	}
	if asset.StorageKind != AudioStorageLocalFile {
		t.Fatalf("storage kind = %q, want %q", asset.StorageKind, AudioStorageLocalFile)
	}
	if asset.Path != path || asset.MimeType != "audio/wav" || asset.DurationMs != 1234 {
		t.Fatalf("asset metadata = %+v", asset)
	}
	if asset.SizeBytes != int64(len(payload)) {
		t.Fatalf("asset size = %d, want %d", asset.SizeBytes, len(payload))
	}
}

func TestNormalizeTranscriptionModelHints(t *testing.T) {
	hints := normalizeTranscriptionModelHints(map[string]string{
		" HF ":      " whisper-large-v3 ",
		"openai":    "",
		" ":         "ignored",
		"GOOGLE_AI": " chirp ",
	})

	if hints["hf"] != "whisper-large-v3" {
		t.Fatalf("hf hint = %q", hints["hf"])
	}
	if hints["google_ai"] != "chirp" {
		t.Fatalf("google_ai hint = %q", hints["google_ai"])
	}
	if _, ok := hints["openai"]; ok {
		t.Fatalf("empty model hint should be omitted: %#v", hints)
	}
}
