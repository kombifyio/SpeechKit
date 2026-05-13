//go:build windows

package main

import (
	"errors"

	"golang.org/x/sys/windows"
)

// singletonMutexName is held for the lifetime of the desktop process to
// prevent a second instance from racing through bootstrap and registering
// global Windows hotkeys before Wails' own SingleInstance check fires
// inside app.Run(). The name is deliberately distinct from Wails'
// internal mutex (derived from the UniqueID "com.kombify.speechkit") so
// the two singleton mechanisms don't observe each other.
const singletonMutexName = `Local\com.kombify.speechkit.singleton-belt`

// tryAcquireSingleton creates a Windows-session-scoped named mutex.
//
// Returns isFirst=true and a release func when this is the first instance
// in the user session. Returns isFirst=false when another instance is
// already running. The caller is expected to exit the process in that
// case before any global resources (hotkeys, audio capture) are claimed.
//
// On unexpected errors, isFirst defaults to true so a transient API
// failure does not block the user from launching the app at all.
func tryAcquireSingleton() (release func(), isFirst bool, err error) {
	namePtr, perr := windows.UTF16PtrFromString(singletonMutexName)
	if perr != nil {
		return nil, true, perr
	}
	handle, cerr := windows.CreateMutex(nil, false, namePtr)
	if errors.Is(cerr, windows.ERROR_ALREADY_EXISTS) {
		if handle != 0 {
			_ = windows.CloseHandle(handle)
		}
		return nil, false, nil
	}
	if cerr != nil {
		if handle != 0 {
			_ = windows.CloseHandle(handle)
		}
		return nil, true, cerr
	}
	return func() { _ = windows.CloseHandle(handle) }, true, nil
}
