//go:build !windows

package secrets

import (
	"os"
	"path/filepath"
	"strings"
)

func newDefaultStore() secretBackend {
	return &fileStore{
		protect: func(data []byte) ([]byte, error) { return data, nil },
		unprotect: func(data []byte) ([]byte, error) {
			return data, nil
		},
	}
}

type fileStore struct {
	protect   func([]byte) ([]byte, error)
	unprotect func([]byte) ([]byte, error)
}

func (s *fileStore) Load(name string) (string, bool, error) {
	path, err := secretFilePath(name)
	if err != nil {
		return "", false, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	plain, err := s.unprotect(data)
	if err != nil {
		return "", false, err
	}
	return strings.TrimSpace(string(plain)), true, nil
}

func (s *fileStore) Store(name, value string) error {
	path, err := secretFilePath(name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	protected, err := s.protect([]byte(value))
	if err != nil {
		return err
	}
	return os.WriteFile(path, protected, 0600)
}

func (s *fileStore) Delete(name string) error {
	path, err := secretFilePath(name)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func secretFilePath(name string) (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "SpeechKit", "secrets", name+".bin"), nil
}
