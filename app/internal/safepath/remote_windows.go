//go:build windows

package safepath

import (
	"path/filepath"

	"golang.org/x/sys/windows"
)

// isRemoteVolume reports whether path sits on a drive letter that Windows
// maps to a network share (net use Z: \\host\share). GetDriveType takes the
// volume root with a trailing backslash.
func isRemoteVolume(path string) bool {
	vol := filepath.VolumeName(path)
	if len(vol) != 2 || vol[1] != ':' {
		return false
	}
	root, err := windows.UTF16PtrFromString(vol + `\`)
	if err != nil {
		return false
	}
	return windows.GetDriveType(root) == windows.DRIVE_REMOTE
}
