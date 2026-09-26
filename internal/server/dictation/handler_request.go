//go:build linux

package dictation

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
)

type transcribeJSONRequest struct {
	AudioBase64 string `json:"audio_base64"`
	Format      string `json:"format"`   // "wav" | "mp3" | "pcm16"
	Language    string `json:"language"` // "de" | "en" | "auto"
	Model       string `json:"model"`
	Prompt      string `json:"prompt"`
	// ProviderProfileID explicitly pins the STT provider profile for this
	// request (ops parity with the streaming `start` frame). Validated
	// against the Dictation provider-profile catalog; unknown IDs are
	// rejected with 400 invalid_provider_profile. When omitted, the server
	// resolves the provider from the edge-injected user preference headers
	// and its configured ModelSelection primary.
	ProviderProfileID string `json:"provider_profile_id"`
	// ConversationContext carries preceding dialogue turns (oldest first, no
	// speaker labels) for providers whose speech models condition on
	// conversational context — e.g. AssemblyAI Universal-3.5 Pro sync.
	// Providers without native support ignore it.
	ConversationContext []string        `json:"conversation_context"`
	Speaker             speaker.Options `json:"speaker"`
	SpeakerOptions      speaker.Options `json:"speaker_options"`
}

func resolveSpeakerOptions(primary, fallback speaker.Options) speaker.Options {
	if primary.WantsDiarization() || primary.Enabled {
		return primary.Normalized()
	}
	return fallback.Normalized()
}

func parseSpeakerOptionsFromForm(r *http.Request) speaker.Options {
	if r == nil {
		return speaker.Options{}
	}
	opts := speaker.Options{
		Enabled:             parseFormBool(r, "speaker_enabled"),
		Diarization:         parseFormBool(r, "speaker_diarization"),
		Identification:      parseFormBool(r, "speaker_identification"),
		Attribution:         parseFormBool(r, "speaker_attribution"),
		ProviderProfileID:   strings.TrimSpace(r.FormValue("speaker_provider_profile_id")),
		Model:               strings.TrimSpace(r.FormValue("speaker_model")),
		DiarizationModel:    strings.TrimSpace(r.FormValue("speaker_diarization_model")),
		SpeakerType:         strings.TrimSpace(r.FormValue("speaker_type")),
		SpeakersExpected:    parseFormInt(r, "speakers_expected"),
		MinSpeakersExpected: parseFormInt(r, "speaker_min"),
		MaxSpeakersExpected: parseFormInt(r, "speaker_max"),
		KnownValues:         splitCSV(r.FormValue("speaker_known_values")),
	}
	return opts.Normalized()
}

func parseFormBool(r *http.Request, name string) bool {
	switch strings.ToLower(strings.TrimSpace(r.FormValue(name))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func parseFormInt(r *http.Request, name string) int {
	raw := strings.TrimSpace(r.FormValue(name))
	if raw == "" {
		return 0
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 0
	}
	return value
}

func splitCSV(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
