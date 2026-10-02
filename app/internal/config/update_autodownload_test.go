package config

import (
	"os"
	"path/filepath"
	"testing"
)

// The real user config has an [update] section that predates auto_download.
// A missing key must keep the protective default (true), not silently
// decode to false.
func TestUpdateAutoDownloadDefaultsWhenKeyAbsent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(`
[update]
enabled = true
manifest_url = "https://api.github.com/repos/kombifyio/SpeechKit/releases/latest"
check_interval_hours = 6
signature_pin_thumbprint = ""
`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Update.AutoDownload {
		t.Fatal("auto_download flipped to false when the key was absent from an existing [update] section")
	}
	// And an explicit opt-out must be honoured.
	if err := os.WriteFile(path, []byte("[update]\nauto_download = false\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg, err = Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Update.AutoDownload {
		t.Fatal("explicit auto_download = false was ignored")
	}
}
