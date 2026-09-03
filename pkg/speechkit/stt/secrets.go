package stt

import "os"

// SecretResolver resolves a named secret (typically an environment-variable
// name) to its value. An empty string means "not set".
//
// Providers that read credentials lazily (for example the Google streaming
// adapter, which loads service-account JSON on first stream) expose a
// SecretResolver field so each provider instance can be bound to its own
// secret backend. That keeps two SpeechKit runtimes in one process — a test
// harness, a multi-tenant server — independent of each other. There is no
// process-wide resolver: hosts bind secrets per provider (or through
// allproviders.EnabledProviders.Secrets).
type SecretResolver func(name string) string

// EnvSecretResolver reads secrets from the process environment. It is the
// default every provider falls back to when no resolver is configured.
func EnvSecretResolver(name string) string { return os.Getenv(name) }

// Resolve returns r(name). A nil resolver reads the process environment, so
// provider packages can call it on a zero-value field.
func (r SecretResolver) Resolve(name string) string {
	if r == nil {
		return EnvSecretResolver(name)
	}
	return r(name)
}
