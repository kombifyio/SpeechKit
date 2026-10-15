package runtimepath

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/kombifyio/SpeechKit/app/internal/productid"
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

// portableDisabled reports the <EnvPrefix>_DISABLE_PORTABLE switch. A product
// without an environment prefix (or none registered) reads no override.
func portableDisabled() bool {
	product, err := productid.Current()
	if err != nil || product.EnvPrefix == "" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv(product.EnvPrefix + "_DISABLE_PORTABLE"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// dataDirName is the registered product's data directory name. Every path
// function below fails closed with productid.ErrNoIdentity without one, so a
// process that names no product never gets a relative or shared path.
func dataDirName() (string, error) {
	p, err := productid.Current()
	if err != nil {
		return "", err
	}
	return p.DataDirName, nil
}

// DataDir is the roaming per-user directory for config, feedback and
// secrets. Portable bundles keep it next to the executable; installed
// builds use the platform's per-user location (paths_<os>.go).
func DataDir() (string, error) {
	name, err := dataDirName()
	if err != nil {
		return "", err
	}
	if IsPortable() {
		exeDir := ExecutableDir()
		if exeDir != "" {
			return filepath.Join(exeDir, "data"), nil
		}
	}
	return platformDataDir(name), nil
}

// LocalDataDir is the machine-local per-user directory for large,
// non-roaming data such as downloaded models and runtimes.
func LocalDataDir() (string, error) {
	name, err := dataDirName()
	if err != nil {
		return "", err
	}
	if IsPortable() {
		return DataDir()
	}
	return platformLocalDataDir(name), nil
}

// ContentDir is the data root: where user content lives (the SQLite
// database, recordings, snapshots, wake-word clips). On Windows installs it
// is %LOCALAPPDATA%\<DataDirName>, never Roaming, because roaming profiles,
// FSLogix and UE-V copy Roaming to file servers. SetContentDir pins it (the
// data_root setting, or the root start-up kept after a failed migration);
// portable bundles keep <exe>\data; macOS and the other targets keep
// DataDir.
func ContentDir() (string, error) {
	if _, err := dataDirName(); err != nil {
		return "", err
	}
	contentMu.RLock()
	pinned := contentOverride
	contentMu.RUnlock()
	if pinned != "" {
		return pinned, nil
	}
	return DefaultContentDir()
}

// DefaultContentDir is the data root without any pin applied.
func DefaultContentDir() (string, error) {
	name, err := dataDirName()
	if err != nil {
		return "", err
	}
	if IsPortable() {
		return DataDir()
	}
	return platformContentDir(name), nil
}

// LegacyContentDir is the roaming directory earlier releases kept user
// content in, or "" where the content root never moved (portable bundles,
// macOS, Unix).
func LegacyContentDir() (string, error) {
	name, err := dataDirName()
	if err != nil {
		return "", err
	}
	if IsPortable() {
		return "", nil
	}
	return platformLegacyContentDir(name), nil
}

// SetContentDir pins the data root for this process; "" clears the pin.
func SetContentDir(dir string) {
	contentMu.Lock()
	contentOverride = strings.TrimSpace(dir)
	contentMu.Unlock()
}

var (
	contentMu       sync.RWMutex
	contentOverride string
)

// ModelsDir is where downloaded models live, under LocalDataDir.
func ModelsDir() (string, error) {
	local, err := LocalDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(local, "models"), nil
}

// ConfigFilePath is config.toml: next to the executable of a portable
// bundle, in DataDir otherwise.
func ConfigFilePath() (string, error) {
	data, err := DataDir()
	if err != nil {
		return "", err
	}
	if IsPortable() {
		exeDir := ExecutableDir()
		if exeDir != "" {
			return filepath.Join(exeDir, "config.toml"), nil
		}
	}
	return filepath.Join(data, "config.toml"), nil
}

// SecretsDir is where the encrypted secret store keeps its blobs.
func SecretsDir() (string, error) {
	name, err := dataDirName()
	if err != nil {
		return "", err
	}
	if IsPortable() {
		data, err := DataDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(data, "secrets"), nil
	}
	return platformSecretsDir(name), nil
}

// envDataDir is the %APPDATA%-based roaming directory the Windows install
// uses; other Unix targets share it so a server container that sets the
// same variables behaves as before.
//
// Without APPDATA the data must still land in the user's own profile. The
// old fallback was ".\<name>" in the current working directory, which for a
// process started from a shared or system folder put transcripts and
// recordings wherever that happened to be.
func envDataDir(name string) string {
	if appData := strings.TrimSpace(os.Getenv("APPDATA")); appData != "" {
		return filepath.Join(appData, name)
	}
	if configDir, err := userConfigDir(); err == nil && strings.TrimSpace(configDir) != "" {
		return filepath.Join(configDir, name)
	}
	if home, err := userHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		return filepath.Join(home, "."+strings.ToLower(name))
	}
	// No profile at all (a stripped service environment): the per-user
	// temp directory is still private to the account, unlike the CWD.
	return filepath.Join(os.TempDir(), name)
}

// envLocalDataDir is the %LOCALAPPDATA%-based local directory, falling
// back to the roaming directory when the variable is unset.
func envLocalDataDir(name string) string {
	localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
	if localAppData == "" {
		return platformDataDir(name)
	}
	return filepath.Join(localAppData, name)
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
