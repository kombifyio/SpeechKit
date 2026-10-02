//go:build !windows

package safepath

// isRemoteVolume has no portable equivalent outside Windows: network mounts
// there are ordinary directories, and the UNC prefix check in IsNetworkPath
// is the part that applies.
func isRemoteVolume(string) bool { return false }
