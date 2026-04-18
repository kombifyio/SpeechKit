package runtimepath

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// withMocks swaps the package-level hooks for the duration of a test and
// restores them after. Having a single helper keeps tests compact and avoids
// leaking mock state between cases when t.Parallel is off (the package's
// package-level vars are not goroutine-safe for parallel execution).
func withMocks(t *testing.T, exe func() (string, error), stat func(string) (os.FileInfo, error), cfgDir func() (string, error)) {
	t.Helper()
	prevExe, prevStat, prevCfg := osExecutable, statPath, userConfigDir
	if exe != nil {
		osExecutable = exe
	}
	if stat != nil {
		statPath = stat
	}
	if cfgDir != nil {
		userConfigDir = cfgDir
	}
	t.Cleanup(func() {
		osExecutable = prevExe
		statPath = prevStat
		userConfigDir = prevCfg
	})
}

func mockExecutable(path string, err error) func() (string, error) {
	return func() (string, error) { return path, err }
}

// mockStat returns os.ErrNotExist for any path not in the set.
func mockStat(existing map[string]bool) func(string) (os.FileInfo, error) {
	return func(path string) (os.FileInfo, error) {
		if existing[path] {
			return fakeFileInfo{name: filepath.Base(path)}, nil
		}
		return nil, os.ErrNotExist
	}
}

type fakeFileInfo struct {
	name string
}

func (f fakeFileInfo) Name() string      { return f.name }
func (f fakeFileInfo) Size() int64       { return 0 }
func (f fakeFileInfo) Mode() os.FileMode { return 0 }
func (f fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (f fakeFileInfo) IsDir() bool       { return false }
func (f fakeFileInfo) Sys() any          { return nil }

func TestExecutableDir_ValidPath(t *testing.T) {
	withMocks(t, mockExecutable(`C:\Program Files\SpeechKit\SpeechKit.exe`, nil), nil, nil)
	got := ExecutableDir()
	want := filepath.Dir(`C:\Program Files\SpeechKit\SpeechKit.exe`)
	if got != want {
		t.Errorf("ExecutableDir() = %q, want %q", got, want)
	}
}

func TestExecutableDir_Error(t *testing.T) {
	withMocks(t, mockExecutable("", errors.New("boom")), nil, nil)
	if got := ExecutableDir(); got != "" {
		t.Errorf("ExecutableDir() on error = %q, want empty", got)
	}
}

func TestExecutableDir_EmptyPath(t *testing.T) {
	withMocks(t, mockExecutable("   ", nil), nil, nil)
	if got := ExecutableDir(); got != "" {
		t.Errorf("ExecutableDir() on whitespace path = %q, want empty", got)
	}
}

func TestIsPortable_PortableMarkers(t *testing.T) {
	exe := `C:\portable\SpeechKit.exe`
	exeDir := filepath.Dir(exe)
	tests := []struct {
		name   string
		marker string
	}{
		{"config.toml", "config.toml"},
		{"config.default.toml", "config.default.toml"},
		{"whisper-server.exe", "whisper-server.exe"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			withMocks(t,
				mockExecutable(exe, nil),
				mockStat(map[string]bool{filepath.Join(exeDir, tc.marker): true}),
				nil,
			)
			if !IsPortable() {
				t.Errorf("IsPortable() with marker %q = false, want true", tc.marker)
			}
		})
	}
}

func TestIsPortable_UninstallerBlocksPortable(t *testing.T) {
	exe := `C:\installed\SpeechKit.exe`
	exeDir := filepath.Dir(exe)
	// Uninstaller present + a portable marker: uninstaller wins.
	withMocks(t,
		mockExecutable(exe, nil),
		mockStat(map[string]bool{
			filepath.Join(exeDir, "uninstall.exe"): true,
			filepath.Join(exeDir, "config.toml"):   true,
		}),
		nil,
	)
	if IsPortable() {
		t.Error("IsPortable() with uninstall.exe present = true, want false")
	}
}

func TestIsPortable_NoMarkers(t *testing.T) {
	withMocks(t,
		mockExecutable(`C:\some\path\SpeechKit.exe`, nil),
		mockStat(map[string]bool{}),
		nil,
	)
	if IsPortable() {
		t.Error("IsPortable() with no markers = true, want false")
	}
}

