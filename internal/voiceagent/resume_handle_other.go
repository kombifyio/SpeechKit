//go:build !windows

package voiceagent

// On non-Windows platforms there is no readily-available, dependency-free
// equivalent of DPAPI's user-scoped CryptProtectData. We rely on the TTL in
// resumeHandle plus process memory isolation. The handle is still kept only
// in memory — never written to disk.
func protectResumeHandle(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}
	clone := make([]byte, len(data))
	copy(clone, data)
	return clone, nil
}

func unprotectResumeHandle(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}
	clone := make([]byte, len(data))
	copy(clone, data)
	return clone, nil
}
