//go:build windows

package secrets

import (
	"errors"
	"strings"

	"golang.org/x/sys/windows/registry"

	"github.com/kombifyio/SpeechKit/app/internal/productid"
)

// bootstrapRegistryValueName is the value the installer leaves below the
// product's HKCU\Software\<Vendor>\<Product> key.
const bootstrapRegistryValueName = "PendingHFInstallToken"

// bootstrapRegistryPath is the registered product's per-user key, or
// productid.ErrNoIdentity.
func bootstrapRegistryPath() (string, error) {
	p, err := productid.Current()
	if err != nil {
		return "", err
	}
	return `Software\` + p.Vendor + `\` + p.Product, nil
}

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
	path, err := bootstrapRegistryPath()
	if err != nil {
		return "", err
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, path, registry.QUERY_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	defer key.Close() //nolint:errcheck // Windows registry key close, error not actionable

	value, _, err := key.GetStringValue(bootstrapRegistryValueName)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	return value, nil
}

func clearInstallBootstrapTokenFromRegistry() error {
	path, err := bootstrapRegistryPath()
	if err != nil {
		return err
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, path, registry.SET_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		return err
	}
	defer key.Close() //nolint:errcheck // Windows registry key close, error not actionable

	if err := key.DeleteValue(bootstrapRegistryValueName); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return nil
}
