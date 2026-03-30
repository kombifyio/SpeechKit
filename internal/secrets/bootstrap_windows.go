//go:build windows

package secrets

import (
	"strings"

	"golang.org/x/sys/windows/registry"
)

const (
	bootstrapRegistryPath      = `Software\kombify\SpeechKit`
	bootstrapRegistryValueName = "PendingHFInstallToken"
)

var (
	readBootstrapInstallToken  = readInstallBootstrapTokenFromRegistry
	clearBootstrapInstallToken = clearInstallBootstrapTokenFromRegistry
)

func MigrateInstallTokenBootstrap() (bool, error) {
	token, err := readBootstrapInstallToken()
	if err != nil {
		return false, err
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return false, nil
	}
	if err := SetInstallHuggingFaceToken(token); err != nil {
		return false, err
	}
	if err := clearBootstrapInstallToken(); err != nil {
		return false, err
	}
	return true, nil
}

func readInstallBootstrapTokenFromRegistry() (string, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, bootstrapRegistryPath, registry.QUERY_VALUE)
	if err != nil {
		if err == registry.ErrNotExist {
			return "", nil
		}
		return "", err
	}
	defer key.Close()

	value, _, err := key.GetStringValue(bootstrapRegistryValueName)
	if err != nil {
		if err == registry.ErrNotExist {
			return "", nil
		}
		return "", err
	}
	return value, nil
}

func clearInstallBootstrapTokenFromRegistry() error {
	key, err := registry.OpenKey(registry.CURRENT_USER, bootstrapRegistryPath, registry.SET_VALUE)
	if err != nil {
		if err == registry.ErrNotExist {
			return nil
		}
		return err
	}
	defer key.Close()

	if err := key.DeleteValue(bootstrapRegistryValueName); err != nil && err != registry.ErrNotExist {
		return err
	}
	return nil
}
