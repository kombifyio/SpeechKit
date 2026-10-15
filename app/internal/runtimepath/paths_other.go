//go:build !windows && !darwin

package runtimepath

// Linux and the other Unix targets keep the environment-variable layout the
// package always had: the Server-Target container and the local E2E
// harness set APPDATA/LOCALAPPDATA explicitly, and there is no desktop
// client on these platforms yet that would want an XDG layout.

func platformDataDir(name string) string      { return envDataDir(name) }
func platformLocalDataDir(name string) string { return envLocalDataDir(name) }
func platformSecretsDir(name string) string   { return configDirSecretsDir(name) }

func platformContentDir(name string) string  { return platformDataDir(name) }
func platformLegacyContentDir(string) string { return "" }

func platformAllowsPortable(string) bool { return true }
