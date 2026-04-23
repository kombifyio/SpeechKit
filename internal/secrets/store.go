package secrets

import (
	"errors"
	"fmt"
	"strings"
	"sync"
)

type TokenSource string

const (
	TokenSourceNone    TokenSource = "none"
	TokenSourceUser    TokenSource = "user"
	TokenSourceInstall TokenSource = "install"
	TokenSourceEnv     TokenSource = "env"
)

const genericSecretPrefix = "named-secret:"

// ErrSecureStoreUnavailable means the current OS has no supported encrypted
// secret store implementation. Callers should use environment variables or
// Doppler-managed install credentials instead of persisting local secrets.
var ErrSecureStoreUnavailable = errors.New("secrets: secure secret storage unavailable on this OS")

type TokenStatus struct {
	HasUserToken    bool
	HasInstallToken bool
	ActiveSource    TokenSource
}

type secretBackend interface {
	Load(name string) (string, bool, error)
	Store(name, value string) error
	Delete(name string) error
}

var (
	backendMu      sync.RWMutex
	currentBackend = newDefaultStore()
)

const (
	huggingFaceUserKey    = "huggingface-user"
	huggingFaceInstallKey = "huggingface-install"
)

func UseMemoryStoreForTests() func() {
	backendMu.Lock()
	previous := currentBackend
	currentBackend = &memoryStore{values: map[string]string{}}
	backendMu.Unlock()

	return func() {
		backendMu.Lock()
		currentBackend = previous
		backendMu.Unlock()
	}
}

func SetUserHuggingFaceToken(token string) error {
	return storeSecret(huggingFaceUserKey, token)
}

func SetInstallHuggingFaceToken(token string) error {
	return storeSecret(huggingFaceInstallKey, token)
}

func ClearUserHuggingFaceToken() error {
	return currentStore().Delete(huggingFaceUserKey)
}

func SetNamedSecret(name, value string) error {
	return storeSecret(namedSecretKey(name), value)
}

func ClearNamedSecret(name string) error {
	return currentStore().Delete(namedSecretKey(name))
}

func ResolveNamedSecret(name string, envResolver func() string) (string, TokenStatus, error) {
	status, err := NamedSecretStatus(name, envResolver)
	if err != nil {
		return "", status, err
	}

	switch status.ActiveSource {
	case TokenSourceUser:
		value, _, err := currentStore().Load(namedSecretKey(name))
		return strings.TrimSpace(value), status, err
	case TokenSourceEnv:
		if envResolver == nil {
			return "", status, nil
		}
		return strings.TrimSpace(envResolver()), status, nil
	default:
		return "", status, nil
	}
}

func NamedSecretStatus(name string, envResolver func() string) (TokenStatus, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return TokenStatus{ActiveSource: TokenSourceNone}, nil
	}
	storedSecret, hasStoredSecret, err := currentStore().Load(namedSecretKey(name))
	if err != nil {
		return TokenStatus{}, err
	}

	status := TokenStatus{
		HasUserToken: hasStoredSecret && strings.TrimSpace(storedSecret) != "",
		ActiveSource: TokenSourceNone,
	}
	if status.HasUserToken {
		status.ActiveSource = TokenSourceUser
		return status, nil
	}
	if envResolver != nil && strings.TrimSpace(envResolver()) != "" {
		status.ActiveSource = TokenSourceEnv
	}
	return status, nil
}

func ResolveHuggingFaceToken(envResolver func() string) (string, TokenStatus, error) {
	status, err := HuggingFaceTokenStatus(envResolver)
	if err != nil {
		return "", status, err
	}

	switch status.ActiveSource {
	case TokenSourceUser:
		token, _, err := currentStore().Load(huggingFaceUserKey)
		return token, status, err
	case TokenSourceInstall:
		token, _, err := currentStore().Load(huggingFaceInstallKey)
		return token, status, err
	case TokenSourceEnv:
		if envResolver == nil {
			return "", status, nil
		}
		return strings.TrimSpace(envResolver()), status, nil
	default:
		return "", status, nil
	}
}

func HuggingFaceTokenStatus(envResolver func() string) (TokenStatus, error) {
	userToken, hasUserToken, err := currentStore().Load(huggingFaceUserKey)
	if err != nil {
		return TokenStatus{}, err
	}
	installToken, hasInstallToken, err := currentStore().Load(huggingFaceInstallKey)
	if err != nil {
		return TokenStatus{}, err
	}

	status := TokenStatus{
		HasUserToken:    hasUserToken && strings.TrimSpace(userToken) != "",
		HasInstallToken: hasInstallToken && strings.TrimSpace(installToken) != "",
		ActiveSource:    TokenSourceNone,
	}

	switch {
	case status.HasUserToken:
		status.ActiveSource = TokenSourceUser
	case status.HasInstallToken:
		status.ActiveSource = TokenSourceInstall
	case envResolver != nil && strings.TrimSpace(envResolver()) != "":
		status.ActiveSource = TokenSourceEnv
	}

	return status, nil
}

func storeSecret(name, value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("secret %q cannot be empty", name)
	}
	return currentStore().Store(name, trimmed)
}

func currentStore() secretBackend {
	backendMu.RLock()
	defer backendMu.RUnlock()
	return currentBackend
}

func namedSecretKey(name string) string {
	return genericSecretPrefix + strings.TrimSpace(name)
}

type memoryStore struct {
	mu     sync.RWMutex
	values map[string]string
}

func (m *memoryStore) Load(name string) (string, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	value, ok := m.values[name]
	return value, ok, nil
}

func (m *memoryStore) Store(name, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.values[name] = value
	return nil
}

func (m *memoryStore) Delete(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.values, name)
	return nil
}
