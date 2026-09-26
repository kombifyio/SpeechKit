//go:build linux

package assist

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
)

// ── request / response shapes ───────────────────────────────────────────────

type processJSONRequest struct {
	Text        string `json:"text"`
	AudioBase64 string `json:"audio_base64"`
	Format      string `json:"format"`
	Locale      string `json:"locale"`
	Selection   string `json:"selection"`
	Context     string `json:"context"`
	// App and WindowTitle describe the foreground application on the
	// integrating client. The server has no desktop, so the caller
	// supplies these; the kernel folds them into the LLM context block.
	App         string `json:"app"`
	WindowTitle string `json:"window_title"`
	// TTS overrides — the Pipeline already knows whether TTS is globally
	// enabled; these fields let the caller opt out per-request.
	TTS            *bool           `json:"tts,omitempty"`
	TTSFormat      string          `json:"tts_format,omitempty"`
	TTSVoice       string          `json:"tts_voice,omitempty"`
	TTSSpeed       float64         `json:"tts_speed,omitempty"`
	Speaker        speaker.Options `json:"speaker,omitempty"`
	SpeakerOptions speaker.Options `json:"speaker_options,omitempty"`
}

func audioFormatHint(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "wav":
		return "audio/wav"
	case "mp3":
		return "audio/mpeg"
	case "pcm16", "pcm":
		return "audio/L16;rate=16000;channels=1"
	default:
		return ""
	}
}

// parseTTSOverrides interprets multipart form fields. "tts" is parsed as a
// permissive boolean; other fields pass through verbatim.
func parseTTSOverrides(ttsRaw, ttsFormat, ttsVoice string) (override *bool, format, voice string) {
	if trimmed := strings.TrimSpace(ttsRaw); trimmed != "" {
		switch strings.ToLower(trimmed) {
		case "1", "true", "yes", "on":
			t := true
			override = &t
		case "0", "false", "no", "off":
			f := false
			override = &f
		}
	}
	return override, strings.TrimSpace(ttsFormat), strings.TrimSpace(ttsVoice)
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
