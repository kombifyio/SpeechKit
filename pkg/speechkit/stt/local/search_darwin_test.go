//go:build darwin

package local

import (
	"path/filepath"
	"testing"
)

func TestBundleHelpersDirFor(t *testing.T) {
	cases := map[string]string{
		"":                         "",
		"/usr/local/bin/speechkit": "",
		"/tmp/build/speechkit":     "",
		"/Applications/SpeechKit.app/Contents/MacOS/SpeechKit": "/Applications/SpeechKit.app/Contents/Helpers",
		"/Applications/SpeechKit.app/Contents/Resources/x":     "",
	}
	for exe, want := range cases {
		if got := bundleHelpersDirFor(exe); got != want {
			t.Errorf("bundleHelpersDirFor(%q) = %q, want %q", exe, got, want)
		}
	}
}

func TestPlatformWhisperSearchDirsUsesApplicationSupport(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dirs := platformWhisperSearchDirs()
	base := filepath.Join(home, "Library", "Application Support", "SpeechKit")
	wantSuffixes := []string{base, filepath.Join(base, "bin")}
	for _, want := range wantSuffixes {
		found := false
		for _, dir := range dirs {
			if dir == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("platformWhisperSearchDirs() = %v, missing %q", dirs, want)
		}
	}
	for _, dir := range dirs {
		if dir == "/opt/homebrew/bin" || dir == "/usr/local/bin" {
			t.Errorf("platformWhisperSearchDirs() must not probe PATH-like directory %q", dir)
		}
	}
}

func TestWhisperBinaryNamesOnDarwin(t *testing.T) {
	names := whisperBinaryNames()
	if len(names) != 1 || names[0] != "whisper-server" {
		t.Fatalf("whisperBinaryNames() = %v, want [whisper-server]", names)
	}
}
