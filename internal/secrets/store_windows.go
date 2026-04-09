//go:build windows

package secrets

import (
	"os"
	"path/filepath"
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
	return filepath.Join(runtimepath.SecretsDir(), name+".bin"), nil
}

func protectWithDPAPI(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}

	input := dataBlob{
		cbData: uint32(len(data)),
		pbData: &data[0],
	}
	var output dataBlob

	result, _, err := procCryptProtectData.Call(
		uintptr(unsafe.Pointer(&input)),
		0,
		0,
		0,
		0,
		0,
		uintptr(unsafe.Pointer(&output)),
	)
	if result == 0 {
		return nil, err
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(output.pbData)))

	protected := unsafe.Slice(output.pbData, output.cbData)
	clone := make([]byte, len(protected))
	copy(clone, protected)
	return clone, nil
}

func unprotectWithDPAPI(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}

	input := dataBlob{
		cbData: uint32(len(data)),
		pbData: &data[0],
	}
	var output dataBlob

	result, _, err := procCryptUnprotectData.Call(
		uintptr(unsafe.Pointer(&input)),
		0,
		0,
		0,
		0,
		0,
		uintptr(unsafe.Pointer(&output)),
	)
	if result == 0 {
		return nil, err
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(output.pbData)))

	plain := unsafe.Slice(output.pbData, output.cbData)
	clone := make([]byte, len(plain))
	copy(clone, plain)
	return clone, nil
}
