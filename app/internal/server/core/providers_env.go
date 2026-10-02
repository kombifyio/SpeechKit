//go:build linux

package core

import (
	"github.com/kombifyio/SpeechKit/app/internal/config"
)

// anyCloudKeyEnvSet reports whether any cloud-provider credential env var
// resolves to a non-empty value at the current point in time. Used by
// /v1/deployment/status's providers.cloud_keys_present field. The names come
// from config.CloudCredentialEnvNames, the same list
// scripts/install-server.sh --strict-local-only refuses and the install-E2E
// gates clear.
func anyCloudKeyEnvSet(cfg *config.Config) bool {
	for _, name := range config.CloudCredentialEnvNames(cfg) {
		if envPresent(name) {
			return true
		}
	}
	return false
}
