package codex

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/agentbridge"
)

// TestLiveAppServerHandshake talks to the REAL codex CLI on this machine:
// initialize handshake + thread/start (no model turn, no token cost). Gated
// behind SPEECHKIT_CODEX_LIVE=1 so CI and machines without a signed-in Codex
// skip it.
func TestLiveAppServerHandshake(t *testing.T) {
	if os.Getenv("SPEECHKIT_CODEX_LIVE") != "1" {
		t.Skip("set SPEECHKIT_CODEX_LIVE=1 to run against the real codex CLI")
	}
	bridge := New(Config{Mode: "app_server"})
	defer bridge.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	st := bridge.Status(ctx)
	if !st.Installed || st.Auth == agentbridge.AuthNone {
		t.Skipf("codex not usable here: %+v", st)
	}
	t.Logf("live codex: version=%q auth=%s plan=%q", st.Version, st.Auth, st.Plan)

	session, err := startAppServer(ctx, st.BinaryPath, "live-test", slog.Default(), func(agentbridge.Event) {})
	if err != nil {
		t.Fatalf("real app-server handshake failed: %v", err)
	}
	defer session.Close()

	// Not t.TempDir(): the killed codex child releases its cwd handle
	// asynchronously on Windows, which makes the strict TempDir cleanup
	// flake. Best-effort removal after a settle interval instead.
	cwd, err := os.MkdirTemp("", "sk-codex-live-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		session.Close()
		time.Sleep(500 * time.Millisecond)
		_ = os.RemoveAll(cwd)
	})

	threadID, err := session.StartThread(ctx, "", cwd, agentbridge.SandboxReadOnly)
	if err != nil {
		t.Fatalf("real thread/start failed: %v", err)
	}
	if threadID == "" {
		t.Fatal("real thread/start returned an empty thread id")
	}
	t.Logf("live thread started: %s", threadID)
}
