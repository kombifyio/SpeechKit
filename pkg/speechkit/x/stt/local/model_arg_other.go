//go:build !windows

package local

// asciiShortPath has no equivalent outside Windows; POSIX argv is passed as
// raw bytes so the UTF-8 path reaches whisper-server unchanged.
func asciiShortPath(string) string { return "" }
