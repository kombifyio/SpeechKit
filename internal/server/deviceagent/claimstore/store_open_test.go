package claimstore

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestPrivacyAndFilePermissions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	dir := filepath.Join(t.TempDir(), "private")
	path := filepath.Join(dir, "claims.db")
	ledger := openTestLedger(t, path, Options{})
	requestID := testUUIDV7(now, 14)
	commandText := "ultra-unique-private-command-52bd7d"
	pairingKey := []byte("ultra-unique-private-pairing-key-32-bytes-minimum")
	canonical := CanonicalRequest{
		PairedDeviceID: "pair-private",
		RequestID:      requestID,
		RuleID:         "private-light-off",
		Locale:         "en-US",
		Text:           commandText,
		EntityID:       "light.private",
		ExpectedState:  "off",
	}
	digest, err := HMACDigest(pairingKey, canonical)
	if err != nil {
		t.Fatal(err)
	}
	decision, err := ledger.Claim(ctx, Key{
		PairedDeviceID: canonical.PairedDeviceID,
		RequestID:      canonical.RequestID,
	}, digest, now)
	if err != nil {
		t.Fatal(err)
	}
	result := CompletedResult{
		Outcome:        OutcomeSuccess,
		ConversationID: "ha-conversation-private-1",
		SpeechText:     "The private command completed.",
		Language:       "en-US",
		ResponseType:   "action_done",
		ActionExecuted: ActionExecutedYes,
	}
	if err := ledger.Complete(ctx, decision.Handle, result, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatalf("checkpoint privacy fixture: %v", err)
	}
	if err := ledger.Close(); err != nil {
		t.Fatal(err)
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("database permissions = %#o, want 0600", got)
		}
		dirInfo, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if got := dirInfo.Mode().Perm(); got != 0o700 {
			t.Fatalf("new database directory permissions = %#o, want 0700", got)
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range [][]byte{[]byte(commandText), pairingKey} {
		if bytes.Contains(raw, forbidden) {
			t.Fatalf("database contains forbidden plaintext %q", forbidden)
		}
	}
	if !bytes.Contains(raw, []byte(result.SpeechText)) {
		t.Fatal("database does not contain the explicitly allow-listed replay speech")
	}
	if !bytes.Contains(raw, []byte(result.ConversationID)) {
		t.Fatal("database does not contain the explicitly allow-listed conversation id")
	}
}

func TestSQLiteDSNIsFileURI(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "claims with spaces.db")
	dsn := sqliteDSN(path)
	if !strings.HasPrefix(dsn, "file:") || !strings.Contains(dsn, "_txlock=immediate") {
		t.Fatalf("sqliteDSN(%q) = %q", path, dsn)
	}
	if strings.Contains(dsn, " ") {
		t.Fatalf("sqliteDSN contains an unescaped space: %q", dsn)
	}
}
