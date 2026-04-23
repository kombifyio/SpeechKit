//go:build !windows

package secrets

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
