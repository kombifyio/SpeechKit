//go:build windows

package runtimepath

import (
	"path/filepath"
	"strings"
	"testing"
)

// A fresh install keeps user content in Local AppData and names Roaming only
// as the place old releases wrote to; a pin overrides the default.
func TestContentDirIsLocalAppDataOnAnInstall(t *testing.T) {
	local := t.TempDir()
	roaming := t.TempDir()
	t.Setenv("LOCALAPPDATA", local)
	t.Setenv("APPDATA", roaming)
	withMocks(t, mockExecutable(`C:\install\SpeechKit.exe`, nil), mockStat(map[string]bool{}), nil)
	t.Cleanup(func() { SetContentDir("") })

	if got := must(t, ContentDir); !strings.HasPrefix(got, local) {
		t.Errorf("ContentDir() = %q, want it under LOCALAPPDATA %q", got, local)
	}
	if got := must(t, LegacyContentDir); !strings.HasPrefix(got, roaming) {
		t.Errorf("LegacyContentDir() = %q, want it under APPDATA %q", got, roaming)
	}

	pinned := filepath.Join(t.TempDir(), "content")
	SetContentDir(pinned)
	if got := must(t, ContentDir); got != pinned {
		t.Errorf("ContentDir() = %q after pin, want %q", got, pinned)
	}
}
