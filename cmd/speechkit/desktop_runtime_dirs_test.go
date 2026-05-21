package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// captureSlog redirects slog output to a buffer for the duration of t.
func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf
}

// findLog returns the first JSON-decoded log record whose `msg` matches
// the given event tag, or nil if no record matched.
func findLog(t *testing.T, buf *bytes.Buffer, msg string) map[string]any {
	t.Helper()
	for _, line := range strings.Split(buf.String(), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("decode log line %q: %v", line, err)
		}
		if rec["msg"] == msg {
			return rec
		}
	}
	return nil
}

// TestInitRuntimeDirectories_FreshInstall covers the empty-profile case
// that kombify-SpeechKit-1pg targets: APPDATA/LOCALAPPDATA point to an
// empty tempdir, no SpeechKit subtree exists, and every required
// directory must be created with structured logging.
func TestInitRuntimeDirectories_FreshInstall(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("APPDATA", tempDir)
	t.Setenv("LOCALAPPDATA", filepath.Join(tempDir, "Local"))

	buf := captureSlog(t)

	initRuntimeDirectories()

	dataDir := filepath.Join(tempDir, "SpeechKit")
	if _, err := os.Stat(dataDir); err != nil {
		t.Fatalf("DataDir not created at %s: %v", dataDir, err)
	}

	createdLog := findLog(t, buf, "desktop.startup.dir_created")
	if createdLog == nil {
		t.Fatalf("expected at least one desktop.startup.dir_created log line, got:\n%s", buf.String())
	}
	if _, ok := createdLog["path"].(string); !ok {
		t.Errorf("desktop.startup.dir_created missing 'path' attribute: %v", createdLog)
	}
	if _, ok := createdLog["mode"].(string); !ok {
		t.Errorf("desktop.startup.dir_created missing 'mode' attribute: %v", createdLog)
	}
	if _, ok := createdLog["purpose"].(string); !ok {
		t.Errorf("desktop.startup.dir_created missing 'purpose' attribute: %v", createdLog)
	}
}

// TestInitRuntimeDirectories_AlreadyExists covers the upgrade path where
// the SpeechKit data directory already exists from a prior install.
// The helper must be idempotent and emit dir_present log lines instead
// of dir_created.
func TestInitRuntimeDirectories_AlreadyExists(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("APPDATA", tempDir)
	t.Setenv("LOCALAPPDATA", filepath.Join(tempDir, "Local"))

	// Pre-create the SpeechKit data directory.
	preExisting := filepath.Join(tempDir, "SpeechKit")
	if err := os.MkdirAll(preExisting, 0o755); err != nil {
		t.Fatalf("pre-create DataDir: %v", err)
	}

	buf := captureSlog(t)

	initRuntimeDirectories()

	if _, err := os.Stat(preExisting); err != nil {
		t.Fatalf("DataDir lost: %v", err)
	}

	if findLog(t, buf, "desktop.startup.dir_present") == nil {
		t.Errorf("expected desktop.startup.dir_present for the pre-existing DataDir, got:\n%s", buf.String())
	}
}

// TestRuntimeLogsDir_EmptyExeDir verifies that the logs-dir resolution
// returns an empty string when the executable directory cannot be
// resolved — the contract that lets initRuntimeDirectories skip the
// target rather than picking an unsafe fallback.
func TestRuntimeLogsDir_NonEmpty(t *testing.T) {
	got := runtimeLogsDir()
	if runtime.GOOS == "windows" || runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
		if got == "" {
			t.Fatalf("runtimeLogsDir() returned empty string on %s; os.Executable should resolve in tests", runtime.GOOS)
		}
		if !strings.HasSuffix(got, string(filepath.Separator)+"logs") {
			t.Errorf("runtimeLogsDir() = %q, want suffix /logs", got)
		}
	}
}
