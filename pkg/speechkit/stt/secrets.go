package stt

import (
	"os"
	"sync/atomic"
)

// SecretResolver resolves a named secret (typically an environment-variable
// name) to its value. An empty string means "not set".
//
// Providers that read credentials lazily (for example the Google streaming
// adapter, which loads service-account JSON on first stream) expose a
// SecretResolver field so each provider instance can be bound to its own
// secret backend. That keeps two SpeechKit runtimes in one process — a test
// harness, a multi-tenant server — independent of each other.
type SecretResolver func(name string) string

// EnvSecretResolver reads secrets from the process environment. It is the
// default every provider falls back to when no resolver is configured.
func EnvSecretResolver(name string) string { return os.Getenv(name) }

// Resolve returns r(name). A nil resolver falls back to the process-wide
// resolver (see SetSecretResolver), which in turn defaults to the
// environment, so provider packages can call it on a zero-value field and
// existing hosts that still rely on SetSecretResolver keep their behaviour.
func (r SecretResolver) Resolve(name string) string {
	if r == nil {
		return legacyResolveSecret(name)
	}
	return r(name)
}

// legacySecretResolver backs the deprecated package-level SetSecretResolver.
// It is consulted only when a provider has no per-instance resolver.
var legacySecretResolver atomic.Value // SecretResolver

// ResolveSecret reads a named secret through the process-wide resolver
// installed with SetSecretResolver, defaulting to the environment.
//
// Deprecated: process-wide state cannot be scoped per runtime. Set the
// SecretResolver field on the provider (or pass it through the assembly
// options) instead. ResolveSecret keeps working for hosts that still call
// SetSecretResolver.
func ResolveSecret(name string) string { return legacyResolveSecret(name) }

// legacyResolveSecret is the fallback used by providers without a
// per-instance resolver.
func legacyResolveSecret(name string) string {
	if fn, _ := legacySecretResolver.Load().(SecretResolver); fn != nil {
		return fn(name)
	}
	return EnvSecretResolver(name)
}

// SetSecretResolver overrides the process-wide fallback resolver. A nil
// resolver is ignored.
//
// Deprecated: prefer a per-provider SecretResolver (for example
// google.Provider.SecretResolver or allproviders.EnabledProviders.Secrets).
// The process-wide setter remains only so existing hosts keep working; it
// affects every provider in the process that has no resolver of its own.
func SetSecretResolver(fn func(string) string) {
	if fn != nil {
		legacySecretResolver.Store(SecretResolver(fn))
	}
}
