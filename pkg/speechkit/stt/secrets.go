package stt

import "os"

// Default environment-variable names for provider credentials. These are the
// contract between the STT adapters and the host's secret store. Hosts may
// override per provider via the provider's *Env fields; these are the
// fallbacks used when no override is set.
const (
	envGoogleSTTAPIKey          = "SPEECHKIT_GOOGLE_STT_API_KEY"
	envGoogleSTTCredentialsJSON = "SPEECHKIT_GOOGLE_STT_CREDENTIALS_JSON"
	envGoogleApplicationCreds   = "GOOGLE_APPLICATION_CREDENTIALS"
	envGoogleCloudSTTAPIKey     = "GOOGLE_CLOUD_STT_API_KEY"
	envGoogleLegacySTTAPIKey    = "GOOGLE_STT_API_KEY"
	envDeepgramAPIKey           = "DEEPGRAM_API_KEY"
	envAssemblyAIAPIKey         = "ASSEMBLYAI_API_KEY"
)

// resolveSecret resolves a named secret. It defaults to reading the process
// environment so the STT adapters stay free of any host-specific secret
// backend (and importable from pkg/speechkit/** without internal/config).
// Hosts with a richer secret store (environment + Doppler + token store) inject
// their resolver once at startup via SetSecretResolver.
var resolveSecret = os.Getenv

// SetSecretResolver overrides how this package resolves named secrets (see
// resolveSecret). A nil resolver is ignored. Call once at startup, before
// constructing or using providers; the resolver is read when a provider opens
// a credentialed connection.
func SetSecretResolver(fn func(string) string) {
	if fn != nil {
		resolveSecret = fn
	}
}
