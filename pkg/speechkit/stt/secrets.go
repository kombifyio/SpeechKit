package stt

import "os"

// resolveSecret resolves a named secret. It defaults to reading the process
// environment so the STT adapters stay free of any host-specific secret
// backend (and importable from pkg/speechkit/** without internal/config).
// Hosts with a richer secret store (environment + Doppler + token store) inject
// their resolver once at startup via SetSecretResolver.
var resolveSecret = os.Getenv

// ResolveSecret reads a named secret through the configured resolver.
// Provider packages call it instead of touching the environment, so a
// host that injected its own resolver stays in control of every lookup.
func ResolveSecret(name string) string { return resolveSecret(name) }

// SetSecretResolver overrides how this package resolves named secrets (see
// resolveSecret). A nil resolver is ignored. Call once at startup, before
// constructing or using providers; the resolver is read when a provider opens
// a credentialed connection.
func SetSecretResolver(fn func(string) string) {
	if fn != nil {
		resolveSecret = fn
	}
}
