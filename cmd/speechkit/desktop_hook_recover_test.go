package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

// TestRecoverHookSuppressesPanic verifies that a deferred recoverHook
// call prevents a panicking callback from propagating, so a single
// faulty Wails event-loop hook cannot crash the desktop event loop.
func TestRecoverHookSuppressesPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic propagated past recoverHook: %v", r)
		}
	}()

	func() {
		defer recoverHook("test_hook")
		panic("boom")
	}()
}

// TestRecoverHookNoPanicIsNoop confirms recoverHook is safe to defer in
// every callback even when the body completes normally.
func TestRecoverHookNoPanicIsNoop(t *testing.T) {
	called := false
	func() {
		defer recoverHook("test_hook_noop")
		called = true
	}()
	if !called {
		t.Fatal("body did not execute")
	}
}

// TestRecoverHookLogsVersionAndGoRuntime asserts that the panic-attribution
// log carries version + go_version fields so a support bundle can map a
// field crash to a specific release without grep-walking the rest of the
// log file. The hook name + raw panic value + stack remain too.
func TestRecoverHookLogsVersionAndGoRuntime(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	prevVersion := AppVersion
	AppVersion = "0.99.99-recover-test"
	t.Cleanup(func() { AppVersion = prevVersion })

	func() {
		defer recoverHook("triage_test_hook")
		panic("instrumented boom")
	}()

	var rec map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &rec); err != nil {
		t.Fatalf("recover log was not single-line JSON: %v\nbuf: %s", err, buf.String())
	}
	if got := rec["msg"]; got != "desktop.hook.panic" {
		t.Errorf("msg = %q, want desktop.hook.panic", got)
	}
	if got := rec["hook"]; got != "triage_test_hook" {
		t.Errorf("hook = %v, want triage_test_hook", got)
	}
	if got := rec["version"]; got != "0.99.99-recover-test" {
		t.Errorf("version = %v, want 0.99.99-recover-test", got)
	}
	if got, ok := rec["go_version"].(string); !ok || !strings.HasPrefix(got, "go") {
		t.Errorf("go_version = %v, want a go-prefixed runtime string", rec["go_version"])
	}
	if got := rec["panic"]; got != "instrumented boom" {
		t.Errorf("panic = %v, want \"instrumented boom\"", got)
	}
	if got, ok := rec["stack"].(string); !ok || !strings.Contains(got, "goroutine") {
		t.Errorf("stack = %v, want a goroutine dump", rec["stack"])
	}
}
