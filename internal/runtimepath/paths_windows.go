//go:build windows

package runtimepath

// Windows installs live in %LOCALAPPDATA%\SpeechKit (installer/speechkit.nsi)
// with roaming data under %APPDATA%\SpeechKit and secrets under the user
// config directory.

func platformDataDir() string      { return envDataDir() }
func platformLocalDataDir() string { return envLocalDataDir() }
func platformSecretsDir() string   { return configDirSecretsDir() }

// platformAllowsPortable: every location may host a portable bundle on
// Windows; Program Files and the uninstaller marker are checked by IsPortable.
func platformAllowsPortable(string) bool { return true }
