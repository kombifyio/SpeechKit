//go:build linux

package core

import (
	"strings"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/server/persona"
	vsserver "github.com/kombifyio/SpeechKit/internal/server/voiceagent"
	"github.com/kombifyio/SpeechKit/internal/voiceagentprofile"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live/openai"
)

// ── persona resolver ────────────────────────────────────────────────────────

// personaResolver implements vsserver.PersonaResolver by composing a
// LiveConfigFrame from three layers, in order of precedence:
//
//  1. explicit fields on the client-sent StartFrame (highest priority)
//  2. the resolved persona + role + sequence step from the registry
//  3. the server-wide [voice_agent] config (lowest)
//
// This preserves the previous behaviour when no persona_id and no configured
// agent profile are supplied, while giving clients full control when they do
// pick a persona.
type personaResolver struct {
	cfg      *config.Config
	registry *persona.Registry
}

func (r *personaResolver) Resolve(start vsserver.StartFrame) (vsserver.LiveConfigFrame, error) {
	return r.resolve(start, 0)
}

func (r *personaResolver) ResolveStep(start vsserver.StartFrame, stepIndex int) (vsserver.LiveConfigFrame, error) {
	return r.resolve(start, stepIndex)
}

func normalizeStartSpeakerOptions(opts *speaker.Options) speaker.Options {
	if opts == nil {
		return speaker.Options{}
	}
	return opts.Normalized()
}

func (r *personaResolver) resolve(start vsserver.StartFrame, stepIndex int) (vsserver.LiveConfigFrame, error) {
	va := r.cfg.VoiceAgent

	// The adapter normalises start.Provider to the resolved backend before
	// calling Resolve, so the API key + model match THIS session's provider
	// (empty falls back to the configured default provider's credentials).
	provider := normalizeVoiceAgentProvider(start.Provider)
	if provider == "" {
		provider = normalizeVoiceAgentProvider(va.Provider)
	}

	frame := vsserver.LiveConfigFrame{
		Model:             r.resolveModel(start.Model, provider),
		FallbackModel:     va.FallbackModel,
		APIKey:            resolveRealtimeAPIKey(r.cfg, provider),
		Voice:             firstNonEmpty(start.Voice, va.Voice),
		SystemPrompt:      firstNonEmpty(start.SystemPromptOverride, va.FrameworkPrompt),
		RefinementPrompt:  va.RefinementPrompt,
		Locale:            firstNonEmpty(start.Locale, r.cfg.General.Language, "en"),
		Automatic:         va.AutomaticActivityDetection,
		StartSensitivity:  va.VADStartSensitivity,
		EndSensitivity:    va.VADEndSensitivity,
		PrefixPaddingMs:   intToInt32Clamp(va.VADPrefixPaddingMs),
		SilenceDurationMs: intToInt32Clamp(va.VADSilenceDurationMs),
		ActivityHandling:  va.ActivityHandling,
		TurnCoverage:      va.TurnCoverage,
		Speaker:           normalizeStartSpeakerOptions(start.Speaker),
	}

	// Layer (2): persona + role + sequence. Explicit start-frame personas win;
	// otherwise a non-default server-wide agent profile acts as the default
	// persona for clients that do not send persona_id yet.
	personaID := strings.TrimSpace(start.PersonaID)
	if personaID == "" {
		if configured := voiceagentprofile.NormalizeID(va.AgentProfileID); configured != voiceagentprofile.DefaultID {
			personaID = configured
		}
	}
	if personaID != "" && r.registry != nil {
		resolved, err := r.registry.Resolve(personaID, start.RoleID, start.SequenceID, stepIndex)
		if err != nil {
			return vsserver.LiveConfigFrame{}, err
		}
		frame.PersonaID = resolved.PersonaID
		frame.RoleID = resolved.RoleID
		frame.SequenceID = resolved.SequenceID
		frame.SequenceCompletion = resolved.SequenceCompletion
		frame.SequenceMaxTurns = resolved.SequenceMaxTurns
		frame.StepID = resolved.StepID
		frame.StepIndex = resolved.StepIndex
		frame.StepCount = resolved.StepCount
		frame.StepInstruction = resolved.StepInstruction
		frame.StepExitCriteria = resolved.StepExitCriteria
		frame.StepMaxTurns = resolved.StepMaxTurns
		if strings.TrimSpace(start.Voice) == "" && resolved.Voice != "" {
			frame.Voice = resolved.Voice
		}
		if strings.TrimSpace(start.Locale) == "" && resolved.Locale != "" {
			frame.Locale = resolved.Locale
		}
		if strings.TrimSpace(start.SystemPromptOverride) == "" && resolved.SystemPrompt != "" {
			frame.SystemPrompt = resolved.SystemPrompt
		} else if strings.TrimSpace(start.SystemPromptOverride) != "" && resolved.StepInstruction != "" {
			frame.SystemPrompt = composeStartOverrideWithStep(start.SystemPromptOverride, resolved.StepID, resolved.StepInstruction)
		}
		if resolved.RefinementPrompt != "" {
			frame.RefinementPrompt = resolved.RefinementPrompt
		}
		// Role VAD/activity fields override config defaults only when they
		// are non-empty — this keeps roles minimal without wiping server
		// defaults that the admin cares about.
		if resolved.AutomaticVAD {
			frame.Automatic = true
		}
		if resolved.StartSensitivity != "" {
			frame.StartSensitivity = resolved.StartSensitivity
		}
		if resolved.EndSensitivity != "" {
			frame.EndSensitivity = resolved.EndSensitivity
		}
		if resolved.PrefixPaddingMs != 0 {
			frame.PrefixPaddingMs = resolved.PrefixPaddingMs
		}
		if resolved.SilenceDurationMs != 0 {
			frame.SilenceDurationMs = resolved.SilenceDurationMs
		}
		if resolved.ActivityHandling != "" {
			frame.ActivityHandling = resolved.ActivityHandling
		}
		if resolved.TurnCoverage != "" {
			frame.TurnCoverage = resolved.TurnCoverage
		}
	}

	// Layer (1): explicit client activity-detection override.
	if start.ActivityDetection != nil {
		ad := start.ActivityDetection
		frame.Automatic = ad.Automatic
		if ad.StartSensitivity != "" {
			frame.StartSensitivity = ad.StartSensitivity
		}
		if ad.EndSensitivity != "" {
			frame.EndSensitivity = ad.EndSensitivity
		}
		if ad.PrefixPaddingMs != 0 {
			frame.PrefixPaddingMs = ad.PrefixPaddingMs
		}
		if ad.SilenceDurationMs != 0 {
			frame.SilenceDurationMs = ad.SilenceDurationMs
		}
		if ad.ActivityHandling != "" {
			frame.ActivityHandling = ad.ActivityHandling
		}
		if ad.TurnCoverage != "" {
			frame.TurnCoverage = ad.TurnCoverage
		}
	}
	return frame, nil
}

