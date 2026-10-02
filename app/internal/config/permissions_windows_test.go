//go:build windows

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckConfigFilePermissionsOS_NonexistentReturnsError(t *testing.T) {
	_, err := checkConfigFilePermissionsOS(filepath.Join(t.TempDir(), "does-not-exist.toml"))
	if err == nil {
		t.Fatalf("want error for nonexistent file, got nil")
	}
}

func TestCheckConfigFilePermissionsOS_OwnerOnlyReturnsNoError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("# test\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Files created via t.TempDir() on Windows inherit the current user's
	// temp-directory ACL, which is typically owner-only. The check may return
	// an empty warning (clean DACL) or a warning if the runner's temp ACL is
	// broader — both are valid, the key invariant is no error and no panic.
	msg, err := checkConfigFilePermissionsOS(path)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	// Just verify no panic and a sensible (possibly empty) string result.
	t.Logf("permission check result: %q", msg)
}
