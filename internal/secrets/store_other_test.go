//go:build !windows

package secrets

import (
	"errors"
	"testing"
)

func TestUnsupportedStoreDoesNotPersistSecrets(t *testing.T) {
	store := newDefaultStore()

	if err := store.Store("api-key", "secret"); !errors.Is(err, ErrSecureStoreUnavailable) {
		t.Fatalf("Store error = %v, want ErrSecureStoreUnavailable", err)
	}

	value, ok, err := store.Load("api-key")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if ok || value != "" {
		t.Fatalf("Load = (%q, %v), want empty miss", value, ok)
	}

	if err := store.Delete("api-key"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}
