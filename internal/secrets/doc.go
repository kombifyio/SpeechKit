// Package secrets is the cross-platform credential store with the
// canonical User > Install > Env > None resolution hierarchy. Wraps
// the platform secret store (DPAPI on Windows, file fallback
// elsewhere) so provider API keys never have to live in config.toml
// on disk in plaintext.
//
// Audit 2026-05-24 maintainability sweep.
package secrets
