package config

import "strings"

// ScreenshotDeniedProcesses returns the process image names whose windows a
// Meeting screenshot must refuse: the configured [meeting]
// screenshot_denied_processes list together with the machine policy
// Meeting\ScreenshotDeniedProcesses as it is right now. The policy is read at
// call time, so an entry an administrator adds applies to the next capture
// without a restart, and a deny list is never replaced by a narrower one.
// Entries are normalized (see NormalizeProcessImage), empty ones dropped.
func ScreenshotDeniedProcesses(configured []string) []string {
	return unionPolicyList(normalizeProcessImages(configured), normalizeProcessImages(readPolicyValues().MeetingScreenshotDeniedProcesses))
}

// NormalizeProcessImage reduces a process image name to what is compared:
// the file name without any directory, trimmed and lower-case. A path is
// accepted so a pasted full path still names the process.
func NormalizeProcessImage(value string) string {
	value = strings.TrimSpace(value)
	if i := strings.LastIndexAny(value, `\/`); i >= 0 {
		value = value[i+1:]
	}
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeProcessImages(in []string) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		if n := NormalizeProcessImage(v); n != "" {
			out = append(out, n)
		}
	}
	return out
}
