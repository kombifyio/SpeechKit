//go:build darwin && cgo

package secrets

import (
	"crypto/rand"
	"errors"
	"fmt"
	"sync"

	"github.com/keybase/go-keychain"
)

// macOS has no DPAPI, and putting every provider key in its own Keychain item
// would ask the user to authorise each one separately on an ad-hoc signed
// build (the signature changes on every rebuild, so the Keychain treats each
// build as a new application). Instead a single generic-password item holds a
// random 32-byte master key and the secrets stay AES-256-GCM files under
// runtimepath.SecretsDir(), exactly like the Windows DPAPI layout. That is one
// Keychain prompt per build identity for the whole store.
const (
	// keychainService is the app's bundle identifier, so the item shows up in
	// Keychain Access under the same name as the desktop client.
	keychainService = "com.kombify.speechkit"
	// keychainMasterKeyAccount is the account of the single master-key item.
	keychainMasterKeyAccount = "secrets-master-key"
	// keychainMasterKeyLabel is what Keychain Access displays.
	keychainMasterKeyLabel = "kombify SpeechKit secrets master key"
)

// newDefaultStore wraps the shared file store (filestore.go) in AES-256-GCM
// under the Keychain-held master key.
func newDefaultStore() secretBackend {
	return newKeychainFileStore(keychainService, keychainMasterKeyAccount)
}

func newKeychainFileStore(service, account string) *fileStore {
	master := &keychainMasterKey{service: service, account: account}
	return &fileStore{
		protect: func(plain []byte) ([]byte, error) {
			// An empty payload seals to an empty blob, so it needs no key and
			// must not cost a Keychain prompt.
			if len(plain) == 0 {
				return nil, nil
			}
			key, err := master.resolve()
			if err != nil {
				return nil, err
			}
			return sealSecret(key, plain)
		},
		unprotect: func(envelope []byte) ([]byte, error) {
			if len(envelope) == 0 {
				return nil, nil
			}
			key, err := master.resolve()
			if err != nil {
				return nil, err
			}
			return openSecret(key, envelope)
		},
	}
}

// keychainMasterKey resolves the master key at most once per process so a run
// costs at most one Keychain prompt. A failed attempt is deliberately not
// cached: a user who unlocks the Keychain and retries succeeds without
// restarting the app. The mutex is held across the Keychain call so concurrent
// callers queue behind one prompt instead of racing into several.
type keychainMasterKey struct {
	service string
	account string

	mu  sync.Mutex
	key []byte
}

func (k *keychainMasterKey) resolve() ([]byte, error) {
	k.mu.Lock()
	defer k.mu.Unlock()

	if len(k.key) == secretMasterKeyLen {
		return k.key, nil
	}
	key, err := loadOrCreateKeychainMasterKey(k.service, k.account)
	if err != nil {
		return nil, err
	}
	k.key = key
	return key, nil
}

func loadOrCreateKeychainMasterKey(service, account string) ([]byte, error) {
	key, err := readKeychainMasterKey(service, account)
	if err != nil {
		return nil, err
	}
	switch len(key) {
	case secretMasterKeyLen:
		return key, nil
	case 0:
		return createKeychainMasterKey(service, account)
	default:
		// An item of the wrong size is not ours to overwrite: fail closed
		// rather than silently orphan whatever it protects.
		return nil, fmt.Errorf("secrets: keychain master key has %d bytes, want %d: %w",
			len(key), secretMasterKeyLen, ErrSecureStoreUnavailable)
	}
}

func readKeychainMasterKey(service, account string) ([]byte, error) {
	query := keychain.NewItem()
	query.SetSecClass(keychain.SecClassGenericPassword)
	query.SetService(service)
	query.SetAccount(account)
	query.SetMatchLimit(keychain.MatchLimitOne)
	query.SetReturnData(true)

	results, err := keychain.QueryItem(query)
	if err != nil {
		if errors.Is(err, keychain.ErrorItemNotFound) {
			return nil, nil
		}
		return nil, keychainUnavailable("read master key", err)
	}
	if len(results) == 0 {
		return nil, nil
	}
	return results[0].Data, nil
}

func createKeychainMasterKey(service, account string) ([]byte, error) {
	key := make([]byte, secretMasterKeyLen)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("secrets: generate keychain master key: %w", err)
	}

	item := keychain.NewGenericPassword(service, account, keychainMasterKeyLabel, key, "")
	// No access group: an ad-hoc signed build has no team identifier, and an
	// access group it cannot claim would make the item unreadable.
	item.SetSynchronizable(keychain.SynchronizableNo)
	item.SetAccessible(keychain.AccessibleWhenUnlocked)

	if err := keychain.AddItem(item); err != nil {
		if errors.Is(err, keychain.ErrorDuplicateItem) {
			// Another process created the item between the read and the add.
			return readExistingKeychainMasterKey(service, account)
		}
		return nil, keychainUnavailable("store master key", err)
	}
	return key, nil
}

func readExistingKeychainMasterKey(service, account string) ([]byte, error) {
	existing, err := readKeychainMasterKey(service, account)
	if err != nil {
		return nil, err
	}
	if len(existing) != secretMasterKeyLen {
		return nil, fmt.Errorf("secrets: keychain master key vanished after a duplicate-item add: %w",
			ErrSecureStoreUnavailable)
	}
	return existing, nil
}

// keychainUnavailable reports a Keychain failure as an unavailable secure
// store so callers keep their documented env/Doppler fallback. The Keychain
// error is wrapped too, so a caller can still test for a specific OSStatus.
// Keychain errors carry status codes and static messages, never key material.
func keychainUnavailable(op string, err error) error {
	return fmt.Errorf("secrets: keychain %s: %w: %w", op, ErrSecureStoreUnavailable, err)
}
