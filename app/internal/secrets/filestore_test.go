package secrets

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// redirectSecretsDir points runtimepath.SecretsDir() at a temporary directory
// on every OS the store compiles for: %APPDATA% on Windows, XDG_CONFIG_HOME on
// Linux, $HOME on macOS. It skips the test when the redirect did not take, so
// a test can never write into the real user's secrets directory.
func redirectSecretsDir(t *testing.T) {
	t.Helper()

	root := t.TempDir()
	t.Setenv("SPEECHKIT_DISABLE_PORTABLE", "1")
	t.Setenv("APPDATA", root)
	t.Setenv("LOCALAPPDATA", root)
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("HOME", root)
	t.Setenv("USERPROFILE", root)

	dir := filepath.Dir(secretFilePath("probe"))
	if rel, err := filepath.Rel(root, dir); err != nil || strings.HasPrefix(rel, "..") {
		t.Skipf("secrets dir %q is outside the test root; refusing to touch the real store", dir)
	}
}

func TestFileStoreRoundTripsThroughInjectedProtection(t *testing.T) {
	redirectSecretsDir(t)
	store := &fileStore{
		protect: func(data []byte) ([]byte, error) {
			return append([]byte("protected:"), data...), nil
		},
		unprotect: func(data []byte) ([]byte, error) {
			return bytes.TrimPrefix(data, []byte("protected:")), nil
		},
	}

	if err := store.Store(" api-key ", " secret-value "); err != nil {
		t.Fatalf("Store: %v", err)
	}
	raw, err := os.ReadFile(secretFilePath("api-key")) // #nosec G304 -- test reads the path created by secretFilePath.
	if err != nil {
		t.Fatalf("read stored secret: %v", err)
	}
	if !bytes.HasPrefix(raw, []byte("protected:")) {
		t.Fatal("stored secret was not handed to the platform protector")
	}

	value, ok, err := store.Load("api-key")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !ok {
		t.Fatal("Load did not return a stored secret")
	}
	assertTestSecret(t, "loaded secret", value, "secret-value")

	if err := store.Delete("api-key"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if value, ok, err := store.Load("api-key"); err != nil || ok || value != "" {
		t.Fatalf("Load after delete retained secret (present=%v, err=%v)", ok, err)
	}
}

func TestFileStorePropagatesProtectionErrors(t *testing.T) {
	redirectSecretsDir(t)
	protectErr := errors.New("protect failed")
	unprotectErr := errors.New("unprotect failed")
	store := &fileStore{
		protect: func([]byte) ([]byte, error) {
			return nil, protectErr
		},
		unprotect: func([]byte) ([]byte, error) {
			return nil, unprotectErr
		},
	}

	if err := store.Store("api-key", "secret"); !errors.Is(err, protectErr) {
		t.Fatalf("Store error = %v, want the protector's error", err)
	}
	path := secretFilePath("api-key")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create secrets dir: %v", err)
	}
	if err := os.WriteFile(path, []byte("ciphertext"), 0o600); err != nil {
		t.Fatalf("write secret fixture: %v", err)
	}
	if _, _, err := store.Load("api-key"); !errors.Is(err, unprotectErr) {
		t.Fatalf("Load error = %v, want the unprotector's error", err)
	}
}

func TestSecretFileNameSanitizesUnsafeNames(t *testing.T) {
	if got := secretFileName("safe.Name-1"); got != "safe.Name-1.bin" {
		t.Fatalf("safe secret file name = %q", got)
	}
	got := secretFileName("../unsafe")
	if !strings.HasPrefix(got, "secret-") || !strings.HasSuffix(got, ".bin") || strings.Contains(got, "..") {
		t.Fatalf("unsafe secret file name = %q", got)
	}
}
