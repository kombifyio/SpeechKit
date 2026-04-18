//go:build windows

package voiceagent

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// protectResumeHandle encrypts the handle with DPAPI at user scope so that only
// the current Windows user (and only on this machine) can decrypt it. The
// returned slice is an independent copy safe to retain after LocalFree.
func protectResumeHandle(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}
	input := rhDataBlob{cbData: uint32(len(data)), pbData: &data[0]} //nolint:gosec // Windows API integer conversion, value fits
	var output rhDataBlob
	result, _, err := procCryptProtectDataRH.Call(
		uintptr(unsafe.Pointer(&input)), //nolint:gosec // Windows API requires unsafe.Pointer
		0, 0, 0, 0, 0,
		uintptr(unsafe.Pointer(&output)), //nolint:gosec // Windows API requires unsafe.Pointer
	)
	if result == 0 {
		return nil, err
	}
	defer procLocalFreeRH.Call(uintptr(unsafe.Pointer(output.pbData))) //nolint:errcheck,gosec // Windows API requires unsafe.Pointer; return value not meaningful

	protected := unsafe.Slice(output.pbData, output.cbData) //nolint:gosec // Windows API requires unsafe.Pointer
	clone := make([]byte, len(protected))
	copy(clone, protected)
	return clone, nil
}

func unprotectResumeHandle(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}
	input := rhDataBlob{cbData: uint32(len(data)), pbData: &data[0]} //nolint:gosec // Windows API integer conversion, value fits
	var output rhDataBlob
	result, _, err := procCryptUnprotectDataRH.Call(
		uintptr(unsafe.Pointer(&input)), //nolint:gosec // Windows API requires unsafe.Pointer
		0, 0, 0, 0, 0,
		uintptr(unsafe.Pointer(&output)), //nolint:gosec // Windows API requires unsafe.Pointer
	)
	if result == 0 {
		return nil, err
	}
	defer procLocalFreeRH.Call(uintptr(unsafe.Pointer(output.pbData))) //nolint:errcheck,gosec // Windows API requires unsafe.Pointer; return value not meaningful

	plain := unsafe.Slice(output.pbData, output.cbData) //nolint:gosec // Windows API requires unsafe.Pointer
	clone := make([]byte, len(plain))
	copy(clone, plain)
	return clone, nil
}

type rhDataBlob struct {
	cbData uint32
	pbData *byte
}

var (
	crypt32RH                = windows.NewLazySystemDLL("Crypt32.dll")
	kernel32RH               = windows.NewLazySystemDLL("Kernel32.dll")
	procCryptProtectDataRH   = crypt32RH.NewProc("CryptProtectData")
	procCryptUnprotectDataRH = crypt32RH.NewProc("CryptUnprotectData")
	procLocalFreeRH          = kernel32RH.NewProc("LocalFree")
)
