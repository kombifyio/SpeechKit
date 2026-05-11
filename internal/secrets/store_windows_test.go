//go:build windows

package secrets

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsFileStoreRoundTripsWithInjectedProtection(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
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
	storedPath := secretFilePath("api-key")
	if filepath.Dir(storedPath) != filepath.Join(os.Getenv("APPDATA"), "SpeechKit", "secrets") {
		t.Fatalf("secret path = %q", storedPath)
	}
	raw, err := os.ReadFile(storedPath) // #nosec G304 -- test reads the path created by secretFilePath.
	if err != nil {
		t.Fatalf("read stored secret: %v", err)
	}
	if !bytes.HasPrefix(raw, []byte("protected:")) {
		t.Fatalf("stored secret was not protected: %q", raw)
	}

	value, ok, err := store.Load("api-key")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !ok || value != "secret-value" {
		t.Fatalf("Load = (%q, %v), want trimmed secret", value, ok)
	}
	if err := store.Delete("api-key"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if value, ok, err := store.Load("api-key"); err != nil || ok || value != "" {
		t.Fatalf("Load after delete = (%q, %v, %v)", value, ok, err)
	}
}

func TestWindowsFileStorePropagatesProtectionErrors(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
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
		t.Fatalf("Store error = %v, want %v", err, protectErr)
	}
	path := secretFilePath("api-key")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create secrets dir: %v", err)
	}
	if err := os.WriteFile(path, []byte("ciphertext"), 0o600); err != nil {
		t.Fatalf("write secret fixture: %v", err)
	}
	if _, _, err := store.Load("api-key"); !errors.Is(err, unprotectErr) {
		t.Fatalf("Load error = %v, want %v", err, unprotectErr)
	}
}

func TestWindowsSecretFileNameSanitizesUnsafeNames(t *testing.T) {
	if got := secretFileName("safe.Name-1"); got != "safe.Name-1.bin" {
		t.Fatalf("safe secret file name = %q", got)
	}
	got := secretFileName("../unsafe")
	if !strings.HasPrefix(got, "secret-") || !strings.HasSuffix(got, ".bin") || strings.Contains(got, "..") {
		t.Fatalf("unsafe secret file name = %q", got)
	}
}

func TestWindowsDPAPIHelpersAcceptEmptyPayload(t *testing.T) {
	if protected, err := protectWithDPAPI(nil); err != nil || protected != nil {
		t.Fatalf("protect empty = (%v, %v), want nil nil", protected, err)
	}
	if plain, err := unprotectWithDPAPI(nil); err != nil || plain != nil {
		t.Fatalf("unprotect empty = (%v, %v), want nil nil", plain, err)
	}
}

func TestWindowsDPAPIHelpersRoundTrip(t *testing.T) {
	protected, err := protectWithDPAPI([]byte("secret-value"))
	if err != nil {
		t.Fatalf("protect: %v", err)
	}
	if bytes.Equal(protected, []byte("secret-value")) {
		t.Fatal("protected payload should not equal plaintext")
	}
	plain, err := unprotectWithDPAPI(protected)
	if err != nil {
		t.Fatalf("unprotect: %v", err)
	}
	if string(plain) != "secret-value" {
		t.Fatalf("plain = %q", plain)
	}
}

func TestWindowsConfigureDopplerCommandHidesWindow(t *testing.T) {
	cmd := exec.Command("doppler")
	configureDopplerCommand(cmd)
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.HideWindow {
		t.Fatalf("SysProcAttr = %+v, want HideWindow", cmd.SysProcAttr)
	}
}
