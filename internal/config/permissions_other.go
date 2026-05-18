//go:build !windows

package config

import "os"

// checkConfigFilePermissionsOS checks file permissions using POSIX mode bits.
// The Windows equivalent walks the NTFS DACL (see permissions_windows.go).
func checkConfigFilePermissionsOS(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	return configPermissionWarning(path, info.Mode().Perm()), nil
}
