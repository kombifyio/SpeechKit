//go:build windows

package engine

import (
	"unicode/utf8"

	"golang.org/x/sys/windows"
)

// nativePath returns a path the sherpa-onnx C API can open. It takes paths as
// char*, which do not survive the round trip on Windows when they hold
// non-ASCII characters (an umlaut in the user name under %LOCALAPPDATA% or
// %TEMP% is enough), so such a path is replaced by its 8.3 short name. A
// volume without short names keeps the long path.
func nativePath(path string) string {
	if isASCII(path) {
		return path
	}
	long, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return path
	}
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n, err := windows.GetShortPathName(long, &buf[0], uint32(len(buf)))
	if err != nil || n == 0 || n >= uint32(len(buf)) {
		return path
	}
	if short := windows.UTF16ToString(buf[:n]); isASCII(short) {
		return short
	}
	return path
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}
