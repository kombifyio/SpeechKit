package training

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPruneCaptureDirEnforcesAgeAndCountLimits(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writePair := func(name string, age time.Duration) string {
		t.Helper()
		for _, ext := range []string{".wav", ".json"} {
			p := filepath.Join(dir, name+ext)
			if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(p, now.Add(-age), now.Add(-age)); err != nil {
				t.Fatal(err)
			}
		}
		return filepath.Join(dir, name+".wav")
	}
	expired := writePair("2026-01-01T10-00-00.000Z_hey_kubi_0.91", 40*24*time.Hour)
	surplus := writePair("2026-09-01T10-00-00.000Z_hey_kubi_0.80", 3*time.Hour)
	keepA := writePair("2026-09-30T10-00-00.000Z_hey_kubi_0.85", 2*time.Hour)
	keepB := writePair("2026-09-30T11-00-00.000Z_hey_kubi_0.95", time.Hour)
	unrelated := filepath.Join(dir, "notes.json")
	if err := os.WriteFile(unrelated, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = os.Chtimes(unrelated, now.Add(-400*24*time.Hour), now.Add(-400*24*time.Hour))

	if _, err := PruneCaptureDir(dir, 30*24*time.Hour, 2, now); err != nil {
		t.Fatalf("PruneCaptureDir: %v", err)
	}

	for _, gone := range []string{expired, surplus} {
		if _, err := os.Stat(gone); !os.IsNotExist(err) {
			t.Errorf("%s should have been pruned", filepath.Base(gone))
		}
	}
	for _, kept := range []string{keepA, keepB, unrelated} {
		if _, err := os.Stat(kept); err != nil {
			t.Errorf("%s should have been kept: %v", filepath.Base(kept), err)
		}
	}
}
