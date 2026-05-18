package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/internal/auditlog"
	"github.com/kombifyio/SpeechKit/internal/auditlogtest"
)

// TestVoiceAgentSessionEndEmitsIdleViaStateInactive verifies that
// handleVoiceAgentStateInactive emits a voiceagent.session.end event with
// terminated_by=idle when no other termination path has claimed the session,
// and that it clears the session state afterwards.
func TestVoiceAgentSessionEndEmitsIdleViaStateInactive(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(auditlogtest.Reset) // LIFO: Reset closes file handle before TempDir cleanup removes the dir
	auditlog.Configure(true, dir, 90, false)

	state := &appState{}
	state.voiceAgentSessionID = "va-idle-test"
	state.voiceAgentSessionStart = time.Now().Add(-30 * time.Second)
	state.voiceAgentTerminatedBy = "" // no other path claimed the session

	handleVoiceAgentStateInactive(context.Background(), state)

	dateKey := time.Now().UTC().Format("2006-01-02")
	data, err := os.ReadFile(filepath.Join(dir, "audit-"+dateKey+".log"))
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	if !strings.Contains(string(data), `"terminated_by":"idle"`) {
		t.Errorf("want terminated_by:idle in audit, got: %s", data)
	}

	// State must be cleared after the idle emit so a subsequent state
	// transition cannot double-emit.
	state.mu.Lock()
	remainingID := state.voiceAgentSessionID
	state.mu.Unlock()
	if remainingID != "" {
		t.Errorf("want voiceAgentSessionID cleared, still %q", remainingID)
	}
}

// TestVoiceAgentSessionEndSkipsIdleWhenAnotherPathClaimed verifies that
// handleVoiceAgentStateInactive does NOT emit any audit event when another
// termination path (user or error) has already set voiceAgentTerminatedBy.
func TestVoiceAgentSessionEndSkipsIdleWhenAnotherPathClaimed(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(auditlogtest.Reset) // LIFO: Reset closes file handle before TempDir cleanup removes the dir
	auditlog.Configure(true, dir, 90, false)

	state := &appState{}
	state.voiceAgentSessionID = "va-claimed-test"
	state.voiceAgentSessionStart = time.Now()
	state.voiceAgentTerminatedBy = "error" // error/user path already set this

	handleVoiceAgentStateInactive(context.Background(), state)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "audit-") {
			data, _ := os.ReadFile(filepath.Join(dir, e.Name()))
			t.Errorf("want no audit emission when terminatedBy=error, got: %s", data)
		}
	}
}
