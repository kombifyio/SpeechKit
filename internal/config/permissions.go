package config

import (
	"fmt"
	"os"
	"runtime"
)

// insecureConfigPermissionBits are the mode bits that indicate the config file
// is readable or writable by users other than the owner. config.toml may hold
// provider API keys, DSNs, or Doppler references, so it should be owner-only
// on multi-user systems.
const insecureConfigPermissionBits os.FileMode = 0o077

// checkConfigFilePermissions returns a non-empty warning message if the given
// file is readable or writable by group/world on a POSIX system. On Windows
// the Go file-mode bits are a simulation of Unix permissions and do not
// reflect NTFS ACLs, so the check is skipped and an empty string is returned.
//
// The warning is non-fatal: callers are expected to log it and continue so a
// misconfigured permission does not prevent the app from running.
func checkConfigFilePermissions(path string) (string, error) {
	if runtime.GOOS == "windows" {
		return "", nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	return configPermissionWarning(path, info.Mode().Perm()), nil
}

// configPermissionWarning is the pure-logic half of checkConfigFilePermissions
// so it can be unit-tested on any platform without touching the filesystem.
func configPermissionWarning(path string, perm os.FileMode) string {
	if perm&insecureConfigPermissionBits == 0 {
		return ""
	}
	return fmt.Sprintf(
		"config file %s has insecure permissions %#o (group/world accessible); "+
			"recommend running: chmod 600 %s",
		path, perm, path,
	)
}
