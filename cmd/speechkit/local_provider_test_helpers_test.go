package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/stt"
)

func installTestWhisperBinary(t *testing.T) {
	t.Helper()

	localAppData := t.TempDir()
	t.Setenv("LOCALAPPDATA", localAppData)
	binDir := filepath.Join(localAppData, "SpeechKit", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir whisper bin dir: %v", err)
	}
	binaryPath := filepath.Join(binDir, "whisper-server.exe")
	if err := os.WriteFile(binaryPath, []byte("test-whisper-binary"), 0o755); err != nil {
		t.Fatalf("write whisper binary: %v", err)
	}
}

func writeValidWhisperModelFile(t *testing.T, path string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir model dir: %v", err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create model file: %v", err)
	}
	defer file.Close()

	if err := file.Truncate(stt.MinWhisperModelBytes); err != nil {
		t.Fatalf("truncate model file: %v", err)
	}
}
