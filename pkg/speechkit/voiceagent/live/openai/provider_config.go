package openai

import (
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
)

func buildOpenAISession(cfg live.LiveConfig) map[string]any {
	resolved := live.ResolveLiveOptions("openai", "realtime.openai.gpt-realtime-2", cfg, nil, nil)
	activity := ResolveActivityDetection(cfg.Policies.ActivityDetection, resolved)
	turnDetection := buildOpenAITurnDetection(activity)
	inputAudio := map[string]any{
		"format": map[string]any{
			"type": "audio/pcm",
			"rate": openaiInputSampleRate,
		},
	}
	if turnDetection != nil {
		inputAudio["turn_detection"] = turnDetection
	}
	if cfg.Policies.EnableInputAudioTranscription {
		inputAudio["transcription"] = map[string]any{
			"model": "whisper-1",
		}
	}
	session := map[string]any{
		"type":              "realtime",
		"model":             resolveOpenAIRealtimeModel(cfg.Model),
		"output_modalities": []string{"audio"},
		"audio": map[string]any{
			"input": inputAudio,
			"output": map[string]any{
				"format": map[string]any{
					"type": "audio/pcm",
					"rate": openaiInputSampleRate,
				},
				"voice": firstNonEmptyOpenAIVoice(resolved.Voice),
			},
		},
	}
	if tools := buildOpenAITools(cfg.Tools); len(tools) > 0 {
		session["tools"] = tools
		session["tool_choice"] = "auto"
	}
	if effort := strings.TrimSpace(resolved.ReasoningEffort); effort != "" {
		session["reasoning"] = map[string]any{"effort": effort}
	}
	return session
}

// ResolveActivityDetection applies the resolved endpointing and
// turn-detection option overrides to the kernel's activity policy: an
// endpointing override turns automatic detection on with that silence
// duration, and an explicit turn_detection=false turns it off. Providers
// that share the Realtime turn_detection contract reuse it.
func ResolveActivityDetection(policy live.ActivityDetectionPolicy, resolved live.ResolvedLiveOptions) live.ActivityDetectionPolicy {
	if resolved.HasEndpointingOverride() && resolved.EndpointingMs > 0 {
		policy.Automatic = true
		policy.SilenceDurationMs = endpointingMsToInt32(resolved.EndpointingMs)
	}
	if resolved.HasTurnDetectionOverride() && !resolved.TurnDetection {
		policy.Automatic = false
	}
	return policy
}

// BuildTurnDetection translates the kernel's activity policy into the
// Realtime API's server_vad turn_detection object. nil means push-to-talk:
// the kernel commits the audio buffer itself.
func BuildTurnDetection(policy live.ActivityDetectionPolicy) map[string]any {
	return buildOpenAITurnDetection(policy)
}

// BuildTools translates kernel tool definitions into Realtime session tool
// entries. Foundry Voice Live shares the function schema and reuses it.
func BuildTools(defs []live.ToolDefinition) []map[string]any {
	return buildOpenAITools(defs)
}

func endpointingMsToInt32(value int) int32 {
	if value <= 0 {
		return 0
	}
	const maxInt32 = int(^uint32(0) >> 1)
	if value > maxInt32 {
		return int32(maxInt32)
	}
	return int32(value) // #nosec G115 -- value is clamped to int32 range above.
}

// firstNonEmptyOpenAIVoice maps the kernel's voice name to a value the
// Realtime API accepts. OpenAI exposes a fixed set of named voices. Unknown
// Unsupported SpeechKit voice names intentionally fall back to alloy so switching
// provider=OpenAI does not turn a valid server config into a failed session.
func firstNonEmptyOpenAIVoice(voice string) string {
	v := strings.ToLower(strings.TrimSpace(voice))
	if v == "" {
		return "alloy"
	}
	switch v {
	case "alloy", "ash", "ballad", "cedar", "coral", "echo", "marin", "sage", "shimmer", "verse":
		return v
	default:
		return "alloy"
	}
}

// buildOpenAITurnDetection translates the kernel's live.ActivityDetectionPolicy
// into the Realtime API's `turn_detection` object. Returning nil disables
// server-side VAD entirely (push-to-talk mode where the kernel commits the
// audio buffer manually).
func buildOpenAITurnDetection(policy live.ActivityDetectionPolicy) map[string]any {
	if !policy.Automatic {
		return nil
	}
	td := map[string]any{
		"type":               "server_vad",
		"create_response":    true,
		"interrupt_response": true,
	}
	if policy.SilenceDurationMs > 0 {
		td["silence_duration_ms"] = int(policy.SilenceDurationMs)
	}
	if policy.PrefixPaddingMs > 0 {
		td["prefix_padding_ms"] = int(policy.PrefixPaddingMs)
	}
	// OpenAI's VAD threshold runs 0.0–1.0. We map kernel sensitivity:
	// low = trip late (high threshold), high = trip early (low threshold).
	switch strings.ToLower(string(policy.StartSensitivity)) {
	case "low":
		td["threshold"] = 0.7
	case "high":
		td["threshold"] = 0.3
	case "medium":
		td["threshold"] = 0.5
	}
	return td
}

// buildOpenAITools translates kernel ToolDefinitions into Realtime
// session.update tool entries.
func buildOpenAITools(defs []live.ToolDefinition) []map[string]any {
	if len(defs) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(defs))
	for _, def := range defs {
		entry := map[string]any{
			"type":        "function",
			"name":        def.Name,
			"description": def.Description,
		}
		if def.ParametersJSONSchema != nil {
			entry["parameters"] = def.ParametersJSONSchema
		}
		out = append(out, entry)
	}
	return out
}
