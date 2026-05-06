package update

import (
	"path/filepath"
	"testing"
)

func TestInstallerAssetName(t *testing.T) {
	tests := []struct {
		name    string
		release ReleaseInfo
		want    string
	}{
		{
			name: "prefers release asset name",
			release: ReleaseInfo{
				Version:      "0.29.0",
				DownloadURL:  "https://example.com/releases/SpeechKit-Setup-v0.29.0.exe",
				DownloadName: "SpeechKit-Setup-v0.29.0-signed.exe",
			},
			want: "SpeechKit-Setup-v0.29.0-signed.exe",
		},
		{
			name: "falls back to URL basename",
			release: ReleaseInfo{
				Version:     "0.29.0",
				DownloadURL: "https://example.com/releases/SpeechKit-Setup-v0.29.0.msi",
			},
			want: "SpeechKit-Setup-v0.29.0.msi",
		},
		{
			name:    "falls back to setup exe name",
			release: ReleaseInfo{Version: "0.29.0"},
			want:    "SpeechKit-Setup-v0.29.0.exe",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := InstallerAssetName(tt.release); got != tt.want {
				t.Fatalf("InstallerAssetName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveDir(t *testing.T) {
	if got := ResolveDir(filepath.Join("C:", "SpeechKit", "config.toml"), "", "", ""); got != filepath.Join("C:", "SpeechKit", "updates") {
		t.Fatalf("ResolveDir(config) = %q", got)
	}
	if got := ResolveDir("", filepath.Join("C:", "Program Files", "SpeechKit"), "", ""); got != filepath.Join("C:", "Program Files", "SpeechKit", "updates") {
		t.Fatalf("ResolveDir(exe) = %q", got)
	}
	if got := ResolveDir("", "", filepath.Join("C:", "Users", "me", "AppData", "Local"), ""); got != filepath.Join("C:", "Users", "me", "AppData", "Local", "SpeechKit", "updates") {
		t.Fatalf("ResolveDir(local app data) = %q", got)
	}
}

func TestIsInstallerAssetName(t *testing.T) {
	for _, name := range []string{"SpeechKit.exe", "SpeechKit.msi"} {
		if !IsInstallerAssetName(name) {
			t.Fatalf("IsInstallerAssetName(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"SpeechKit.zip", "", "README.txt"} {
		if IsInstallerAssetName(name) {
			t.Fatalf("IsInstallerAssetName(%q) = true, want false", name)
		}
	}
}
