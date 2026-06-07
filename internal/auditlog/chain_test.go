package auditlog

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func readChain(t *testing.T, dir string) []Record {
	t.Helper()
	dateKey := time.Now().UTC().Format("2006-01-02")
	f, err := os.Open(filepath.Join(dir, "audit-"+dateKey+".log"))
	if err != nil {
		t.Fatalf("open audit file: %v", err)
	}
	defer func() { _ = f.Close() }()
	recs, err := ParseLog(f)
	if err != nil {
		t.Fatalf("ParseLog: %v", err)
	}
	return recs
}

func appendN(t *testing.T, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if err := AppendEvent(context.Background(), Record{
			Event:    EventProviderSelected,
			Resource: map[string]any{"i": i, "name": "p"},
			Outcome:  OutcomeSuccess,
		}); err != nil {
			t.Fatalf("AppendEvent: %v", err)
		}
	}
}

func TestChain_IntactVerifies(t *testing.T) {
	dir := t.TempDir()
	key := []byte("test-integrity-key")
	if err := ConfigureFromOptions(ConfigOptions{Enabled: true, Dir: dir, HMACKey: key}); err != nil {
		t.Fatal(err)
	}
	appendN(t, 5)

	recs := readChain(t, dir)
	if len(recs) != 5 {
		t.Fatalf("want 5 records, got %d", len(recs))
	}
	if recs[0].PrevHash != "" {
		t.Errorf("genesis prev_hash should be empty, got %q", recs[0].PrevHash)
	}
	for i, r := range recs {
		if r.Hash == "" {
			t.Errorf("record %d has empty hash", i)
		}
	}
	if idx, err := VerifyChain(recs, key, ""); idx != -1 {
		t.Fatalf("intact chain reported a break at %d: %v", idx, err)
	}
}

func TestChain_DetectsContentTampering(t *testing.T) {
	dir := t.TempDir()
	key := []byte("k")
	_ = ConfigureFromOptions(ConfigOptions{Enabled: true, Dir: dir, HMACKey: key})
	appendN(t, 4)

	recs := readChain(t, dir)
	recs[2].Outcome = OutcomeFailure // alter the 3rd record's content
	idx, err := VerifyChain(recs, key, "")
	if idx != 2 {
		t.Fatalf("want break at index 2, got %d (err=%v)", idx, err)
	}
}

func TestChain_DetectsDeletion(t *testing.T) {
	dir := t.TempDir()
	key := []byte("k")
	_ = ConfigureFromOptions(ConfigOptions{Enabled: true, Dir: dir, HMACKey: key})
	appendN(t, 4)

	recs := readChain(t, dir)
	pruned := append(recs[:1:1], recs[2:]...) // delete the 2nd record
	idx, err := VerifyChain(pruned, key, "")
	if idx != 1 {
		t.Fatalf("want break at index 1 after deletion, got %d (err=%v)", idx, err)
	}
}

func TestChain_WrongKeyFails(t *testing.T) {
	dir := t.TempDir()
	_ = ConfigureFromOptions(ConfigOptions{Enabled: true, Dir: dir, HMACKey: []byte("real-key")})
	appendN(t, 3)

	recs := readChain(t, dir)
	if idx, _ := VerifyChain(recs, []byte("wrong-key"), ""); idx != 0 {
		t.Fatalf("verification under the wrong key should fail at index 0, got %d", idx)
	}
}

func TestChain_SurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	key := []byte("k")
	_ = ConfigureFromOptions(ConfigOptions{Enabled: true, Dir: dir, HMACKey: key})
	appendN(t, 2)
	// Simulate a process restart: reconfigure clears the in-memory chain head,
	// forcing a re-seed from the file tail on the next write.
	_ = ConfigureFromOptions(ConfigOptions{Enabled: true, Dir: dir, HMACKey: key})
	appendN(t, 2)

	recs := readChain(t, dir)
	if len(recs) != 4 {
		t.Fatalf("want 4 records across the restart, got %d", len(recs))
	}
	if idx, err := VerifyChain(recs, key, ""); idx != -1 {
		t.Fatalf("chain broke across the restart at %d: %v", idx, err)
	}
}

func TestChain_KeylessStillChains(t *testing.T) {
	dir := t.TempDir()
	_ = ConfigureFromOptions(ConfigOptions{Enabled: true, Dir: dir}) // no key
	appendN(t, 3)

	recs := readChain(t, dir)
	if idx, err := VerifyChain(recs, nil, ""); idx != -1 {
		t.Fatalf("keyless chain should verify with a nil key: %d %v", idx, err)
	}
}
