package runtimepath

import (
	"os"
	"path/filepath"
	"strings"
)

var (
	osExecutable  = os.Executable
	statPath      = os.Stat
	userConfigDir = os.UserConfigDir
)

func ExecutableDir() string {
	exePath, err := osExecutable()
	if err != nil || strings.TrimSpace(exePath) == "" {
		return ""
	}
	return filepath.Dir(exePath)
}

func IsPortable() bool {
	exeDir := ExecutableDir()
	if exeDir == "" {
		return false
	}
	if pathExists(filepath.Join(exeDir, "uninstall.exe")) {
		return false
	}
	markers := []string{
		"config.toml",
		"config.default.toml",
		"whisper-server.exe",
		"SpeechKit.exe",
	}
	for _, marker := range markers {
		if pathExists(filepath.Join(exeDir, marker)) {
			return true
		}
	}
	return false
}

func DataDir() string {
	if IsPortable() {
		exeDir := ExecutableDir()
		if exeDir != "" {
			return filepath.Join(exeDir, "data")
		}
	}
	appData := strings.TrimSpace(os.Getenv("APPDATA"))
	if appData == "" {
		appData = "."
	}
	return filepath.Join(appData, "SpeechKit")
}

func LocalDataDir() string {
	if IsPortable() {
		return DataDir()
	}
	localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
	if localAppData == "" {
		return DataDir()
	}
	return filepath.Join(localAppData, "SpeechKit")
}

func SecretsDir() string {
	if IsPortable() {
		return filepath.Join(DataDir(), "secrets")
	}
	configDir, err := userConfigDir()
	if err != nil || strings.TrimSpace(configDir) == "" {
		return filepath.Join(DataDir(), "secrets")
	}
	return filepath.Join(configDir, "SpeechKit", "secrets")
}

func pathExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	_, err := statPath(path)
	return err == nil
}
