package secrets

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/runtimepath"
)

// fileStore keeps one encrypted blob per secret under runtimepath.SecretsDir()
// and delegates the encryption to the platform. Windows supplies DPAPI
// (store_windows.go); macOS supplies AES-256-GCM under a Keychain-held master
// key (store_darwin.go). The layout, the filename mapping and the file modes
// are identical on both so a secret written by one backend is only ever
// readable by the same backend on the same machine.
type fileStore struct {
	protect   func([]byte) ([]byte, error)
	unprotect func([]byte) ([]byte, error)
}

func (s *fileStore) Load(name string) (string, bool, error) {
	path := secretFilePath(name)
	data, err := os.ReadFile(path) // #nosec G304 -- secretFilePath maps names to a scoped secrets-dir filename.
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
	path := secretFilePath(name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	protected, err := s.protect([]byte(value))
	if err != nil {
		return err
	}
	return os.WriteFile(path, protected, 0o600)
}

func (s *fileStore) Delete(name string) error {
	path := secretFilePath(name)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func secretFilePath(name string) string {
	return filepath.Join(runtimepath.SecretsDir(), secretFileName(name))
}

var safeSecretFileNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func secretFileName(name string) string {
	name = strings.TrimSpace(name)
	if safeSecretFileNamePattern.MatchString(name) {
		return name + ".bin"
	}
	// The hashed value is the secret's identifier ("huggingface-user",
	// "named-secret:openai"), never the secret itself: SHA-256 only derives a
	// filesystem-safe file name for an identifier that carries characters a
	// path cannot. The secret is protected by the platform backend, not by
	// this hash. The marker below documents that for a reader; code scanning
	// default setup does not honour inline suppressions, so the alert is
	// dismissed in the security tab as well, as with the other false
	// positives in this repository.
	sum := sha256.Sum256([]byte(name)) // codeql[go/weak-sensitive-data-hashing] hashes the secret's name, not the secret
	return "secret-" + hex.EncodeToString(sum[:]) + ".bin"
}
