//go:build windows

package secrets

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unsafe"

	"github.com/kombifyio/SpeechKit/internal/runtimepath"
	"golang.org/x/sys/windows"
)

var (
	crypt32                = windows.NewLazySystemDLL("Crypt32.dll")
	kernel32               = windows.NewLazySystemDLL("Kernel32.dll")
	procCryptProtectData   = crypt32.NewProc("CryptProtectData")
	procCryptUnprotectData = crypt32.NewProc("CryptUnprotectData")
	procLocalFree          = kernel32.NewProc("LocalFree")
)

type dataBlob struct {
	cbData uint32
	pbData *byte
}

func newDefaultStore() secretBackend {
	return &fileStore{
		protect:   protectWithDPAPI,
		unprotect: unprotectWithDPAPI,
	}
}

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
	sum := sha256.Sum256([]byte(name))
	return "secret-" + hex.EncodeToString(sum[:]) + ".bin"
}

func protectWithDPAPI(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}

	input := dataBlob{
		cbData: uint32(len(data)), // #nosec G115 -- secrets are small in-memory DPAPI payloads.
		pbData: &data[0],
	}
	var output dataBlob

	result, _, err := procCryptProtectData.Call(
		uintptr(unsafe.Pointer(&input)), //nolint:gosec // Windows API requires unsafe.Pointer
		0,
		0,
		0,
		0,
		0,
		uintptr(unsafe.Pointer(&output)), //nolint:gosec // Windows API requires unsafe.Pointer
	)
	if result == 0 {
		return nil, err
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(output.pbData))) //nolint:gosec,errcheck // Windows API requires unsafe.Pointer; return value not meaningful

	protected := unsafe.Slice(output.pbData, output.cbData) //nolint:gosec // G103: DPAPI output buffer, audited
	clone := make([]byte, len(protected))
	copy(clone, protected)
	return clone, nil
}

func unprotectWithDPAPI(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}

	input := dataBlob{
		cbData: uint32(len(data)), // #nosec G115 -- secrets are small in-memory DPAPI payloads.
		pbData: &data[0],
	}
	var output dataBlob

	result, _, err := procCryptUnprotectData.Call(
		uintptr(unsafe.Pointer(&input)), //nolint:gosec // Windows API requires unsafe.Pointer
		0,
		0,
		0,
		0,
		0,
		uintptr(unsafe.Pointer(&output)), //nolint:gosec // Windows API requires unsafe.Pointer
	)
	if result == 0 {
		return nil, err
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(output.pbData))) //nolint:gosec,errcheck // Windows API requires unsafe.Pointer; return value not meaningful

	plain := unsafe.Slice(output.pbData, output.cbData) //nolint:gosec // G103: DPAPI output buffer, audited
	clone := make([]byte, len(plain))
	copy(clone, plain)
	return clone, nil
}
