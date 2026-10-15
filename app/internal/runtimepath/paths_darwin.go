//go:build darwin

package runtimepath

import (
	"path/filepath"
	"strings"
)

// macOS keeps everything under ~/Library/Application Support/<DataDirName>.
// There is no roaming/local split, and models deliberately do not go to
// ~/Library/Caches: that directory is purgeable and a multi-gigabyte
// whisper model must not vanish under memory pressure. When the home
// directory cannot be resolved the environment-based layout is the
// fallback, so a launchd or CI context with HOME unset still gets a
// deterministic path.

// userHomeDir (runtimepath.go) is the seam tests replace.

func applicationSupportDir(name string) string {
	home, err := userHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, "Library", "Application Support", name)
}

func platformDataDir(name string) string {
	if dir := applicationSupportDir(name); dir != "" {
		return dir
	}
	return envDataDir(name)
}

func platformLocalDataDir(name string) string {
	if dir := applicationSupportDir(name); dir != "" {
		return dir
	}
	return envLocalDataDir(name)
}

func platformContentDir(name string) string  { return platformDataDir(name) }
func platformLegacyContentDir(string) string { return "" }

func platformSecretsDir(name string) string {
	return filepath.Join(platformDataDir(name), "secrets")
}

// platformAllowsPortable refuses portable mode inside an app bundle: writing
// data next to the binary would land in SpeechKit.app/Contents/MacOS and
// break the bundle's code signature.
func platformAllowsPortable(exeDir string) bool {
	return BundleDirFor(exeDir) == ""
}

// BundleDir returns the SpeechKit.app directory the running binary lives in,
// or "" when it runs outside a bundle (a `go build` output, the test binary).
func BundleDir() string {
	return BundleDirFor(ExecutableDir())
}

// BundleDirFor maps an executable directory of the form
// …/SpeechKit.app/Contents/MacOS to the …/SpeechKit.app directory.
func BundleDirFor(exeDir string) string {
	exeDir = strings.TrimSpace(exeDir)
	if exeDir == "" || filepath.Base(exeDir) != "MacOS" {
		return ""
	}
	contents := filepath.Dir(exeDir)
	if filepath.Base(contents) != "Contents" {
		return ""
	}
	bundle := filepath.Dir(contents)
	if !strings.HasSuffix(strings.ToLower(bundle), ".app") {
		return ""
	}
	return bundle
}

// BundleResourcesDir is Contents/Resources (config.default.toml, models,
// icons) or "" outside a bundle.
func BundleResourcesDir() string {
	bundle := BundleDir()
	if bundle == "" {
		return ""
	}
	return filepath.Join(bundle, "Contents", "Resources")
}

// BundleHelpersDir is Contents/Helpers (whisper-server and later sidecars)
// or "" outside a bundle.
func BundleHelpersDir() string {
	bundle := BundleDir()
	if bundle == "" {
		return ""
	}
	return filepath.Join(bundle, "Contents", "Helpers")
}

// IsTranslocated reports whether Gatekeeper launched the bundle from a
// read-only App Translocation mount (a quarantined app opened from
// ~/Downloads). The in-app updater refuses to swap such a bundle.
func IsTranslocated() bool {
	return strings.Contains(ExecutableDir(), "/AppTranslocation/")
}
