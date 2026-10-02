package procguard

import (
	"os"
	"strings"
	"sync"
)

// A local sidecar (whisper-server, llama-server, Piper, the wake-word
// helpers) needs the operating system's basics and the GPU runtime's knobs,
// not the host's credentials. An exec.Cmd without Env inherits everything:
// every provider API key in the user's environment, and LLAMA_ARG_* settings
// that switch llama-server endpoints on behind the host's back.

// sidecarEnvKeep are names a sidecar always receives, whatever the deny
// rules below say: the process cannot start or find its temp dir without them.
var sidecarEnvKeep = map[string]bool{
	"PATH": true, "PATHEXT": true,
	"SYSTEMROOT": true, "SYSTEMDRIVE": true, "WINDIR": true,
	"TEMP": true, "TMP": true, "TMPDIR": true,
	"HOME": true, "USERPROFILE": true, "LOCALAPPDATA": true, "APPDATA": true,
	"LANG": true, "LANGUAGE": true,
}

// sidecarEnvDropPrefixes are variable families that carry cloud credentials,
// secret-manager tokens or SpeechKit's own settings, plus LLAMA_* which
// llama-server reads as command-line flags (the host sets the ones it means).
var sidecarEnvDropPrefixes = []string{
	"AWS_", "AZURE_", "GOOGLE_", "GCLOUD_", "DOPPLER_",
	"SPEECHKIT_", "KOMBIFY_", "LLAMA_",
}

// sidecarEnvDropSuffixes catch credentials by their conventional names
// (OPENAI_API_KEY, HF_TOKEN, CLOUDFLARE_API_TOKEN, …).
var sidecarEnvDropSuffixes = []string{
	"_API_KEY", "_APIKEY", "_KEY", "_TOKEN", "_SECRET", "_PASSWORD", "_CREDENTIALS",
}

var (
	sidecarEnvMu    sync.RWMutex
	sidecarEnvExtra []string
)

// SetSidecarEnvDrop registers further variable names that [HostSidecarEnv]
// drops, for credentials whose environment variable name is configurable
// and therefore matches no convention. It replaces the previous set.
func SetSidecarEnvDrop(names []string) {
	cleaned := make([]string, 0, len(names))
	for _, name := range names {
		if name = strings.TrimSpace(name); name != "" {
			cleaned = append(cleaned, name)
		}
	}
	sidecarEnvMu.Lock()
	sidecarEnvExtra = cleaned
	sidecarEnvMu.Unlock()
}

// HostSidecarEnv is [SidecarEnv] applied to this process's environment and
// the names registered with [SetSidecarEnvDrop]. Assign it to exec.Cmd.Env
// before starting a local sidecar.
func HostSidecarEnv() []string {
	sidecarEnvMu.RLock()
	extra := append([]string(nil), sidecarEnvExtra...)
	sidecarEnvMu.RUnlock()
	return SidecarEnv(os.Environ(), extra...)
}

// SidecarEnv returns the entries of environ ("NAME=value") a local sidecar
// may inherit. It drops the credential and configuration families listed
// above and every name in drop; it keeps the operating-system basics (PATH,
// SystemRoot, TEMP/TMP, HOME/USERPROFILE, LANG and LC_*) and everything else,
// GPU runtime variables (CUDA_*, HIP_*, VULKAN_*, GGML_*, METAL_*) included.
// Names compare case-insensitively, as Windows does.
func SidecarEnv(environ []string, drop ...string) []string {
	dropNames := make(map[string]bool, len(drop))
	for _, name := range drop {
		if name = strings.ToUpper(strings.TrimSpace(name)); name != "" {
			dropNames[name] = true
		}
	}
	out := make([]string, 0, len(environ))
	for _, entry := range environ {
		name, _, _ := strings.Cut(entry, "=")
		// Windows keeps per-drive working directories as "=C:=C:\…" entries;
		// they have no name and carry nothing secret.
		if name == "" || sidecarEnvAllowed(strings.ToUpper(name), dropNames) {
			out = append(out, entry)
		}
	}
	return out
}

func sidecarEnvAllowed(name string, dropNames map[string]bool) bool {
	if sidecarEnvKeep[name] || strings.HasPrefix(name, "LC_") {
		return true
	}
	if dropNames[name] {
		return false
	}
	for _, prefix := range sidecarEnvDropPrefixes {
		if strings.HasPrefix(name, prefix) {
			return false
		}
	}
	for _, suffix := range sidecarEnvDropSuffixes {
		if strings.HasSuffix(name, suffix) {
			return false
		}
	}
	return true
}
