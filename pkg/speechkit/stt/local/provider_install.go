package local

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// whisperModelPattern restricts whisper.cpp model filenames to the
// ggml-<variant>.bin naming convention. This blocks attempts to load
// arbitrary binaries or paths containing shell metacharacters.
var whisperModelPattern = regexp.MustCompile(`^ggml-[A-Za-z0-9._\-]+\.bin$`)

// ValidateModelPath verifies that path points at a whisper.cpp ggml model
// file with a safe filename. It rejects path traversal, non-absolute paths,
// and filenames that don't match the ggml-*.bin pattern.
func ValidateModelPath(path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("whisper: model path is empty")
	}
	clean := filepath.Clean(path)
	if clean != path && filepath.ToSlash(clean) != filepath.ToSlash(path) {
		// filepath.Clean collapses ../ and double separators. A change
		// means the caller supplied something suspicious.
		return fmt.Errorf("whisper: model path must be in canonical form (got %q, want %q)", path, clean)
	}
	if strings.Contains(filepath.ToSlash(clean), "../") {
		return fmt.Errorf("whisper: model path must not contain .. traversal: %s", clean)
	}
	if !filepath.IsAbs(clean) {
		return fmt.Errorf("whisper: model path must be absolute: %s", clean)
	}
	base := filepath.Base(clean)
	if !whisperModelPattern.MatchString(base) {
		return fmt.Errorf("whisper: model filename %q does not match ggml-*.bin pattern", base)
	}
	return nil
}

// InstallStatus describes what's present and what's missing for local STT.
type InstallStatus struct {
	BinaryFound bool     `json:"binaryFound"`
	BinaryPath  string   `json:"binaryPath"`
	ModelFound  bool     `json:"modelFound"`
	ModelPath   string   `json:"modelPath"`
	ModelBytes  int64    `json:"modelBytes"`
	ServerReady bool     `json:"serverReady"`
	Problems    []string `json:"problems,omitempty"`
}

// MinWhisperModelBytes is the minimum file size we expect for a valid ggml model.
// ggml-base.bin is ~150 MB; anything under 50 MB is clearly corrupt/truncated.
const MinWhisperModelBytes = 50_000_000

// VerifyInstallation checks binary and model availability without starting the server.
func (p *Provider) VerifyInstallation() InstallStatus {
	status := InstallStatus{
		ModelPath:   p.ModelPath,
		ServerReady: p.ready.Load(),
	}

	// Check binary.
	binaryPath, err := findWhisperBinary()
	if err != nil {
		status.Problems = append(status.Problems, "whisper-server binary not found")
	} else {
		status.BinaryFound = true
		status.BinaryPath = binaryPath
	}

	// Check model file.
	if p.ModelPath == "" {
		status.Problems = append(status.Problems, "no model path configured")
	} else if err := ValidateModelPath(p.ModelPath); err != nil {
		status.Problems = append(status.Problems, err.Error())
	} else if fi, err := os.Stat(p.ModelPath); err != nil {
		status.Problems = append(status.Problems, fmt.Sprintf("model file missing: %s", p.ModelPath))
	} else {
		status.ModelBytes = fi.Size()
		if fi.Size() < MinWhisperModelBytes {
			status.Problems = append(status.Problems, fmt.Sprintf("model file too small (%d bytes) — likely corrupt or truncated", fi.Size()))
		} else if err := verifyReadableModelFile(p.ModelPath); err != nil {
			status.Problems = append(status.Problems, fmt.Sprintf("model file not readable: %s (%v)", p.ModelPath, err))
		} else {
			status.ModelFound = true
		}
	}

	return status
}

func verifyReadableModelFile(path string) error {
	file, err := os.Open(path) // #nosec G304 -- path is validated by ValidateModelPath before this helper is called.
	if err != nil {
		return err
	}
	return file.Close()
}

// FindWhisperBinary exposes the local whisper runtime lookup for callers that
// need to reflect runtime readiness without starting the subprocess.
func FindWhisperBinary() (string, error) {
	return findWhisperBinary()
}

// findWhisperBinary looks for the whisper-server executable in standard locations.
//
// Per the kernel/adapter discipline in CLAUDE.md, this kernel function
// must stay platform-neutral. Windows-specific binary names and
// install locations live in local_search_windows.go;
// local_search_unix.go is the no-op fallback for Linux/macOS where
// the Server-Target reads its whisper path from server settings.
func findWhisperBinary() (string, error) {
	names := whisperBinaryNames()

	// Check next to executable first (trusted bundle path).
	exe, _ := os.Executable()
	if exe != "" {
		for _, name := range names {
			path := filepath.Join(filepath.Dir(exe), name)
			if _, err := os.Stat(path); err == nil { //nolint:gosec // G703: path is app data dir, not user input
				return path, nil
			}
		}
	}

	// Check platform-specific managed install locations.
	for _, dir := range platformWhisperSearchDirs() {
		for _, name := range names {
			path := filepath.Join(dir, name)
			if _, err := os.Stat(path); err == nil { // #nosec G703 -- path is app data dir, not user input.
				return path, nil
			}
		}
	}

	// Optional developer escape hatch: allow PATH lookup explicitly.
	if strings.EqualFold(strings.TrimSpace(os.Getenv("SPEECHKIT_ALLOW_WHISPER_PATH")), "1") {
		for _, name := range names {
			if path, err := exec.LookPath(name); err == nil {
				slog.Warn("using whisper-server from PATH due to SPEECHKIT_ALLOW_WHISPER_PATH=1", "path", path)
				return path, nil
			}
		}
	}

	return "", fmt.Errorf("whisper-server binary not found in bundle or managed install location")
}
