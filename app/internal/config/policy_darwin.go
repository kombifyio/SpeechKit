//go:build darwin

package config

import (
	"context"
	"fmt"
	"io/fs"
	"syscall"
	"time"
)

// ReadPolicyValues reads the MDM-delivered managed preferences of the
// registered PolicyIdentity from /Library/Managed Preferences. Nothing else
// on macOS is a policy source (see policy_plist.go). Without a registered
// identity nothing is read.
func ReadPolicyValues() PolicyValues {
	return readManagedPreferencesFor(ManagedPreferencesDir, rootOwnedNotShared)
}

// rootOwnedNotShared accepts a file owned by root that neither its group nor
// other users can write: what mdmclient writes, and nothing a user can plant.
func rootOwnedNotShared(info fs.FileInfo) error {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("%w: owner unknown", ErrManagedPreferencesUntrusted)
	}
	if st.Uid != 0 {
		return fmt.Errorf("%w: not owned by root", ErrManagedPreferencesUntrusted)
	}
	if info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("%w: writable by group or others", ErrManagedPreferencesUntrusted)
	}
	return nil
}

// watchPolicyChanges re-reads the managed preferences every interval; MDM
// rewrites the file when a profile changes.
func watchPolicyChanges(ctx context.Context, interval time.Duration, notify func()) {
	pollPolicyChanges(ctx, interval, notify)
}