func TestIsPortable_NoExecutableDir(t *testing.T) {
	withMocks(t, mockExecutable("", errors.New("boom")), mockStat(map[string]bool{}), nil)
	if IsPortable() {
		t.Error("IsPortable() with no exe dir = true, want false")
	}
}

func TestDataDir_Portable(t *testing.T) {
	exe := `C:\portable\SpeechKit.exe`
	exeDir := filepath.Dir(exe)
	withMocks(t,
		mockExecutable(exe, nil),
		mockStat(map[string]bool{filepath.Join(exeDir, "config.toml"): true}),
		nil,
	)
	want := filepath.Join(exeDir, "data")
	if got := DataDir(); got != want {
		t.Errorf("DataDir() portable = %q, want %q", got, want)
	}
}

func TestDataDir_InstalledUsesAppData(t *testing.T) {
	t.Setenv("APPDATA", `C:\Users\test\AppData\Roaming`)
	withMocks(t,
		mockExecutable(`C:\install\SpeechKit.exe`, nil),
		mockStat(map[string]bool{}),
		nil,
	)
	want := filepath.Join(`C:\Users\test\AppData\Roaming`, "SpeechKit") //nolint:gocritic // filepathJoin: Windows path literal in test — intentional
	if got := DataDir(); got != want {
		t.Errorf("DataDir() installed = %q, want %q", got, want)
	}
}

func TestDataDir_NoAppDataFallsBackToCwd(t *testing.T) {
	t.Setenv("APPDATA", "")
	withMocks(t,
		mockExecutable(`C:\install\SpeechKit.exe`, nil),
		mockStat(map[string]bool{}),
		nil,
	)
	want := filepath.Join(".", "SpeechKit")
	if got := DataDir(); got != want {
		t.Errorf("DataDir() with empty APPDATA = %q, want %q", got, want)
	}
}

func TestIsPortable_DevWorkspaceBinaryIsNotPortable(t *testing.T) {
	exe := `C:\Users\dev\kombify-SpeechKit\SpeechKit.exe`
	exeDir := filepath.Dir(exe)
	withMocks(t,
		mockExecutable(exe, nil),
		mockStat(map[string]bool{
			filepath.Join(exeDir, "config.toml"):                  true,
			filepath.Join(exeDir, "SpeechKit.exe"):                true,
			filepath.Join(exeDir, "go.mod"):                       true,
			filepath.Join(exeDir, "frontend", "app", "package.json"): true,
		}),
		nil,
	)
	if IsPortable() {
		t.Fatal("IsPortable() = true for a dev workspace binary, want false")
	}
}

func TestConfigFilePath_PortableUsesExecutableDir(t *testing.T) {
	exe := `C:\portable\SpeechKit.exe`
	exeDir := filepath.Dir(exe)
	withMocks(t,
		mockExecutable(exe, nil),
		mockStat(map[string]bool{filepath.Join(exeDir, "whisper-server.exe"): true}),
		nil,
	)
	want := filepath.Join(exeDir, "config.toml")
	if got := ConfigFilePath(); got != want {
		t.Fatalf("ConfigFilePath() = %q, want %q", got, want)
	}
}

func TestConfigFilePath_NonPortableUsesDataDir(t *testing.T) {
	t.Setenv("APPDATA", `C:\Users\test\AppData\Roaming`)
	withMocks(t,
		mockExecutable(`C:\Users\test\AppData\Local\Programs\SpeechKit\SpeechKit.exe`, nil),
		mockStat(map[string]bool{}),
		nil,
	)
	want := filepath.Join(`C:\Users\test\AppData\Roaming`, "SpeechKit", "config.toml")
	if got := ConfigFilePath(); got != want {
		t.Fatalf("ConfigFilePath() = %q, want %q", got, want)
	}
}

func TestLocalDataDir_PortableMatchesDataDir(t *testing.T) {
	exe := `C:\portable\SpeechKit.exe`
	exeDir := filepath.Dir(exe)
	withMocks(t,
		mockExecutable(exe, nil),
		mockStat(map[string]bool{filepath.Join(exeDir, "config.toml"): true}),
		nil,
	)
	if got, want := LocalDataDir(), DataDir(); got != want {
		t.Errorf("LocalDataDir() portable = %q, want DataDir() %q", got, want)
	}
}

