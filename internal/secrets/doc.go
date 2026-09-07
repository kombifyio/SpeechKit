// Package secrets is the cross-platform credential store with the
// canonical User > Install > Env > None resolution hierarchy. Wraps
// the platform secret store so provider API keys never have to live in
// config.toml on disk in plaintext.
//
// Backends: Windows seals each secret file with DPAPI; macOS seals each
// secret file with AES-256-GCM under a random 32-byte master key held in
// one login-Keychain generic-password item, which costs one Keychain
// prompt per build identity instead of one per secret. Both keep the
// blobs under runtimepath.SecretsDir(). Every other target has no
// encrypted store and fails closed with ErrSecureStoreUnavailable, so
// callers fall back to environment or Doppler-managed credentials
// rather than to plaintext on disk.
//
// Audit 2026-05-24 maintainability sweep.
package secrets
