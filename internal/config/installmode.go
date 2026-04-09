package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/kombifyio/SpeechKit/internal/runtimepath"
	"github.com/google/uuid"
)

// InstallMode defines whether SpeechKit runs locally or connected to kombify Cloud.
type InstallMode string

const (
	InstallModeLocal  InstallMode = "local"
	InstallModeCloud  InstallMode = "cloud"
	InstallModeNotSet InstallMode = ""
)

// InstallState persists the user's install mode choice and device identity.
// Stored in %APPDATA%/SpeechKit/install.toml, separate from config.toml.
type InstallState struct {
	Mode      InstallMode `toml:"mode"`
	SetupDone bool        `toml:"setup_done"`
	DeviceID  string      `toml:"device_id"`
}

// installStateDir returns the directory for install state (APPDATA/SpeechKit).
func installStateDir() string {
	return runtimepath.DataDir()
}

// installStatePath returns the full path to install.toml.
func installStatePath() string {
	return filepath.Join(installStateDir(), "install.toml")
}

// LoadInstallState reads the install state from disk.
// Returns a default (empty mode) if the file doesn't exist.
func LoadInstallState() (*InstallState, error) {
	path := installStatePath()
	state := &InstallState{}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return state, nil
		}
		return nil, fmt.Errorf("read install state: %w", err)
	}

	if err := toml.Unmarshal(data, state); err != nil {
		return nil, fmt.Errorf("parse install state: %w", err)
	}

	return state, nil
}

// SaveInstallState writes the install state to disk.
func SaveInstallState(state *InstallState) error {
	dir := installStateDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create install state dir: %w", err)
	}

	if state.DeviceID == "" {
		state.DeviceID = uuid.New().String()
	}

	path := installStatePath()
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create install state: %w", err)
	}
	defer file.Close()

	if err := toml.NewEncoder(file).Encode(state); err != nil {
		return fmt.Errorf("encode install state: %w", err)
	}

	return nil
}

// IsFirstRun returns true if no install state file exists.
func IsFirstRun() bool {
	_, err := os.Stat(installStatePath())
	return os.IsNotExist(err)
}

// ApplyLocalInstallDefaults configures a pending local install to use the bundled
// local runtime without requiring additional manual setup.
func ApplyLocalInstallDefaults(cfg *Config, state *InstallState) bool {
	if cfg == nil || state == nil {
		return false
	}
	if state.Mode != InstallModeLocal || state.SetupDone {
		return false
	}

	changed := false
	if !cfg.Local.Enabled {
		cfg.Local.Enabled = true
		changed = true
	}
	if cfg.Routing.Strategy == "" || cfg.Routing.Strategy == "cloud-only" {
		cfg.Routing.Strategy = "dynamic"
		changed = true
	}
	if cfg.Local.Model == "" {
		cfg.Local.Model = "ggml-small.bin"
		changed = true
	}
	if cfg.Local.Port == 0 {
		cfg.Local.Port = 8080
		changed = true
	}
	if cfg.Local.GPU == "" {
		cfg.Local.GPU = "auto"
		changed = true
	}

	return changed
}
