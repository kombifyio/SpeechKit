package speechkit

import (
	"fmt"
	"strings"
)

// ModeBehavior describes how much mode-specific intelligence a host enables.
type ModeBehavior string

const (
	// ModeBehaviorClean keeps a mode on its core contract, such as strict STT
	// for Dictation or deterministic utility handling for Assist.
	ModeBehaviorClean ModeBehavior = "clean"
	// ModeBehaviorIntelligence allows optional intelligence layers such as
	// LLM utility handling, TTS, summaries, or realtime tool use.
	ModeBehaviorIntelligence ModeBehavior = "intelligence"
)

// RuntimePolicy constrains which parts of the SpeechKit framework a host
// application exposes. Empty EnabledModes or AllowedProfiles mean "all".
type RuntimePolicy struct {
	EnabledModes    []Mode                `json:"enabledModes,omitempty"`
	AllowedProfiles []string              `json:"allowedProfiles,omitempty"`
	FixedProfiles   map[Mode]string       `json:"fixedProfiles,omitempty"`
	AllowFallbacks  bool                  `json:"allowFallbacks,omitempty"`
	ModeBehaviors   map[Mode]ModeBehavior `json:"modeBehaviors,omitempty"`
}

// FilterProviderProfiles returns the profiles visible under policy.
func FilterProviderProfiles(profiles []ProviderProfile, policy RuntimePolicy) []ProviderProfile {
	enabledModes := policyEnabledModeSet(policy.EnabledModes)
	allowedProfiles := policyAllowedProfileSet(policy.AllowedProfiles)
	fixedProfiles := policyFixedProfileSet(policy.FixedProfiles)

	filtered := make([]ProviderProfile, 0, len(profiles))
	for _, profile := range profiles {
		mode := NormalizeMode(profile.Mode)
		if mode == ModeNone || !enabledModes[mode] {
			continue
		}
		if len(allowedProfiles) > 0 && !allowedProfiles[profile.ID] {
			continue
		}
		if fixedProfileID := fixedProfiles[mode]; fixedProfileID != "" && profile.ID != fixedProfileID {
			continue
		}
		if err := ValidateProfileForMode(profile, mode); err != nil {
			continue
		}
		filtered = append(filtered, profile)
	}
	return filtered
}

// ValidateRuntimePolicy checks that a policy references existing profiles and
// does not require a profile that violates its mode contract.
func ValidateRuntimePolicy(profiles []ProviderProfile, policy RuntimePolicy) error {
	byID := providerProfilesByID(profiles)
	enabledModes := policyEnabledModeSet(policy.EnabledModes)
	allowedProfiles := policyAllowedProfileSet(policy.AllowedProfiles)

	for _, profileID := range policy.AllowedProfiles {
		profileID = strings.TrimSpace(profileID)
		if profileID == "" {
			continue
		}
		profile, ok := byID[profileID]
		if !ok {
			return fmt.Errorf("speechkit: allowed profile %q not found", profileID)
		}
		mode := NormalizeMode(profile.Mode)
		if !enabledModes[mode] {
			return fmt.Errorf("speechkit: allowed profile %q belongs to disabled mode %q", profileID, mode)
		}
		if err := ValidateProfileForMode(profile, mode); err != nil {
			return err
		}
	}

	for rawMode, rawProfileID := range policy.FixedProfiles {
		mode := NormalizeMode(rawMode)
		profileID := strings.TrimSpace(rawProfileID)
		if mode == ModeNone {
			return fmt.Errorf("speechkit: fixed profile %q uses unsupported mode %q", profileID, rawMode)
		}
		if !enabledModes[mode] {
			return fmt.Errorf("speechkit: fixed profile %q belongs to disabled mode %q", profileID, mode)
		}
		profile, ok := byID[profileID]
		if !ok {
			return fmt.Errorf("speechkit: fixed profile %q not found", profileID)
		}
		if len(allowedProfiles) > 0 && !allowedProfiles[profileID] {
			return fmt.Errorf("speechkit: fixed profile %q is not allowed by policy", profileID)
		}
		if NormalizeMode(profile.Mode) != mode {
			return fmt.Errorf("speechkit: fixed profile %q belongs to mode %q, not %q", profileID, profile.Mode, mode)
		}
		if err := ValidateProfileForMode(profile, mode); err != nil {
			return err
		}
	}

	return nil
}

