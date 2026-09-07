//go:build !windows && !(darwin && cgo)

package secrets

// Every OS without an encrypted store of its own: Windows uses DPAPI
// (store_windows.go), macOS uses a Keychain-held master key over AES-256-GCM
// files (store_darwin.go, cgo only). Everything else — Linux, and a darwin
// cross-compile with cgo off — fails closed instead of writing plaintext.
func newDefaultStore() secretBackend {
	return unsupportedStore{}
}

type unsupportedStore struct{}

func (unsupportedStore) Load(name string) (string, bool, error) {
	return "", false, nil
}

func (unsupportedStore) Store(name, value string) error {
	return ErrSecureStoreUnavailable
}

func (unsupportedStore) Delete(name string) error {
	return nil
}
