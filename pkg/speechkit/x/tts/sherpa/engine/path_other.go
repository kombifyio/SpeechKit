//go:build !windows

package engine

// nativePath returns path unchanged: outside Windows the C API receives the
// UTF-8 path as is.
func nativePath(path string) string { return path }
