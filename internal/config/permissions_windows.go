//go:build windows

package config

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// insecureSIDs are well-known SIDs whose presence in the DACL with
// access-allowed rights is treated as insecure for a config file that may
// hold provider API keys or other secrets.
var insecureSIDs = []string{
	"S-1-1-0",      // Everyone
	"S-1-5-11",     // Authenticated Users
	"S-1-5-32-545", // BUILTIN\Users
}

// checkConfigFilePermissionsOS reads the file's DACL via GetNamedSecurityInfo
// and walks each access-allowed ACE. A warning is returned (non-fatal) when:
//   - the DACL is NULL (full access for everyone), or
//   - any access-allowed ACE grants rights to one of the insecureSIDs.
//
// Why DACL instead of os.FileMode: Go's os.FileMode on Windows reflects a
// synthetic Unix-mode approximation that does not represent the actual NTFS
// access control list. A config file containing provider API keys must be
// checked against the real DACL; GetNamedSecurityInfo is the canonical entry
// point for that on Windows.
func checkConfigFilePermissionsOS(path string) (string, error) {
	if _, err := os.Stat(path); err != nil {
		return "", err
	}

	sd, err := windows.GetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		return "", fmt.Errorf("GetNamedSecurityInfo(%q): %w", path, err)
	}

	// The second return (defaulted) is not relevant: a defaulted DACL with
	// insecure ACEs is still insecure.
	dacl, _, err := sd.DACL()
	if err != nil {
		return "", fmt.Errorf("DACL(): %w", err)
	}
	if dacl == nil {
		// NULL DACL means full access for everyone — always insecure.
		return fmt.Sprintf(
			"config file %s has a NULL DACL (full access for everyone); "+
				"recommend restricting to current user with: "+
				`icacls %q /inheritance:r /grant:r "%%USERNAME%%:(R,W)"`,
			path, path,
		), nil
	}

	insecure, badSID := daclGrantsToInsecureSID(dacl)
	if insecure {
		return fmt.Sprintf(
			"config file %s has an ACE granting access to %s; "+
				"recommend restricting to current user with: "+
				`icacls %q /inheritance:r /grant:r "%%USERNAME%%:(R,W)"`,
			path, badSID, path,
		), nil
	}
	return "", nil
}

// daclGrantsToInsecureSID returns (true, sidString) if any access-allowed ACE
// in the DACL targets a SID we consider insecure. Extracted as a pure function
// so it can be unit-tested with a hand-crafted DACL.
//
// Inherited ACEs are intentionally included: an Everyone-read ACE inherited
// from the parent directory is just as dangerous as an explicitly-set one.
// Returns the first matching SID so the warning names a concrete fix target.
//
// GetAce returns a *ACCESS_ALLOWED_ACE regardless of the actual ACE type; we
// guard with a AceType check so we only inspect ACCESS_ALLOWED_ACE entries and
// skip deny/object ACEs.  The SID is inline in the ACE starting at SidStart —
// the canonical unsafe cast `(*windows.SID)(unsafe.Pointer(&ace.SidStart))` is
// confirmed by golang.org/x/sys/windows/syscall_windows_test.go:505.
func daclGrantsToInsecureSID(dacl *windows.ACL) (bool, string) {
	for i := uint32(0); i < uint32(dacl.AceCount); i++ { //nolint:gosec // G115: AceCount is always small; truncation not possible.
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil {
			// Skip unreadable ACEs rather than aborting.
			continue
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			continue
		}
		// SidStart is the first uint32 of the SID embedded inline in the ACE.
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart)) //nolint:gosec // G103: canonical Windows ACL pattern, documented in x/sys tests.
		sidStr := sid.String()
		for _, bad := range insecureSIDs {
			if sidStr == bad {
				return true, bad
			}
		}
	}
	return false, ""
}
