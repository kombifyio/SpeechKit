package router

// These tests cover the host-side audit wiring this shim owns: the public
// router (pkg/speechkit/stt) emits through the provider-selected observer,
// and this package's init installs the observer that writes
// provider.selected audit events. Routing behavior itself is tested in
// pkg/speechkit/stt.

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/internal/auditlog"
	"github.com/kombifyio/SpeechKit/internal/auditlogtest"
	"github.com/kombifyio/SpeechKit/internal/stt"
)

type mockProvider struct {
	name     string
	text     string
	failNext bool
}

func (m *mockProvider) Transcribe(_ context.Context, _ []byte, opts stt.TranscribeOpts) (*stt.Result, error) {
	if m.failNext {
		return nil, fmt.Errorf("mock %s failure", m.name)
	}
	return &stt.Result{Text: m.text, Provider: m.name, Language: opts.Language}, nil
}

func (m *mockProvider) Name() string                   { return m.name }
func (m *mockProvider) Health(_ context.Context) error { return nil }

// localProbeAddr returns a listening TCP address so the router's internet
// probe deterministically reports online without leaving the machine.
func localProbeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	return ln.Addr().String()
}

func TestRouterEmitsProviderSelectedAuditLocalOnly(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(auditlogtest.Reset) // must run before TempDir cleanup (LIFO: registered after TempDir)
	auditlog.Configure(true, dir, 90, false)

	r := &Router{Strategy: StrategyLocalOnly}
	r.SetLocal(&mockProvider{name: "local", text: "hello"})
	_, err := r.Route(context.Background(), []byte("audio"), 1.0, stt.TranscribeOpts{})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}

	dateKey := time.Now().UTC().Format("2006-01-02")
	data, readErr := os.ReadFile(filepath.Join(dir, "audit-"+dateKey+".log"))
	if readErr != nil {
		t.Fatalf("read audit log: %v", readErr)
	}
	if !strings.Contains(string(data), `"event":"provider.selected"`) {
		t.Errorf("want provider.selected event in audit log, got:\n%s", data)
	}
	if !strings.Contains(string(data), `"provider_name":"local"`) {
		t.Errorf("want provider_name=local in audit log, got:\n%s", data)
	}
}

func TestRouterEmitsProviderSelectedAuditCloudOnly(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(auditlogtest.Reset) // must run before TempDir cleanup (LIFO: registered after TempDir)
	auditlog.Configure(true, dir, 90, false)

	r := &Router{Strategy: StrategyCloudOnly}
	r.AddCloud(&mockProvider{name: "hf", text: "cloud result"})
	_, err := r.Route(context.Background(), []byte("audio"), 1.0, stt.TranscribeOpts{})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}

	dateKey := time.Now().UTC().Format("2006-01-02")
	data, readErr := os.ReadFile(filepath.Join(dir, "audit-"+dateKey+".log"))
	if readErr != nil {
		t.Fatalf("read audit log: %v", readErr)
	}
	if !strings.Contains(string(data), `"event":"provider.selected"`) {
		t.Errorf("want provider.selected event in audit log, got:\n%s", data)
	}
	if !strings.Contains(string(data), `"provider_name":"hf"`) {
		t.Errorf("want provider_name=hf in audit log, got:\n%s", data)
	}
}

func TestRouterEmitsProviderSelectedOnDynamicFallback(t *testing.T) {
	// online + all cloud providers fail + local succeeds. Verifies that the
	// provider.selected audit event is emitted even when the dynamic strategy
	// reaches its local-fallback path.
	dir := t.TempDir()
	t.Cleanup(auditlogtest.Reset) // LIFO: runs before TempDir cleanup
	auditlog.Configure(true, dir, 90, false)

	// Long audio (>PreferLocalUnderSecs) so local isn't preferred up front and
	// routing tries cloud first, then falls back to local.
	r := &Router{
		Strategy:             StrategyDynamic,
		PreferLocalUnderSecs: 10,
		ConnectivityProbe:    localProbeAddr(t),
	}
	r.SetLocal(&mockProvider{name: "local-fallback", text: "local ok"})
	r.AddCloud(&mockProvider{name: "cloud-fail", failNext: true})

	_, err := r.Route(context.Background(), []byte("audio"), 30.0, stt.TranscribeOpts{})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}

	dateKey := time.Now().UTC().Format("2006-01-02")
	data, readErr := os.ReadFile(filepath.Join(dir, "audit-"+dateKey+".log"))
	if readErr != nil {
		t.Fatalf("read audit log: %v", readErr)
	}
	if !strings.Contains(string(data), `"event":"provider.selected"`) {
		t.Errorf("want provider.selected event in audit log for dynamic fallback, got:\n%s", data)
	}
	if !strings.Contains(string(data), `"provider_name":"local-fallback"`) {
		t.Errorf("want provider_name=local-fallback in audit log for dynamic fallback, got:\n%s", data)
	}
}

func TestRouterDoesNotEmitProviderSelectedOnFailure(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(auditlogtest.Reset) // must run before TempDir cleanup (LIFO: registered after TempDir)
	auditlog.Configure(true, dir, 90, false)

	r := &Router{Strategy: StrategyCloudOnly}
	r.AddCloud(&mockProvider{name: "hf", failNext: true})
	_, _ = r.Route(context.Background(), []byte("audio"), 1.0, stt.TranscribeOpts{})

	dateKey := time.Now().UTC().Format("2006-01-02")
	logPath := filepath.Join(dir, "audit-"+dateKey+".log")
	data, readErr := os.ReadFile(logPath)
	if os.IsNotExist(readErr) {
		return // no log file = no events written, which is correct
	}
	if readErr != nil {
		t.Fatalf("read audit log: %v", readErr)
	}
	if strings.Contains(string(data), `"event":"provider.selected"`) {
		t.Errorf("must not emit provider.selected on failure, got:\n%s", data)
	}
}