func TestLocalDataDir_InstalledUsesLocalAppData(t *testing.T) {
	t.Setenv("LOCALAPPDATA", `C:\Users\test\AppData\Local`)
	withMocks(t,
		mockExecutable(`C:\install\SpeechKit.exe`, nil),
		mockStat(map[string]bool{}),
		nil,
	)
	want := filepath.Join(`C:\Users\test\AppData\Local`, "SpeechKit") //nolint:gocritic // filepathJoin: Windows path literal in test — intentional
	if got := LocalDataDir(); got != want {
		t.Errorf("LocalDataDir() = %q, want %q", got, want)
	}
}

func TestLocalDataDir_NoLocalAppDataFallsBackToDataDir(t *testing.T) {
	t.Setenv("LOCALAPPDATA", "")
	t.Setenv("APPDATA", `C:\Users\test\AppData\Roaming`)
	withMocks(t,
		mockExecutable(`C:\install\SpeechKit.exe`, nil),
		mockStat(map[string]bool{}),
		nil,
	)
	if got, want := LocalDataDir(), DataDir(); got != want {
		t.Errorf("LocalDataDir() with no LOCALAPPDATA = %q, want DataDir() %q", got, want)
	}
}

func TestSecretsDir_PortableUnderData(t *testing.T) {
	exe := `C:\portable\SpeechKit.exe`
	exeDir := filepath.Dir(exe)
	withMocks(t,
		mockExecutable(exe, nil),
		mockStat(map[string]bool{filepath.Join(exeDir, "config.toml"): true}),
		nil,
	)
	want := filepath.Join(exeDir, "data", "secrets")
	if got := SecretsDir(); got != want {
		t.Errorf("SecretsDir() portable = %q, want %q", got, want)
	}
}

func TestSecretsDir_InstalledUsesUserConfigDir(t *testing.T) {
	withMocks(t,
		mockExecutable(`C:\install\SpeechKit.exe`, nil),
		mockStat(map[string]bool{}),
		func() (string, error) { return `C:\Users\test\AppData\Roaming`, nil },
	)
	want := filepath.Join(`C:\Users\test\AppData\Roaming`, "SpeechKit", "secrets") //nolint:gocritic // filepathJoin: Windows path literal in test — intentional
	if got := SecretsDir(); got != want {
		t.Errorf("SecretsDir() installed = %q, want %q", got, want)
	}
}

func TestSecretsDir_UserConfigDirErrorFallsBackToDataDir(t *testing.T) {
	t.Setenv("APPDATA", `C:\Users\test\AppData\Roaming`)
	withMocks(t,
		mockExecutable(`C:\install\SpeechKit.exe`, nil),
		mockStat(map[string]bool{}),
		func() (string, error) { return "", errors.New("no config dir") },
	)
	want := filepath.Join(DataDir(), "secrets")
	if got := SecretsDir(); got != want {
		t.Errorf("SecretsDir() on UserConfigDir err = %q, want %q", got, want)
	}
}

func TestSecretsDir_UserConfigDirEmptyFallsBackToDataDir(t *testing.T) {
	t.Setenv("APPDATA", `C:\Users\test\AppData\Roaming`)
	withMocks(t,
		mockExecutable(`C:\install\SpeechKit.exe`, nil),
		mockStat(map[string]bool{}),
		func() (string, error) { return "  ", nil },
	)
	want := filepath.Join(DataDir(), "secrets")
	if got := SecretsDir(); got != want {
		t.Errorf("SecretsDir() on empty UserConfigDir = %q, want %q", got, want)
	}
}

func TestPathExists_EmptyString(t *testing.T) {
	if pathExists("   ") {
		t.Error("pathExists(whitespace) = true, want false")
	}
}

func TestPathExists_StatError(t *testing.T) {
	withMocks(t, nil, func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }, nil)
	if pathExists(`C:\missing`) {
		t.Error("pathExists missing path = true, want false")
	}
}

func TestPathExists_Success(t *testing.T) {
	withMocks(t, nil, mockStat(map[string]bool{`C:\present`: true}), nil)
	if !pathExists(`C:\present`) {
		t.Error("pathExists present path = false, want true")
	}
}
