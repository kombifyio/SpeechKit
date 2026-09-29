//go:build linux

package ttsapi

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/kombifyio/SpeechKit/internal/server/httpx"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/tts"
)

// OpenAISpeechPath is the OpenAI-compatible speech route. It lets OpenAI-audio
// clients (Open WebUI with AUDIO_TTS_ENGINE=openai, the OpenAI SDKs) use
// SpeechKit's TTS router as their speech base URL and returns raw audio bytes.
const OpenAISpeechPath = "/v1/audio/speech"

// openAISpeechMaxInput mirrors the OpenAI audio/speech input ceiling.
const openAISpeechMaxInput = 4096

// openAIStockVoices are OpenAI's built-in voice names. OpenAI clients send
// them by default (Open WebUI: AUDIO_TTS_VOICE=alloy), but they mean nothing
// to local engines, so they select the configured provider default voice.
var openAIStockVoices = map[string]struct{}{
	"alloy": {}, "ash": {}, "ballad": {}, "cedar": {}, "coral": {}, "echo": {}, "fable": {},
	"marin": {}, "nova": {}, "onyx": {}, "sage": {}, "shimmer": {}, "verse": {},
}

type openAISpeechRequest struct {
	Model          string  `json:"model"`
	Input          string  `json:"input"`
	Voice          string  `json:"voice"`
	ResponseFormat string  `json:"response_format"`
	Speed          float64 `json:"speed"`
}

// MountOpenAICompat registers the OpenAI-compatible speech route.
func (h *Handler) MountOpenAICompat(mux *http.ServeMux) {
	mux.HandleFunc(OpenAISpeechPath, h.openAISpeech)
}

func (h *Handler) openAISpeech(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if h.router == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "tts_unavailable", "TTS router is not configured")
		return
	}
	var body openAISpeechRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	input := strings.TrimSpace(body.Input)
	if input == "" {
		httpx.WriteError(w, http.StatusBadRequest, "missing_input", "input is required")
		return
	}
	if utf8.RuneCountInString(input) > openAISpeechMaxInput {
		httpx.WriteError(w, http.StatusBadRequest, "input_too_long",
			fmt.Sprintf("input exceeds %d characters", openAISpeechMaxInput))
		return
	}
	format := strings.ToLower(strings.TrimSpace(body.ResponseFormat))
	switch format {
	case "":
		format = "mp3" // OpenAI's default response format.
	case "mp3", "wav", "opus", "pcm":
	default:
		httpx.WriteError(w, http.StatusBadRequest, "unsupported_response_format",
			fmt.Sprintf("response_format %q is not supported; use mp3, wav, opus or pcm", body.ResponseFormat))
		return
	}
	voice := strings.TrimSpace(body.Voice)
	if _, stock := openAIStockVoices[strings.ToLower(voice)]; stock {
		voice = ""
	}

	result, err := h.router.Synthesize(r.Context(), input, tts.SynthesizeOpts{
		Voice:  voice,
		Speed:  body.Speed,
		Format: format,
	})
	if err != nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "tts_failed", err.Error())
		return
	}
	if result == nil || len(result.Audio) == 0 {
		httpx.WriteError(w, http.StatusServiceUnavailable, "tts_failed", "TTS provider returned no audio")
		return
	}
	// Providers may not honor every requested format (Piper always emits
	// WAV). The Content-Type reports what was actually produced so clients
	// can transcode; Open WebUI does so for any non-MP3 response.
	w.Header().Set("Content-Type", speechContentType(result))
	if result.Provider != "" {
		w.Header().Set("X-SpeechKit-TTS-Provider", result.Provider)
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(result.Audio)
}

func speechContentType(result *tts.Result) string {
	switch strings.ToLower(strings.TrimSpace(result.Format)) {
	case "wav":
		return "audio/wav"
	case "opus", "ogg":
		return "audio/ogg"
	case "pcm":
		rate := result.SampleRate
		if rate <= 0 {
			rate = 24000
		}
		return "audio/pcm;rate=" + strconv.Itoa(rate) + ";channels=1"
	default:
		return "audio/mpeg"
	}
}
