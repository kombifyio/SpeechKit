//go:build !windows && !darwin

package runtimepath

// Linux and the other Unix targets keep the environment-variable layout the
// package always had: the Server-Target container and the local E2E
// harness set APPDATA/LOCALAPPDATA explicitly, and there is no desktop
// client on these platforms yet that would want an XDG layout.

func platformDataDir() string      { return envDataDir() }
func platformLocalDataDir() string { return envLocalDataDir() }
func platformSecretsDir() string   { return configDirSecretsDir() }

func platformAllowsPortable(string) bool { return true }