func intToInt32Clamp(value int) int32 {
	const (
		maxInt32 = 1<<31 - 1
		minInt32 = -1 << 31
	)
	switch {
	case value > maxInt32:
		return maxInt32
	case value < minInt32:
		return minInt32
	default:
		return int32(value) // #nosec G115 -- value is clamped to the int32 range above.
	}
}

func (r *personaResolver) resolveModel(startModel, provider string) string {
	if explicit := strings.TrimSpace(startModel); explicit != "" {
		return explicit
	}
	provider = normalizeVoiceAgentProvider(provider)
	if provider == ProviderOpenAI {
		return firstNonEmpty(r.cfg.Providers.OpenAI.RealtimeModel, liveDefaultModel(provider), openai.DefaultRealtimeModel)
	}
	configuredProvider := normalizeVoiceAgentProvider(r.cfg.VoiceAgent.Provider)
	if provider == configuredProvider && strings.TrimSpace(r.cfg.VoiceAgent.Model) != "" {
		return strings.TrimSpace(r.cfg.VoiceAgent.Model)
	}
	if model := liveDefaultModel(provider); model != "" {
		return model
	}
	return ""
}

func liveDefaultModel(provider string) string {
	cfg, ok := live.DefaultLiveConfigForProvider(provider)
	if !ok {
		return ""
	}
	return strings.TrimSpace(cfg.Model)
}

func composeStartOverrideWithStep(prompt, stepID, stepInstruction string) string {
	prompt = strings.TrimSpace(prompt)
	stepInstruction = strings.TrimSpace(stepInstruction)
	if stepInstruction == "" {
		return prompt
	}
	if prompt == "" {
		return stepInstruction
	}
	return prompt + "\n\n[Current step: " + stepID + "]\n" + stepInstruction
}

// resolveRealtimeAPIKey selects the API key matching the configured Voice
// Agent provider. The persona resolver receives this so the LiveConfigFrame
// it produces carries the right credential downstream — Gemini Live, Deepgram,
// AssemblyAI, OpenAI Realtime, and the Cascaded pipeline have distinct env vars.
func resolveRealtimeAPIKey(cfg *config.Config, provider string) string {
	value, _, err := config.ResolveProviderCredentialValue(cfg, realtimeCredentialTarget(provider))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(value)
}

func realtimeCredentialTarget(provider string) string {
	switch normalizeVoiceAgentProvider(provider) {
	case ProviderOpenAI:
		return "openai"
	case ProviderDeepgram:
		return "deepgram"
	case ProviderAssemblyAI:
		return "assemblyai"
	case ProviderGemini:
		return "google"
	default:
		return "openai"
	}
}
