package config

import (
	"fmt"
	"path/filepath"
	"strings"

	framework "github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/catalog"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/x/procguard"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/x/stt/local"
)

// BindRuntimePrivacy runs again at the composition root after startup settings
// and deployment overlays. The boot binding itself is once-only and immutable.
func BindRuntimePrivacy(c *Config) error {
	if err := netsec.BindBootProfile(); err != nil {
		return err
	}
	if netsec.FullLocal() {
		ApplyEgressPolicy(c)
	}
	return c.ValidateFullLocalSelections()
}

// applyFullLocalDefaults runs before TOML decoding. Explicit selections are
// preserved and checked later; only this deployment's absent-key defaults
// differ. It does not enable a runtime or choose/download a voice asset.
func applyFullLocalDefaults(c *Config) {
	if !netsec.FullLocal() {
		return
	}
	for _, profile := range catalog.ProfilesForMode(framework.ModeTTS) {
		if allowed, _ := framework.ProfileAllowedForBoot(profile); allowed {
			c.ModelSelection.TTS.PrimaryProfileID = profile.ID
			c.ModelSelection.TTS.FallbackProfileID = ""
			return
		}
	}
	// A missing catalog engine is a closed configuration, never an empty
	// selection that the router could reinterpret as an inferred default.
	c.TTS.Enabled = false
}

// fullLocalConstructionAllowed checks the configured engine, not its provider
// family. The server's vps adapter is also the LAN whisper.cpp HTTP path; its
// transport remains guarded independently of the model identity check here.
func (c *Config) fullLocalConstructionAllowed(adapter, modality string) bool {
	if !netsec.FullLocal() {
		return true
	}
	switch modality {
	case provideropts.ModalitySTT:
		switch adapter {
		case "local":
			return c != nil && strings.TrimSpace(c.Local.ModelPath) != "" && !local.IsSherpaModel(c.Local.ModelPath)
		case "vps", "selfhosted":
			return c != nil && c.NetworkScope() != framework.NetworkScopeDeviceOnly && fullLocalWhisperModel(c.VPS.Model)
		}
	case provideropts.ModalityTTS:
		return adapter == "piper" || adapter == "sherpa_piper"
	case "":
		// Local LLM reasoning is separate from speech engine identity.
		return adapter == "local" || adapter == "ollama"
	}
	return false
}

func fullLocalWhisperModel(model string) bool {
	profile, ok := catalog.DefaultCatalog().Profile(DefaultDictatePrimaryProfileID)
	if !ok {
		return false
	}
	model = strings.TrimSpace(model)
	if model == profile.ModelID {
		return true
	}
	for _, variant := range profile.Variants {
		if model == variant.ModelID || filepath.Base(model) == variant.ModelID {
			return true
		}
	}
	return false
}

// ValidateFullLocalSelections rejects explicit primary/fallback engine changes
// before a save or activation can construct a replacement. Disabled selections
// stay stored so the deployment ceiling never rewrites Standard preferences.
func (c *Config) ValidateFullLocalSelections() error {
	if !netsec.FullLocal() || c == nil {
		return nil
	}
	for _, selection := range []struct {
		name    string
		enabled bool
		value   ModeModelSelection
	}{
		{"dictate", c.General.DictateEnabled || c.VPS.Enabled || c.Local.Enabled, c.ModelSelection.Dictate},
		{"tts", c.TTS.Enabled, c.ModelSelection.TTS},
		{"assist", c.General.AssistEnabled, c.ModelSelection.Assist},
		{"voice_agent", c.General.VoiceAgentEnabled, c.ModelSelection.VoiceAgent},
	} {
		if !selection.enabled {
			continue
		}
		for _, id := range []string{selection.value.PrimaryProfileID, selection.value.FallbackProfileID} {
			if strings.TrimSpace(id) == "" {
				continue
			}
			profile, ok := catalog.DefaultCatalog().Profile(id)
			if !ok {
				return fmt.Errorf("data_profile_full_local: unknown %s profile %q", selection.name, id)
			}
			if allowed, reason := framework.ProfileAllowedForBoot(profile); !allowed {
				return fmt.Errorf("%s: %s profile %q", reason, selection.name, id)
			}
		}
	}
	return nil
}

func validateFullLocalServerSettings(settings ServerModelSettings) error {
	if !netsec.FullLocal() {
		return nil
	}
	for _, mode := range []ServerModeSetting{settings.Modes.Dictation, settings.Modes.Assist, settings.Modes.VoiceAgent} {
		if mode.Enabled != nil && !*mode.Enabled {
			continue
		}
		if id := strings.TrimSpace(mode.ProfileID); id != "" {
			profile, ok := catalog.DefaultCatalog().Profile(id)
			if !ok {
				return fmt.Errorf("data_profile_full_local: unknown server profile %q", id)
			}
			if allowed, reason := framework.ProfileAllowedForBoot(profile); !allowed {
				return fmt.Errorf("%s: server profile %q", reason, id)
			}
		}
	}
	if model := strings.TrimSpace(settings.STT.Model); model != "" && !fullLocalWhisperModel(model) {
		return fmt.Errorf("data_profile_full_local: server STT model %q is not an admitted Whisper catalog model", model)
	}
	if model := strings.TrimSpace(settings.Modes.Dictation.Model); model != "" && !fullLocalWhisperModel(model) {
		return fmt.Errorf("data_profile_full_local: dictation model %q is not an admitted Whisper catalog model", model)
	}
	return nil
}

// EffectivePrivacy exposes the same immutable facts the transport enforces.
// It includes configuration identity, not an assertion of artifact qualification.
func (c *Config) EffectivePrivacy() map[string]any {
	facts, err := netsec.BoundBootFacts()
	result := map[string]any{
		"boot": facts, "network_scope": c.NetworkScope(), "allow_setup_traffic": c.SetupTrafficAllowed(),
		"full_local": netsec.FullLocal(),
		"stt_model":  c.VPS.Model, "local_stt_model": filepath.Base(c.Local.ModelPath),
		"stt_primary_profile_id":  c.ModelSelection.Dictate.PrimaryProfileID,
		"stt_fallback_profile_id": c.ModelSelection.Dictate.FallbackProfileID,
		"tts_primary_profile_id":  c.ModelSelection.TTS.PrimaryProfileID,
		"tts_fallback_profile_id": c.ModelSelection.TTS.FallbackProfileID,
		"piper_enabled":           c.TTS.Enabled && c.TTS.Piper.Enabled,
		"sherpa_piper_enabled":    c.TTS.Enabled && (c.TTS.Sherpa.Enabled || c.ModelSelection.TTS.PrimaryProfileID == DefaultTTSPrimaryProfileID),
		"piper_configured_voices": c.TTS.Piper.DefaultVoices,
	}
	if err != nil {
		result["error"] = err.Error()
	}
	if err := procguard.RequireSpeechConfinement(); err != nil {
		result["native_speech_launch"] = "blocked"
		result["native_speech_launch_reason"] = err.Error()
	}
	for _, item := range []struct{ adapter, modality string }{{"local", provideropts.ModalitySTT}, {"vps", provideropts.ModalitySTT}, {"piper", provideropts.ModalityTTS}, {"sherpa_piper", provideropts.ModalityTTS}, {"openedai", provideropts.ModalityTTS}} {
		allowed, reason := c.ProviderConstructionAllowed(item.adapter, item.modality)
		result[item.adapter+"_admitted"] = allowed
		if !allowed {
			result[item.adapter+"_reason"] = reason
		}
	}
	return result
}
