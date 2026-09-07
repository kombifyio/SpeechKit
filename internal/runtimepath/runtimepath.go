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
	userHomeDir   = os.UserHomeDir
)

func ExecutableDir() string {
	exePath, err := osExecutable()
	if err != nil || strings.TrimSpace(exePath) == "" {
		return ""
	}
	return filepath.Dir(exePath)
}

// IsPortable reports whether the binary runs from a portable bundle: a
// config or whisper runtime sits next to the executable and nothing marks
// the location as a managed install (uninstaller, Program Files, a macOS app
// bundle, a development checkout).
func IsPortable() bool {
	if portableDisabled() {
		return false
	}
	exeDir := ExecutableDir()
	if exeDir == "" {
		return false
	}
	if looksLikeDevWorkspace(exeDir) {
		return false
	}
	if !platformAllowsPortable(exeDir) {
		return false
	}
	if isUnderProgramFiles(exeDir) {
		return false
	}
	if pathExists(filepath.Join(exeDir, "uninstall.exe")) {
		return false
	}
	markers := []string{
		"config.toml",
		"config.default.toml",
		"whisper-server.exe",
	}
	for _, marker := range markers {
		if pathExists(filepath.Join(exeDir, marker)) {
			return true
		}
	}
	return false
}

func portableDisabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("SPEECHKIT_DISABLE_PORTABLE"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// DataDir is the roaming per-user directory for config, feedback and
// secrets. Portable bundles keep it next to the executable; installed
// builds use the platform's per-user location (paths_<os>.go).
func DataDir() string {
	if IsPortable() {
		exeDir := ExecutableDir()
		if exeDir != "" {
			return filepath.Join(exeDir, "data")
		}
	}
	return platformDataDir()
}

// LocalDataDir is the machine-local per-user directory for large,
// non-roaming data such as downloaded models and runtimes.
func LocalDataDir() string {
	if IsPortable() {
		return DataDir()
	}
	return platformLocalDataDir()
}

func ModelsDir() string {
	return filepath.Join(LocalDataDir(), "models")
}

func ConfigFilePath() string {
	if IsPortable() {
		exeDir := ExecutableDir()
		if exeDir != "" {
			return filepath.Join(exeDir, "config.toml")
		}
	}
	return filepath.Join(DataDir(), "config.toml")
}

func SecretsDir() string {
	if IsPortable() {
		return filepath.Join(DataDir(), "secrets")
	}
	return platformSecretsDir()
}

// envDataDir is the %APPDATA%-based roaming directory the Windows install
// uses; other Unix targets share it so a server container that sets the
// same variables behaves as before.
func envDataDir() string {
	appData := strings.TrimSpace(os.Getenv("APPDATA"))
	if appData == "" {
		appData = "."
	}
	return filepath.Join(appData, "SpeechKit")
}

// envLocalDataDir is the %LOCALAPPDATA%-based local directory, falling
// back to DataDir when the variable is unset.
func envLocalDataDir() string {
	localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
	if localAppData == "" {
		return DataDir()
	}
	return filepath.Join(localAppData, "SpeechKit")
}

// configDirSecretsDir keeps secrets under os.UserConfigDir, falling back to
// DataDir when the platform cannot report one.
func configDirSecretsDir() string {
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

func looksLikeDevWorkspace(exeDir string) bool {
	if strings.TrimSpace(exeDir) == "" {
		return false
	}
	return pathExists(filepath.Join(exeDir, "go.mod")) &&
		pathExists(filepath.Join(exeDir, "frontend", "app", "package.json"))
}

func isUnderProgramFiles(path string) bool {
	for _, root := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)")} {
		if isSameOrChildPath(path, root) {
			return true
		}
	}
	return false
}

func isSameOrChildPath(path, root string) bool {
	path = strings.TrimSpace(path)
	root = strings.TrimSpace(root)
	if path == "" || root == "" {
		return false
	}

	cleanPath := filepath.Clean(path)
	cleanRoot := filepath.Clean(root)
	if strings.EqualFold(cleanPath, cleanRoot) {
		return true
	}

	rel, err := filepath.Rel(cleanRoot, cleanPath)
	if err != nil || rel == "." || rel == ".." || filepath.IsAbs(rel) {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
