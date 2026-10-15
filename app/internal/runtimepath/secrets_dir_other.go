//go:build !darwin

package runtimepath

import (
	"path/filepath"
	"strings"
)

// configDirSecretsDir keeps secrets under os.UserConfigDir, falling back to
// the roaming data directory when the platform cannot report one. macOS keeps
// them under its data directory instead (paths_darwin.go).
func configDirSecretsDir(name string) string {
	configDir, err := userConfigDir()
	if err != nil || strings.TrimSpace(configDir) == "" {
		return filepath.Join(platformDataDir(name), "secrets")
	}
	return filepath.Join(configDir, name, "secrets")
}
