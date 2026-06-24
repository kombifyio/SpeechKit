package config

// Voice Agent configuration types and helpers.

import "strings"

// VoiceAgentConfig configures the real-time Voice Agent Mode.
type VoiceAgentConfig struct {
	Enabled bool `toml:"enabled"`
	// Provider selects the backend that drives a Voice Agent session.
	// Supported values:
	//   ""          (default) — same as "gemini"
	//   "gemini"    — Google Gemini Live (cloud, GOOGLE_AI_API_KEY required)
	//   "openai"    — OpenAI Realtime API (cloud, OPENAI_API_KEY required)
	//   "deepgram"  — Deepgram Voice Agent (cloud, DEEPGRAM_API_KEY required)
	//   "assemblyai" — AssemblyAI Voice Agent API (cloud, ASSEMBLYAI_API_KEY required)
	//   "cascaded"  — self-hosted whisper.cpp → Genkit agent LLM → TTS pipeline
	//                 (CPU-capable; no external realtime dependency)
	//   "moshi"     — self-hosted Kyutai Moshi Rust server (GPU required, M9b)
	//
	// The Server-Target reads this field via cmd/speechkit-server. The Device-
	// Target runs "gemini", "openai", and "deepgram" in-process; other values
	// fall back to Gemini Live (or the pipeline fallback when enabled).
	Provider      string `toml:"provider"`
	Model         string `toml:"model"`          // Real-time model ID (e.g. "gemini-3.1-flash-live-preview")
	FallbackModel string `toml:"fallback_model"` // Fallback real-time model
	Voice         string `toml:"voice"`          // Voice name for real-time model
	// Deepgram Voice Agent think-LLM overrides. The think leg reasons over the
	// transcript; listen (Nova-3) and speak (Aura-2) stay Deepgram. When unset,
	// the kernel default (Deepgram-managed open_ai/gpt-4o-mini) applies. Setting
	// DeepgramThinkEndpointURL + DeepgramThinkAPIKeyEnv switches the think leg to
	// a bring-your-own LLM deployment, with the credential resolved from the
	// named env var (env -> Doppler). Read by the Server- and Device-Target
	// Deepgram Voice Agent wiring; ignored by the Gemini/cascaded backends.
	DeepgramThinkProvider    string `toml:"deepgram_think_provider"`
	DeepgramThinkModel       string `toml:"deepgram_think_model"`
	DeepgramThinkEndpointURL string `toml:"deepgram_think_endpoint_url"`
	DeepgramThinkAPIKeyEnv   string `toml:"deepgram_think_api_key_env"`
	AgentProfileID           string `toml:"agent_profile_id"`  // Built-in Voice Agent profile ID; "default" preserves current behavior.
	AgentSequenceID          string `toml:"agent_sequence_id"` // Optional workflow sequence ID; empty uses the selected persona default.
	FrameworkPrompt          string `toml:"framework_prompt"`  // Durable host/framework instruction that defines the Voice Agent behavior
	RefinementPrompt         string `toml:"refinement_prompt"` // User-specific refinement appended to the framework prompt
	// AutoStartOnLaunch is legacy: it is kept only so backfillStartupBehavior
	// can migrate an old [voice_agent].auto_start_on_launch into the
	// General.AutoStartOnLaunch app-window preference. It no longer starts a
	// Voice Agent session on launch — launching the app presents the app UI,
	// never a live conversation. See dashboardAutoOpenOnLaunch (Device-Target).
	AutoStartOnLaunch bool   `toml:"auto_start_on_launch"`
	CloseBehavior     string `toml:"close_behavior"` // "continue" keeps the conversation window in the taskbar; "new_chat" ends the current chat on close
	// BargeIn controls whether the microphone stays open while the agent is
	// speaking so the user can interrupt mid-answer:
	//   "auto"   (default) — full duplex when the active output device looks
	//            like a headset (closed acoustic path, no speaker bleed);
	//            half duplex otherwise. Evaluated at session start.
	//   "always" — full duplex on every output device. Only sensible with
	//            hardware/OS echo cancellation; without it the agent hears
	//            itself through the speakers and interrupts itself.
	//   "never"  — half duplex: the mic is muted while the agent speaks and
	//            until the buffered answer finished playing.
	BargeIn                string `toml:"barge_in"`
	ReminderAfterIdleSec   int    `toml:"reminder_after_idle_sec"`
	DeactivateAfterIdleSec int    `toml:"deactivate_after_idle_sec"`
	// HoldReleaseGraceSec controls how long the Voice Agent stays open after
	// the user releases a hold-to-talk shortcut so the model has time to
	// deliver its reply. 0 (or unset) falls back to the kernel default
	// (10 seconds). The Device-Target hard-caps this at 30 seconds; values
	// above that are silently clamped at runtime so a misconfigured profile
	// cannot strand the user in a "still active" session.
	HoldReleaseGraceSec int `toml:"hold_release_grace_sec"`
	// WarmSessionLingerSec keeps the realtime session (and its already-paid
	// WebSocket handshake) open for this many seconds after a hold-to-talk
	// answer finishes, so the next press resumes the warm connection instead
	// of re-dialing the provider (~700 ms handshake measured against Deepgram).
	// A press within the window reuses the connection; the window expiring,
	// an explicit stop, or the idle timeout tears it down. 0 disables the
	// linger (legacy deactivate-immediately behaviour). The session summary
	// and the prompter close only run when the conversation truly ends —
	// never on a release that gets resumed inside the window.
	WarmSessionLingerSec int `toml:"warm_session_linger_sec"`
	// PauseToleranceMs filters short silences out of the outgoing mic stream
	// so the realtime provider's server-side endpointing does not fire during
	// brief thinking pauses: silent frames are dropped until the accumulated
	// pause reaches this tolerance, then silence flows again and the provider
	// answers. Effective answer latency ≈ tolerance + provider threshold.
	// 0 disables the filter (provider default endpointing).
	PauseToleranceMs                int                    `toml:"pause_tolerance_ms"`
	PipelineFallback                bool                   `toml:"pipeline_fallback"` // Use STT -> Agent LLM -> optional TTS when the selected Voice Agent profile is not native realtime.
	ShowPrompter                    bool                   `toml:"show_prompter"`     // Show live transcript prompter window
	EnableSessionSummary            bool                   `toml:"enable_session_summary"`
	EnableInputTranscript           bool                   `toml:"enable_input_transcript"`
	EnableOutputTranscript          bool                   `toml:"enable_output_transcript"`
	EnableAffectiveDialog           bool                   `toml:"enable_affective_dialog"`
	ThinkingEnabled                 bool                   `toml:"thinking_enabled"`
	IncludeThoughts                 bool                   `toml:"include_thoughts"`
	ThinkingBudget                  int                    `toml:"thinking_budget"`
	ThinkingLevel                   string                 `toml:"thinking_level"`
	ContextCompressionEnabled       bool                   `toml:"context_compression_enabled"`
	ContextCompressionTriggerTokens int64                  `toml:"context_compression_trigger_tokens"`
	ContextCompressionTargetTokens  int64                  `toml:"context_compression_target_tokens"`
	AutomaticActivityDetection      bool                   `toml:"automatic_activity_detection"`
	ActivityHandling                string                 `toml:"activity_handling"`
	TurnCoverage                    string                 `toml:"turn_coverage"`
	VADStartSensitivity             string                 `toml:"vad_start_sensitivity"`
	VADEndSensitivity               string                 `toml:"vad_end_sensitivity"`
	VADPrefixPaddingMs              int                    `toml:"vad_prefix_padding_ms"`
	VADSilenceDurationMs            int                    `toml:"vad_silence_duration_ms"`
	Limits                          VoiceAgentLimitsConfig `toml:"limits"`
}

