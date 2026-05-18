//go:build !windows

package config

// ReadPolicyValues returns a zero-value overlay on non-Windows platforms.
// The Server-Target (Linux container) uses this stub so the registry-reading
// code path is never compiled into the Linux binary. The PolicyValues struct
// itself lives in policy.go (build-tag-neutral) so platform-neutral tests of
// applyPolicyOverlay still compile here.
func ReadPolicyValues() PolicyValues {
	return PolicyValues{Origin: "non-windows"}
}
