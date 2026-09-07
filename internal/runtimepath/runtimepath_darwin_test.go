//go:build darwin

package runtimepath

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func withHome(t *testing.T, home string, err error) {
	t.Helper()
	previous := userHomeDir
	userHomeDir = func() (string, error) { return home, err }
	t.Cleanup(func() { userHomeDir = previous })
}

func withExecutable(t *testing.T, path string) {
	t.Helper()
	previous := osExecutable
	osExecutable = func() (string, error) { return path, nil }
	t.Cleanup(func() { osExecutable = previous })
}

func TestDarwinDataDirsUseApplicationSupport(t *testing.T) {
	withHome(t, "/Users/tester", nil)
	withExecutable(t, "/Applications/SpeechKit.app/Contents/MacOS/SpeechKit")

	want := filepath.Join("/Users/tester", "Library", "Application Support", "SpeechKit")
	if got := DataDir(); got != want {
		t.Fatalf("DataDir() = %q, want %q", got, want)
	}
	if got := LocalDataDir(); got != want {
		t.Fatalf("LocalDataDir() = %q, want %q", got, want)
	}
	if got := ModelsDir(); got != filepath.Join(want, "models") {
		t.Fatalf("ModelsDir() = %q", got)
	}
	if got := SecretsDir(); got != filepath.Join(want, "secrets") {
		t.Fatalf("SecretsDir() = %q", got)
	}
	if got := ConfigFilePath(); got != filepath.Join(want, "config.toml") {
		t.Fatalf("ConfigFilePath() = %q", got)
	}
}

func TestDarwinNoHomeFallsBackToEnvironmentLayout(t *testing.T) {
	withHome(t, "", errors.New("no home"))
	withExecutable(t, "/tmp/build/speechkit")
	t.Setenv("APPDATA", "/tmp/appdata")
	t.Setenv("LOCALAPPDATA", "")

	if got, want := DataDir(), filepath.Join("/tmp/appdata", "SpeechKit"); got != want {
		t.Fatalf("DataDir() = %q, want %q", got, want)
	}
}

func TestDarwinBundleIsNeverPortable(t *testing.T) {
	bundle := filepath.Join(t.TempDir(), "SpeechKit.app")
	macOSDir := filepath.Join(bundle, "Contents", "MacOS")
	if err := os.MkdirAll(macOSDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A config next to the binary is the portable marker on Windows; inside
	// a bundle it must not switch the app to portable mode.
	if err := os.WriteFile(filepath.Join(macOSDir, "config.toml"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	withExecutable(t, filepath.Join(macOSDir, "SpeechKit"))
	withHome(t, t.TempDir(), nil)

	if IsPortable() {
		t.Fatal("IsPortable() = true inside an app bundle")
	}
	if got := BundleDir(); got != bundle {
		t.Fatalf("BundleDir() = %q, want %q", got, bundle)
	}
	if got := BundleHelpersDir(); got != filepath.Join(bundle, "Contents", "Helpers") {
		t.Fatalf("BundleHelpersDir() = %q", got)
	}
	if got := BundleResourcesDir(); got != filepath.Join(bundle, "Contents", "Resources") {
		t.Fatalf("BundleResourcesDir() = %q", got)
	}
}

func TestDarwinLooseBinaryWithConfigIsPortable(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	withExecutable(t, filepath.Join(dir, "speechkit"))
	withHome(t, t.TempDir(), nil)

	if !IsPortable() {
		t.Fatal("IsPortable() = false for a loose binary with config.toml next to it")
	}
	if got := DataDir(); got != filepath.Join(dir, "data") {
		t.Fatalf("DataDir() = %q", got)
	}
	if BundleDir() != "" {
		t.Fatalf("BundleDir() = %q for a loose binary", BundleDir())
	}
}

func TestBundleDirFor(t *testing.T) {
	cases := map[string]string{
		"":               "",
		"/usr/local/bin": "",
		"/Applications/SpeechKit.app/Contents/MacOS":                               "/Applications/SpeechKit.app",
		"/Applications/SpeechKit.app/Contents/Resources":                           "",
		"/private/var/folders/x/AppTranslocation/y/d/SpeechKit.app/Contents/MacOS": "/private/var/folders/x/AppTranslocation/y/d/SpeechKit.app",
	}
	for exeDir, want := range cases {
		if got := BundleDirFor(exeDir); got != want {
			t.Errorf("BundleDirFor(%q) = %q, want %q", exeDir, got, want)
		}
	}
}
