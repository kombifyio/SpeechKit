//go:build darwin && cgo

package secrets

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/keybase/go-keychain"
)

// keychainMissingEntitlement is errSecMissingEntitlement (-34018), which
// go-keychain does not export. An unsigned test binary can hit it on a runner
// whose session has no keychain of its own; it means "this environment cannot
// use the Keychain", not "the store is broken".
const keychainMissingEntitlement = keychain.Error(-34018)

// keychainProbeTimeout bounds every Keychain call these tests make.
//
// A locked login keychain does not fail a Keychain call: SecItemAdd blocks
// waiting for an unlock prompt that a non-interactive session can never show.
// On a macos-14 runner that hung this package for the full 15-minute test
// timeout (2026-09-07) instead of skipping, so every call here is answered by
// a goroutine with a deadline and a missing answer is treated exactly like an
// environment error.
const keychainProbeTimeout = 20 * time.Second

// keychainEnvironmentCodes are the OSStatus values that say the runner, not
// the code, is why the Keychain is unusable: a locked or absent login
// keychain, or a non-interactive session that cannot show the prompt.
var keychainEnvironmentCodes = []keychain.Error{
	keychain.ErrorInteractionNotAllowed,
	keychain.ErrorUserCanceled,
	keychain.ErrorAuthFailed,
	keychain.ErrorNotAvailable,
	keychain.ErrorNoSuchKeychain,
	keychainMissingEntitlement,
}

type keychainAnswer struct {
	key []byte
	err error
}

// callKeychain runs one Keychain operation with a deadline. The second result
// is false when the call did not answer in time; the goroutine stays blocked
// in the OS call, which costs nothing because the test binary exits anyway.
func callKeychain(fn func() ([]byte, error)) (keychainAnswer, bool) {
	done := make(chan keychainAnswer, 1)
	go func() {
		key, err := fn()
		done <- keychainAnswer{key: key, err: err}
	}()
	select {
	case answer := <-done:
		return answer, true
	case <-time.After(keychainProbeTimeout):
		return keychainAnswer{}, false
	}
}

// requireUsableKeychain runs one Keychain operation and skips the test unless
// it answered and succeeded. It is the only way into the Keychain from these
// tests: a real round-trip needs a real login keychain, which a headless CI
// session does not have, and a test that cannot reach one must say so rather
// than pass vacuously.
func requireUsableKeychain(t *testing.T, what string, fn func() ([]byte, error)) []byte {
	t.Helper()
	if testing.Short() {
		t.Skipf("%s needs an unlocked login keychain; -short runs on CI, where there is none", what)
	}

	answer, answered := callKeychain(fn)
	if !answered {
		t.Skipf("%s did not answer within %v: the login keychain is locked and this session cannot show its prompt", what, keychainProbeTimeout)
	}
	if answer.err != nil {
		for _, code := range keychainEnvironmentCodes {
			if errors.Is(answer.err, code) {
				t.Skipf("login keychain is not usable in this session (%v); the store itself is untested here", answer.err)
			}
		}
		t.Fatalf("%s: %v", what, answer.err)
	}
	return answer.key
}

// testKeychainAccount keeps the test off the account the product uses, so a
// developer running the suite never overwrites or deletes the master key that
// protects their real secrets. The cleanup is bounded for the same reason the
// calls are.
func testKeychainAccount(t *testing.T) string {
	t.Helper()
	account := fmt.Sprintf("secrets-master-key-test-%d-%d", os.Getpid(), time.Now().UnixNano())
	t.Cleanup(func() {
		answer, answered := callKeychain(func() ([]byte, error) {
			return nil, keychain.DeleteGenericPasswordItem(keychainService, account)
		})
		switch {
		case !answered:
			t.Logf("removing the test keychain item did not answer within %v", keychainProbeTimeout)
		case answer.err != nil && !errors.Is(answer.err, keychain.ErrorItemNotFound):
			t.Logf("could not remove the test keychain item: %v", answer.err)
		}
	})
	return account
}

func TestKeychainStoreRoundTripsASecretAcrossStoreInstances(t *testing.T) {
	account := testKeychainAccount(t)

	// Provision the master key before the environment is redirected, so a
	// runner without a usable keychain skips instead of hanging.
	requireUsableKeychain(t, "provision the keychain master key", func() ([]byte, error) {
		return loadOrCreateKeychainMasterKey(keychainService, account)
	})

	redirectSecretsDir(t)
	if err := newKeychainFileStore(keychainService, account).Store("api-key", "secret-value"); err != nil {
		t.Fatalf("Store: %v", err)
	}

	raw, err := os.ReadFile(secretFilePath("api-key")) // #nosec G304 -- test reads the path created by secretFilePath.
	if err != nil {
		t.Fatalf("read stored secret: %v", err)
	}
	if bytes.Contains(raw, []byte("secret-value")) {
		t.Fatal("secret file was written in plaintext")
	}

	// A second store resolves the master key from the Keychain again rather
	// than from the first store's in-process cache: this is the restart path.
	reader := newKeychainFileStore(keychainService, account)
	value, ok, err := reader.Load("api-key")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !ok {
		t.Fatal("Load did not return the stored secret")
	}
	assertTestSecret(t, "loaded secret", value, "secret-value")

	if err := reader.Delete("api-key"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if value, ok, err := reader.Load("api-key"); err != nil || ok || value != "" {
		t.Fatalf("Load after delete retained secret (present=%v, err=%v)", ok, err)
	}
}

func TestKeychainMasterKeyIsStableAndCachedPerProcess(t *testing.T) {
	account := testKeychainAccount(t)

	master := &keychainMasterKey{service: keychainService, account: account}
	first := requireUsableKeychain(t, "resolve the master key", master.resolve)
	if len(first) != secretMasterKeyLen {
		t.Fatalf("master key length = %d, want %d", len(first), secretMasterKeyLen)
	}

	cached, err := master.resolve()
	if err != nil {
		t.Fatalf("resolve cached master key: %v", err)
	}
	if !bytes.Equal(first, cached) {
		t.Fatal("the cached master key differs from the resolved one")
	}

	// A fresh resolver must read back the same key the first one created,
	// otherwise every restart would orphan the secrets already on disk.
	reread := requireUsableKeychain(t, "re-resolve the master key", (&keychainMasterKey{service: keychainService, account: account}).resolve)
	if !bytes.Equal(first, reread) {
		t.Fatal("a second resolver read a different master key")
	}
}

func TestKeychainStoreReportsUnavailableSecureStore(t *testing.T) {
	// A wrong-size item is the one Keychain state the code can create without
	// the OS: it must fail closed, never silently replace the key.
	account := testKeychainAccount(t)
	requireUsableKeychain(t, "add a short keychain item", func() ([]byte, error) {
		item := keychain.NewGenericPassword(keychainService, account, keychainMasterKeyLabel, []byte("too-short"), "")
		item.SetSynchronizable(keychain.SynchronizableNo)
		item.SetAccessible(keychain.AccessibleWhenUnlocked)
		return nil, keychain.AddItem(item)
	})

	_, err := loadOrCreateKeychainMasterKey(keychainService, account)
	if !errors.Is(err, ErrSecureStoreUnavailable) {
		t.Fatalf("error = %v, want ErrSecureStoreUnavailable", err)
	}
}