// ValidateModeSettingsForPolicy checks mode selections against a RuntimePolicy.
func ValidateModeSettingsForPolicy(profiles []ProviderProfile, settings ModeSettings, policy RuntimePolicy) error {
	if err := ValidateRuntimePolicy(profiles, policy); err != nil {
		return err
	}

	selections := map[Mode]ModeSetting{
		ModeDictation:  settings.Dictation.ModeSetting,
		ModeAssist:     settings.Assist.ModeSetting,
		ModeVoiceAgent: settings.VoiceAgent.ModeSetting,
	}
	enabledModes := policyEnabledModeSet(policy.EnabledModes)
	allowedProfiles := policyAllowedProfileSet(policy.AllowedProfiles)
	fixedProfiles := policyFixedProfileSet(policy.FixedProfiles)
	byID := providerProfilesByID(profiles)

	for mode, setting := range selections {
		if !enabledModes[mode] {
			if setting.Enabled {
				return fmt.Errorf("speechkit: mode %q is disabled by policy", mode)
			}
			continue
		}
		for label, profileID := range map[string]string{
			"primary":  strings.TrimSpace(setting.PrimaryProfileID),
			"fallback": strings.TrimSpace(setting.FallbackProfileID),
		} {
			if profileID == "" {
				continue
			}
			if label == "fallback" && !policy.AllowFallbacks {
				return fmt.Errorf("speechkit: fallback profile %q for %q is disabled by policy", profileID, mode)
			}
			if fixedProfileID := fixedProfiles[mode]; fixedProfileID != "" && profileID != fixedProfileID {
				return fmt.Errorf("speechkit: %s profile %q for %q does not match fixed profile %q", label, profileID, mode, fixedProfileID)
			}
			if len(allowedProfiles) > 0 && !allowedProfiles[profileID] {
				return fmt.Errorf("speechkit: %s profile %q for %q is not allowed by policy", label, profileID, mode)
			}
			profile, ok := byID[profileID]
			if !ok {
				return fmt.Errorf("speechkit: %s profile %q for %q not found", label, profileID, mode)
			}
			if err := ValidateProfileForMode(profile, mode); err != nil {
				return err
			}
		}
	}

	return nil
}

func policyEnabledModeSet(modes []Mode) map[Mode]bool {
	if len(modes) == 0 {
		return map[Mode]bool{
			ModeDictation:  true,
			ModeAssist:     true,
			ModeVoiceAgent: true,
		}
	}
	enabled := map[Mode]bool{}
	for _, mode := range modes {
		mode = NormalizeMode(mode)
		if mode != ModeNone {
			enabled[mode] = true
		}
	}
	return enabled
}

func policyAllowedProfileSet(profileIDs []string) map[string]bool {
	allowed := map[string]bool{}
	for _, profileID := range profileIDs {
		profileID = strings.TrimSpace(profileID)
		if profileID != "" {
			allowed[profileID] = true
		}
	}
	return allowed
}

func policyFixedProfileSet(input map[Mode]string) map[Mode]string {
	fixed := map[Mode]string{}
	for rawMode, rawProfileID := range input {
		mode := NormalizeMode(rawMode)
		profileID := strings.TrimSpace(rawProfileID)
		if mode != ModeNone && profileID != "" {
			fixed[mode] = profileID
		}
	}
	return fixed
}

func providerProfilesByID(profiles []ProviderProfile) map[string]ProviderProfile {
	byID := make(map[string]ProviderProfile, len(profiles))
	for _, profile := range profiles {
		byID[profile.ID] = profile
	}
	return byID
}
