package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"runtime"
	"strings"
	"testing"
)

// TestStartupTrackerLogsVersionAndGoRuntime asserts that every stage event
// emitted by the startup tracker carries the version + go_version fields.
// A single log line from a customer support bundle should be enough to
// attribute a startup failure to a specific release without grep-walking
// back to find the bootstrap banner.
func TestStartupTrackerLogsVersionAndGoRuntime(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	prevVersion := AppVersion
	AppVersion = "0.99.99-test"
	t.Cleanup(func() { AppVersion = prevVersion })

	tracker := newStartupTracker()
	tracker.stage("entered", "pid", 4242)
	tracker.stage("config_loaded", "path", "C:/test/config.toml")

	lines := splitNonEmpty(buf.String())
	if len(lines) != 2 {
		t.Fatalf("want 2 stage events, got %d: %s", len(lines), buf.String())
	}

	for i, line := range lines {
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("event %d: not valid JSON: %v (line=%q)", i, err, line)
		}
		if got := rec["msg"]; got != "desktop.startup" {
			t.Errorf("event %d: msg = %q, want desktop.startup", i, got)
		}
		if got := rec["version"]; got != "0.99.99-test" {
			t.Errorf("event %d: version = %v, want 0.99.99-test", i, got)
		}
		if got, ok := rec["go_version"].(string); !ok || !strings.HasPrefix(got, "go") {
			t.Errorf("event %d: go_version = %v, want a go-prefixed runtime string", i, rec["go_version"])
		}
		if _, ok := rec["elapsed_ms"]; !ok {
			t.Errorf("event %d: missing elapsed_ms", i)
		}
		if _, ok := rec["delta_ms"]; !ok {
			t.Errorf("event %d: missing delta_ms", i)
		}
	}
}

// TestStartupTrackerCapturesCurrentGoRuntime confirms newStartupTracker
// reads the running Go version. Pinning the literal would brittle-break
// every Go upgrade; assert prefix + that it is non-empty.
func TestStartupTrackerCapturesCurrentGoRuntime(t *testing.T) {
	tracker := newStartupTracker()
	if tracker.goRuntime != runtime.Version() {
		t.Errorf("goRuntime = %q, want %q", tracker.goRuntime, runtime.Version())
	}
}

func splitNonEmpty(s string) []string {
	out := []string{}
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}
