//go:build windows

package secrets

import (
	"unsafe"

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

// newDefaultStore wraps the shared file store (filestore.go) in DPAPI: the
// blobs are sealed to the current Windows user account, so no key material
// ever lands on disk.
func newDefaultStore() secretBackend {
	return &fileStore{
		protect:   protectWithDPAPI,
		unprotect: unprotectWithDPAPI,
	}
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
