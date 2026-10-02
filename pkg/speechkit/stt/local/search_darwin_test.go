//go:build darwin

package local

import (
	"os"
	"path/filepath"
	"testing"
)

// whisper-server runs with SpeechKit's TCC grants, so it may only come from
// inside the signed bundle, never from a folder any process of the user can
// write (M5).
func TestFindWhisperBinaryRefusesApplicationSupport(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SPEECHKIT_ALLOW_WHISPER_PATH", "0")
	base := filepath.Join(home, "Library", "Application Support", "SpeechKit")
	for _, dir := range []string{base, filepath.Join(base, "bin")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "whisper-server"), []byte("fake"), 0o755); err != nil { //nolint:gosec // G306: a fake executable in a temp dir needs the exec bit.
			t.Fatal(err)
		}
	}
	if got, err := findWhisperBinary(); err == nil {
		t.Fatalf("findWhisperBinary = %q, want no binary outside the app bundle", got)
	}
}

func TestWhisperBinaryNamesOnDarwin(t *testing.T) {
	names := whisperBinaryNames()
	if len(names) != 1 || names[0] != "whisper-server" {
		t.Fatalf("whisperBinaryNames() = %v, want [whisper-server]", names)
	}
}
