package auditlog

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAppendEventNoOpWhenDisabled(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(ResetForTests)
	Configure(false, dir, 90, false)

	err := AppendEvent(context.Background(), Record{
		Event:   EventProviderSelected,
		Actor:   Actor{UserSID: "S-1-5-21-test"},
		Outcome: OutcomeSuccess,
	})
	if err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("want no audit file when disabled, got %d entries", len(entries))
	}
}

func TestAppendEventWritesValidJSON(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(ResetForTests)
	Configure(true, dir, 90, false)

	err := AppendEvent(context.Background(), Record{
		Event: EventProviderSelected,
		Actor: Actor{UserSID: "S-1-5-21-test", SessionID: "session-abc"},
		Resource: map[string]any{
			"provider_name": "whisper-cpp",
			"provider_kind": "stt",
		},
		Outcome: OutcomeSuccess,
		TraceID: "01HX...",
	})
	if err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	dateKey := time.Now().UTC().Format("2006-01-02")
	path := filepath.Join(dir, "audit-"+dateKey+".log")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		t.Fatalf("want at least one line, got EOF")
	}
	var got Record
	if err := json.Unmarshal(scanner.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.SchemaVersion != "1" {
		t.Errorf("SchemaVersion: want 1, got %q", got.SchemaVersion)
	}
	if got.Event != EventProviderSelected {
		t.Errorf("Event: want %q, got %q", EventProviderSelected, got.Event)
	}
	if got.Actor.UserSID != "S-1-5-21-test" {
		t.Errorf("Actor.UserSID mismatch: %q", got.Actor.UserSID)
	}
	if got.Resource["provider_name"] != "whisper-cpp" {
		t.Errorf("Resource.provider_name mismatch: %v", got.Resource["provider_name"])
	}
	if got.Outcome != OutcomeSuccess {
		t.Errorf("Outcome: want %q, got %q", OutcomeSuccess, got.Outcome)
	}
}

func TestAppendEventRollsToNewDayFile(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(ResetForTests)
	Configure(true, dir, 90, false)

	yesterday := time.Now().UTC().AddDate(0, 0, -1)
	today := time.Now().UTC()

	for _, ts := range []time.Time{yesterday, today} {
		err := AppendEvent(context.Background(), Record{
			Timestamp: ts,
			Event:     EventAuthFailed,
			Outcome:   OutcomeFailure,
		})
		if err != nil {
			t.Fatalf("AppendEvent: %v", err)
		}
	}

	entries, _ := os.ReadDir(dir)
	count := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "audit-") {
			count++
		}
	}
	if count != 2 {
		t.Errorf("want 2 audit files (yesterday + today), got %d", count)
	}
}

func TestPruneOldAuditFiles(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(ResetForTests)
	Configure(true, dir, 2, false) // 2-day retention

	oldDate := time.Now().UTC().AddDate(0, 0, -5).Format("2006-01-02")
	oldPath := filepath.Join(dir, "audit-"+oldDate+".log")
	if err := os.WriteFile(oldPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("seed old file: %v", err)
	}

	err := AppendEvent(context.Background(), Record{Event: EventAuthFailed})
	if err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Errorf("want old file pruned, still exists: %v", err)
	}
}

func TestConfigureRequiredBeforeAppend(t *testing.T) {
	t.Cleanup(ResetForTests)
	// Do NOT call Configure; bypass the disabled-shortcircuit manually
	mu.Lock()
	enabled = true
	logDir = ""
	mu.Unlock()

	err := AppendEvent(context.Background(), Record{Event: EventAuthFailed})
	if err == nil {
		t.Errorf("want error when Configure not called, got nil")
	}
}