// VoiceAgentSessionLimits returns the effective Voice Agent session caps.
// The v0.40.x config surface prefers [voice_agent.limits], while the older
// [server] fields remain supported for existing deployments.
func (cfg *Config) VoiceAgentSessionLimits() VoiceAgentLimitsConfig {
	if cfg == nil {
		return VoiceAgentLimitsConfig{}
	}
	limits := VoiceAgentLimitsConfig{
		MaxGlobalSessions:      cfg.Server.MaxVoiceAgentSessions,
		MaxPerIdentitySessions: cfg.Server.MaxSessionsPerUser,
	}
	if cfg.VoiceAgent.Limits.MaxGlobalSessions > 0 {
		limits.MaxGlobalSessions = cfg.VoiceAgent.Limits.MaxGlobalSessions
	}
	if cfg.VoiceAgent.Limits.MaxPerIdentitySessions > 0 {
		limits.MaxPerIdentitySessions = cfg.VoiceAgent.Limits.MaxPerIdentitySessions
	}
	return limits
}

func (cfg *Config) LegacyAgentHotkey() string {
	if cfg == nil {
		return ""
	}
	return cfg.General.LegacyAgentHotkey()
}

func (g GeneralConfig) LegacyAgentHotkey() string {
	if strings.TrimSpace(g.AgentHotkey) != "" {
		return strings.TrimSpace(g.AgentHotkey)
	}
	if strings.TrimSpace(g.AgentMode) == "voice_agent" {
		return strings.TrimSpace(g.VoiceAgentHotkey)
	}
	return strings.TrimSpace(g.AssistHotkey)
}
