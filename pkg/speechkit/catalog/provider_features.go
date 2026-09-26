package catalog

import (
	"sort"
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
)

func providerFeatureSupports(profiles []ProviderDefault) []ProviderFeatureSupport {
	out := make([]ProviderFeatureSupport, 0, len(providerFeatureOrder))
	for _, feature := range providerFeatureOrder {
		out = append(out, bestFeatureSupport(profiles, feature))
	}
	return out
}

func bestFeatureSupport(profiles []ProviderDefault, feature ProviderFeature) ProviderFeatureSupport {
	best := ProviderFeatureSupport{
		Feature: feature,
		Support: ProviderSupportUnsupported,
		Mode:    modeForProviderFeature(feature),
	}
	for _, profile := range profiles {
		candidate, ok := featureSupportForProfile(profile, feature)
		if !ok {
			continue
		}
		if supportRank(candidate.Support) > supportRank(best.Support) {
			best = candidate
		}
	}
	return best
}

func featureSupportForProfile(profile ProviderDefault, feature ProviderFeature) (ProviderFeatureSupport, bool) {
	support := ProviderFeatureSupport{
		Feature:       feature,
		Support:       profile.Support,
		Mode:          profile.Mode,
		ProfileID:     profile.ProfileID,
		ModelID:       profile.ModelID,
		NativeOptions: append([]string(nil), profile.NativeOptions...),
		EvidenceURL:   profile.EvidenceURL,
	}
	switch feature {
	case ProviderFeatureDictation:
		return support, profile.Mode == speechkit.ModeDictation && providerDefaultHasCapability(profile, speechkit.CapabilityTranscription)
	case ProviderFeatureDictationStreaming:
		if profile.Mode != speechkit.ModeDictation || !providerDefaultHasCapability(profile, speechkit.CapabilityTranscription) {
			return ProviderFeatureSupport{}, false
		}
		if providerDefaultHasCapability(profile, speechkit.CapabilityNativeDictationStream) {
			support.Support = ProviderSupportNative
		} else {
			support.Support = ProviderSupportCascaded
		}
		return support, true
	case ProviderFeatureLongTranscription:
		return support, profile.Mode == speechkit.ModeDictation && providerDefaultHasCapability(profile, speechkit.CapabilityTranscription)
	case ProviderFeatureSpeakerDiarization:
		return support, profile.Mode == speechkit.ModeDictation &&
			(providerDefaultHasCapability(profile, speechkit.CapabilitySpeakerDiarization) ||
				providerDefaultHasCapability(profile, speechkit.CapabilitySpeakerAttribution) ||
				providerDefaultHasCapability(profile, speechkit.CapabilitySpeakerIdentification))
	case ProviderFeatureSpeakerIdentification:
		return support, profile.Mode == speechkit.ModeDictation &&
			(providerDefaultHasCapability(profile, speechkit.CapabilitySpeakerIdentification) ||
				providerDefaultHasCapability(profile, speechkit.CapabilitySpeakerAttribution))
	case ProviderFeatureAssist:
		return support, profile.Mode == speechkit.ModeAssist && providerDefaultHasCapability(profile, speechkit.CapabilityLLM)
	case ProviderFeatureRealtimeVoice:
		if profile.Mode != speechkit.ModeVoiceAgent {
			return ProviderFeatureSupport{}, false
		}
		if providerDefaultHasCapability(profile, speechkit.CapabilityRealtimeAudio) {
			return support, true
		}
		if providerDefaultHasCapability(profile, speechkit.CapabilityPipelineFallback) {
			support.Support = ProviderSupportCascaded
			return support, true
		}
		return ProviderFeatureSupport{}, false
	case ProviderFeatureTTS:
		return support, profile.Mode == speechkit.ModeTTS && providerDefaultHasCapability(profile, speechkit.CapabilityTTS)
	default:
		return ProviderFeatureSupport{}, false
	}
}

func nativeOptionsForProfile(provider string, profile speechkit.ProviderProfile) []string {
	seen := map[string]bool{}
	var out []string
	for _, option := range profile.NativeOptions {
		option = strings.TrimSpace(option)
		if option != "" && !seen[option] {
			seen[option] = true
			out = append(out, option)
		}
	}
	modality := modalityForProviderMode(profile.Mode)
	if modality != "" {
		if manifest, ok := provideropts.FindManifest(provider, modality); ok {
			for _, option := range manifest.Options {
				if option.Status != provideropts.SupportNative {
					continue
				}
				id := strings.TrimSpace(string(option.ID))
				if id != "" && !seen[id] {
					seen[id] = true
					out = append(out, id)
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

func modalityForProviderMode(mode speechkit.Mode) string {
	switch speechkit.NormalizeMode(mode) {
	case speechkit.ModeDictation:
		return provideropts.ModalitySTT
	case speechkit.ModeVoiceAgent:
		return provideropts.ModalityVoiceAgent
	case speechkit.ModeTTS:
		return provideropts.ModalityTTS
	default:
		return ""
	}
}

func modeForProviderFeature(feature ProviderFeature) speechkit.Mode {
	switch feature {
	case ProviderFeatureDictation, ProviderFeatureDictationStreaming, ProviderFeatureLongTranscription,
		ProviderFeatureSpeakerDiarization, ProviderFeatureSpeakerIdentification:
		return speechkit.ModeDictation
	case ProviderFeatureAssist:
		return speechkit.ModeAssist
	case ProviderFeatureRealtimeVoice:
		return speechkit.ModeVoiceAgent
	case ProviderFeatureTTS:
		return speechkit.ModeTTS
	default:
		return speechkit.ModeNone
	}
}
