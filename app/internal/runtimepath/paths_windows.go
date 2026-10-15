//go:build windows

package runtimepath

// Windows installs live in %LOCALAPPDATA%\<name> (installer/speechkit.nsi)
// with roaming data under %APPDATA%\<name> and secrets under the user config
// directory; <name> is the product's DataDirName.

func platformDataDir(name string) string      { return envDataDir(name) }
func platformLocalDataDir(name string) string { return envLocalDataDir(name) }
func platformSecretsDir(name string) string   { return configDirSecretsDir(name) }

// platformContentDir: user content is machine-local on Windows. Without
// LOCALAPPDATA envLocalDataDir falls back to the roaming directory, so a
// stripped environment still lands in the user's own profile.
func platformContentDir(name string) string { return envLocalDataDir(name) }

// platformLegacyContentDir is where releases before the Local AppData move
// kept the database, recordings and snapshots.
func platformLegacyContentDir(name string) string { return envDataDir(name) }

// platformAllowsPortable: every location may host a portable bundle on
// Windows; Program Files and the uninstaller marker are checked by IsPortable.
func platformAllowsPortable(string) bool { return true }
