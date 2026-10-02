package config

import (
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Providers that can write a meeting up.
const (
	// MeetingProviderLocal is the models that run on this device (the bundled
	// model or a local Ollama). A meeting transcript never leaves the machine
	// on this route.
	MeetingProviderLocal = "local"
	// MeetingProviderFoundry is the Microsoft Foundry utility deployment.
	MeetingProviderFoundry = "foundry"
	// MeetingProviderCopilot is GitHub Copilot with its own processing grant.
	MeetingProviderCopilot = "github_copilot"
)

// MeetingFoundryTranscriptGrantVersion is bumped when what the Foundry grant
// covers changes, so an older grant no longer counts.
const MeetingFoundryTranscriptGrantVersion = 1

// IsMeetingGenerationProvider reports whether id names a provider that can
// write a meeting up.
func IsMeetingGenerationProvider(id string) bool {
	switch id {
	case MeetingProviderLocal, MeetingProviderFoundry, MeetingProviderCopilot:
		return true
	default:
		return false
	}
}

// ProviderOrder is the order a meeting is written up in: the primary, then
// each fallback once.
func (m MeetingConfig) ProviderOrder() []string {
	order := make([]string, 0, 1+len(m.FallbackProviders))
	seen := map[string]bool{}
	for _, id := range append([]string{m.GenerationProvider}, m.FallbackProviders...) {
		id = strings.TrimSpace(id)
		if !IsMeetingGenerationProvider(id) || seen[id] {
			continue
		}
		seen[id] = true
		order = append(order, id)
	}
	return order
}

// UsesProvider reports whether a meeting may be written up by the provider.
func (m MeetingConfig) UsesProvider(id string) bool {
	for _, candidate := range m.ProviderOrder() {
		if candidate == id {
			return true
		}
	}
	return false
}

// HasFoundryTranscriptGrant reports whether the user allowed meeting
// transcripts to be sent to Microsoft Foundry.
func (m MeetingConfig) HasFoundryTranscriptGrant() bool {
	return m.FoundryTranscriptGrantVersion == MeetingFoundryTranscriptGrantVersion &&
		strings.TrimSpace(m.FoundryTranscriptGrantGrantedAt) != ""
}

// GrantFoundryTranscripts records the permission to send meeting transcripts
// to Microsoft Foundry.
func (m *MeetingConfig) GrantFoundryTranscripts(now time.Time) {
	m.FoundryTranscriptGrantVersion = MeetingFoundryTranscriptGrantVersion
	m.FoundryTranscriptGrantGrantedAt = now.UTC().Format(time.RFC3339)
}

// RevokeFoundryTranscripts withdraws that permission.
func (m *MeetingConfig) RevokeFoundryTranscripts() {
	m.FoundryTranscriptGrantVersion = 0
	m.FoundryTranscriptGrantGrantedAt = ""
}

// NormalizeMeetingGeneration makes the provider settings consistent: an
// unknown primary becomes the local route, and the fallbacks keep only known
// providers, once each, without the primary.
func NormalizeMeetingGeneration(cfg *Config) {
	if cfg == nil {
		return
	}
	cfg.Meeting.GenerationProvider = strings.TrimSpace(cfg.Meeting.GenerationProvider)
	if !IsMeetingGenerationProvider(cfg.Meeting.GenerationProvider) {
		cfg.Meeting.GenerationProvider = MeetingProviderLocal
	}
	order := cfg.Meeting.ProviderOrder()
	cfg.Meeting.FallbackProviders = append([]string{}, order[1:]...)
}

// migrateMeetingFallbackPolicy folds the removed [meeting].fallback_policy
// into fallback_providers. "allow_local_fallback" meant: fall back to the
// local route. It never meant anything with the local route as primary.
//
// The local route used to include every configured cloud utility model, so a
// meeting on "local" could reach Microsoft Foundry without the user choosing
// it. The route is now on-device only; Foundry has to be picked, with its
// transcript permission, in Meeting settings.
func migrateMeetingFallbackPolicy(meta toml.MetaData, cfg *Config) {
	legacy := strings.TrimSpace(cfg.Meeting.FallbackPolicy)
	cfg.Meeting.FallbackPolicy = ""
	if meta.IsDefined("meeting", "fallback_providers") || legacy != "allow_local_fallback" {
		return
	}
	if strings.TrimSpace(cfg.Meeting.GenerationProvider) != MeetingProviderLocal {
		cfg.Meeting.FallbackProviders = []string{MeetingProviderLocal}
	}
}
