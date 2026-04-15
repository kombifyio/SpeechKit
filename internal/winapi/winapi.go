// Package winapi provides shared Windows DLL proc references used by multiple packages.
package winapi

import "golang.org/x/sys/windows"

var (
	User32   = windows.NewLazyDLL("user32.dll")
	Kernel32 = windows.NewLazyDLL("kernel32.dll")

	RtlMoveMemory = Kernel32.NewProc("RtlMoveMemory")
)
