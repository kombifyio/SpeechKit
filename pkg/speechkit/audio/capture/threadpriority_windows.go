//go:build windows

package capture

import "golang.org/x/sys/windows"

// threadPriorityAboveNormal matches the Win32 THREAD_PRIORITY_ABOVE_NORMAL
// constant (not exposed by x/sys/windows). Inlined here (instead of the
// reference app's internal/winapi helper) so this public package stays
// free of internal/ imports.
const threadPriorityAboveNormal = 1

var procSetThreadPriority = windows.NewLazySystemDLL("kernel32.dll").NewProc("SetThreadPriority")

// setCurrentThreadPriority sets the calling OS thread's scheduling
// priority. Callers should pin the goroutine with runtime.LockOSThread
// first, otherwise the raised priority lands on an arbitrary thread.
func setCurrentThreadPriority(priority int32) error {
	r1, _, err := procSetThreadPriority.Call(uintptr(windows.CurrentThread()), uintptr(priority))
	if r1 == 0 {
		return err
	}
	return nil
}
