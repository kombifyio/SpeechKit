package config

import (
	"fmt"
	"os"
)

// insecureConfigPermissionBits is the POSIX mode mask that flags a config
// file as readable or writable by users other than the owner. config.toml
// may hold provider API keys, DSNs, or Doppler references, so it must be
// owner-only on multi-user systems. On Windows the equivalent check uses
// the NTFS DACL (see permissions_windows.go); this constant is irrelevant
// there.
const insecureConfigPermissionBits os.FileMode = 0o077

// checkConfigFilePermissions is the stable internal entry point. The
// OS-specific implementation lives in checkConfigFilePermissionsOS
// (permissions_windows.go for NTFS DACLs, permissions_other.go for POSIX).
// Do not collapse this into a direct call without preserving build-tag dispatch.
func checkConfigFilePermissions(path string) (string, error) {
	return checkConfigFilePermissionsOS(path)
}

// configPermissionWarning is the pure-POSIX helper. Kept in the
// build-tag-neutral file so existing TestConfigPermissionWarning runs on
// any platform.
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
