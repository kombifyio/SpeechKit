//go:build windows

package local

import (
	"golang.org/x/sys/windows"
)

// asciiShortPath returns the 8.3 short name of path when the volume provides
// one and it is ASCII-only; otherwise "".
func asciiShortPath(path string) string {
	long, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return ""
	}
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n, err := windows.GetShortPathName(long, &buf[0], uint32(len(buf)))
	if err != nil || n == 0 || n >= uint32(len(buf)) {
		return ""
	}
	short := windows.UTF16ToString(buf[:n])
	if !isASCII(short) {
		return ""
	}
	return short
}
