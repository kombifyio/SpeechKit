package secrets

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// DefaultDopplerSecretLookup runs the Doppler CLI to retrieve a single secret value.
// It hides the console window on Windows to avoid terminal flashes in GUI mode.
func DefaultDopplerSecretLookup(dopplerPath, key, project, cfg string) (string, error) {
	cmd := exec.Command( //nolint:gosec,noctx // G204: dopplerPath is app-controlled binary; no context param in this public API
		dopplerPath, "secrets", "get", key,
		"--plain",
		"--project", project,
		"--config", cfg,
		"--no-read-env",
	)
	// Suppress console window in Wails/GUI context.
	configureDopplerCommand(cmd)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// FindDopplerExecutable locates the doppler CLI binary.
// lookPath is injected so callers (and tests) can substitute exec.LookPath.
// Checks DOPPLER_PATH env override, PATH, and common Windows installation directories.
func FindDopplerExecutable(lookPath func(string) (string, error)) string {
	if custom := strings.TrimSpace(os.Getenv("DOPPLER_PATH")); custom != "" && dopplerFileExists(custom) {
		return custom
	}

	if resolved, err := lookPath("doppler"); err == nil && strings.TrimSpace(resolved) != "" {
		return resolved
	}

	candidates := []string{
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "WinGet", "Links", "doppler.exe"),
		filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Local", "Microsoft", "WinGet", "Links", "doppler.exe"),
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Doppler", "doppler.exe"),
		filepath.Join(os.Getenv("ProgramFiles"), "Doppler", "doppler.exe"),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Doppler", "doppler.exe"),
	}

	for _, candidate := range candidates {
		if dopplerFileExists(candidate) {
			return candidate
		}
	}

	return ""
}

func dopplerFileExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path) //nolint:gosec // G703: path is Doppler binary location from app config
	return err == nil && !info.IsDir()
}
