//go:build linux

package cli

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/config"
)

func TestSplitModesNormalizesAndDropsEmptyValues(t *testing.T) {
	got := splitModes(" Dictation, ,ASSIST, voiceagent ")
	want := []string{"dictation", "assist", "voiceagent"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("splitModes = %#v, want %#v", got, want)
	}
}

func TestEffectiveModesUsesConfiguredOrDefaultSet(t *testing.T) {
	if got := effectiveModes(nil); !reflect.DeepEqual(got, []string{"dictation", "assist", "voiceagent"}) {
		t.Fatalf("nil config modes = %#v", got)
	}

	cfg := &config.Config{}
	cfg.Server.Modes = []string{"voiceagent"}
	if got := effectiveModes(cfg); !reflect.DeepEqual(got, []string{"voiceagent"}) {
		t.Fatalf("configured modes = %#v", got)
	}
}

func TestParseLogLevel(t *testing.T) {
	tests := map[string]slog.Level{
		"":          slog.LevelInfo,
		"debug":     slog.LevelDebug,
		" warning ": slog.LevelWarn,
		"warn":      slog.LevelWarn,
		"error":     slog.LevelError,
		"unknown":   slog.LevelInfo,
	}
	for raw, want := range tests {
		if got := parseLogLevel(raw); got != want {
			t.Fatalf("parseLogLevel(%q) = %v, want %v", raw, got, want)
		}
	}
}

func TestDefaultConfigPathHonorsEnvironment(t *testing.T) {
	t.Setenv("SPEECHKIT_CONFIG_PATH", " /tmp/speechkit.toml ")
	if got := defaultConfigPath(); got != "/tmp/speechkit.toml" {
		t.Fatalf("defaultConfigPath = %q", got)
	}
}

func TestRunOnceVersionPrintsConfiguredVersion(t *testing.T) {
	restoreArgs := swapArgs(t, "speechkit-server", "--version")
	defer restoreArgs()
	stdout := captureStdout(t, func() {
		if code := runOnce(Options{Banner: "speechkit-server", Version: "0.28.0"}); code != 0 {
			t.Fatalf("runOnce code = %d, want 0", code)
		}
	})

	if got := strings.TrimSpace(stdout); got != "0.28.0" {
		t.Fatalf("stdout = %q, want version", got)
	}
}

func TestRunOnceReturnsConfigErrorWithoutStartingServer(t *testing.T) {
	restoreArgs := swapArgs(t, "speechkit-server", "--config", t.TempDir())
	defer restoreArgs()
	stderr := captureStderr(t, func() {
		if code := runOnce(Options{Banner: "speechkit-server", Version: "0.28.0"}); code != 2 {
			t.Fatalf("runOnce code = %d, want 2", code)
		}
	})

	if !strings.Contains(stderr, "load config") {
		t.Fatalf("stderr should mention config load failure, got %q", stderr)
	}
}

func swapArgs(t *testing.T, args ...string) func() {
	t.Helper()
	previous := os.Args
	os.Args = append([]string(nil), args...)
	return func() {
		os.Args = previous
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	return captureFD(t, &os.Stdout, fn)
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	return captureFD(t, &os.Stderr, fn)
}

func captureFD(t *testing.T, target **os.File, fn func()) string {
	t.Helper()
	previous := *target
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	*target = writer

	var buf bytes.Buffer
	done := make(chan error, 1)
	go func() {
		_, copyErr := io.Copy(&buf, reader)
		done <- copyErr
	}()

	fn()

	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	*target = previous
	if err := <-done; err != nil {
		t.Fatalf("read captured fd: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close reader: %v", err)
	}
	return buf.String()
}
