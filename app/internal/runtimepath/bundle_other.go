//go:build !darwin

package runtimepath

// A macOS application bundle is the only layout with a Contents directory to
// resolve, so everywhere else these report "no bundle" rather than guessing a
// directory. Callers treat an empty string as "look somewhere else", which is
// what keeps a bundle-aware lookup working unchanged on Windows and Linux.
//
// The darwin implementations are in paths_darwin.go.

// BundleDir is "" outside a macOS application bundle.
func BundleDir() string { return "" }

// BundleResourcesDir is "" outside a macOS application bundle.
func BundleResourcesDir() string { return "" }

// BundleHelpersDir is "" outside a macOS application bundle.
func BundleHelpersDir() string { return "" }
