//go:build linux

package assist

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/internal/server/audio"
	"github.com/kombifyio/SpeechKit/internal/server/httpx"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	assistpkg "github.com/kombifyio/SpeechKit/pkg/speechkit/assist"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
)

// processOptions carries the per-request Assist inputs from the HTTP layer
// to the processor call. The active-window fields, the free-form context
// (plus any speaker transcript appended after STT) and the vocabulary hint
// are folded into one context block by request(), so the service, the
// skills and the LLM see the same prompt-neutral block the desktop host
// produces.
type processOptions struct {
	Locale         string
	Selection      string
	Context        string
	ActiveApp      string
	WindowTitle    string
	VocabularyHint string
	SessionKey     string
}

func (o processOptions) request(text string) speechkit.AssistRequest {
	return speechkit.AssistRequest{
		Text:      text,
		Locale:    o.Locale,
		Selection: o.Selection,
		Context: assistpkg.ComposeContext(assistpkg.ContextParts{
			ActiveApp:      o.ActiveApp,
			WindowTitle:    o.WindowTitle,
			Context:        o.Context,
			VocabularyHint: o.VocabularyHint,
		}),
		SessionKey: o.SessionKey,
	}
}

// ── core flow ───────────────────────────────────────────────────────────────

func (h *Handler) processAudio(ctx context.Context, w http.ResponseWriter, reader io.Reader, contentType string, opts processOptions, speakerOpts speaker.Options, ttsOverride *bool, ttsFormat, ttsVoice string) {
	lr := io.LimitReader(reader, h.maxBytes+1)
	raw, err := io.ReadAll(lr)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "read_failed", "failed to read audio: "+err.Error())
		return
	}
	if int64(len(raw)) > h.maxBytes {
		httpx.WriteError(w, http.StatusRequestEntityTooLarge, "payload_too_large",
			fmt.Sprintf("audio exceeds maximum %d bytes", h.maxBytes))
		return
	}
	h.processAudioBytes(ctx, w, raw, contentType, opts, speakerOpts, ttsOverride, ttsFormat, ttsVoice)
}

func (h *Handler) processAudioBytes(ctx context.Context, w http.ResponseWriter, raw []byte, contentType string, opts processOptions, speakerOpts speaker.Options, ttsOverride *bool, ttsFormat, ttsVoice string) {
	if h.transcriber == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "stt_unavailable",
			"this deployment does not expose an STT router; send 'text' instead of 'audio'")
		return
	}

	decoded, err := audio.DecodeWithLimits(ctx, raw, contentType, h.decodeLimits)
	if err != nil {
		if errors.Is(err, audio.ErrUnsupportedFormat) {
			httpx.WriteError(w, http.StatusUnsupportedMediaType, "unsupported_audio_format", err.Error())
			return
		}
		if errors.Is(err, audio.ErrDecodedAudioTooLarge) {
			httpx.WriteError(w, http.StatusRequestEntityTooLarge, "payload_too_large", err.Error())
			return
		}
		httpx.WriteError(w, http.StatusBadRequest, "invalid_audio", "failed to decode audio: "+err.Error())
		return
	}
	if len(decoded.PCM) == 0 {
		httpx.WriteError(w, http.StatusBadRequest, "empty_audio", "decoded audio contains no samples")
		return
	}

	durationSecs := float64(decoded.DurationMs) / 1000.0
	sttOpts, _ := h.applyCustomizationHints(ctx, stt.TranscribeOpts{Language: opts.Locale, Speaker: speakerOpts})
	sttResult, err := h.transcriber.Route(ctx, decoded.PCM, durationSecs, sttOpts)
	if err != nil {
		slog.Warn("assist: STT failed", "err", err, "duration_ms", decoded.DurationMs) // #nosec G706 -- slog writes structured fields; provider errors are attributes, not message format.
		httpx.WriteError(w, http.StatusServiceUnavailable, "stt_provider_unavailable",
			"no STT provider could satisfy the request: "+err.Error())
		return
	}
	if sttResult == nil || strings.TrimSpace(sttResult.Text) == "" {
		httpx.WriteError(w, http.StatusUnprocessableEntity, "empty_transcript",
			"STT returned an empty transcript; the audio may contain no speech")
		return
	}

	if opts.Locale == "" && isConcreteLocale(sttResult.Language) {
		opts.Locale = sttResult.Language
	}
	// Now that STT has had its say, fall back to the server default so the
	// reply and any TTS still have a locale to work with.
	opts.Locale = h.resolveLocale(opts.Locale)
	if sttResult.Speakers != nil {
		opts.Context = appendSpeakerContext(opts.Context, sttResult.Speakers)
	}
	source := &sourceMeta{
		Format:     decoded.SourceFormat,
		SampleRate: decoded.SourceRate,
		Channels:   decoded.SourceCh,
		DurationMs: decoded.DurationMs,
	}
	h.processTranscript(ctx, w, sttResult.Text, source, sttResult.Speakers, opts, ttsOverride, ttsFormat, ttsVoice)
}

func (h *Handler) processTranscript(ctx context.Context, w http.ResponseWriter, transcript string, source *sourceMeta, speakers *speaker.DiarizationResult, opts processOptions, ttsOverride *bool, ttsFormat, ttsVoice string) {
	if strings.TrimSpace(transcript) == "" {
		httpx.WriteError(w, http.StatusBadRequest, "empty_transcript", "transcript or text field must not be empty")
		return
	}
	// Resolve the user's Words/Replacements customization once and use it for
	// both the post-STT transcript replacement and the Assist-LLM vocabulary
	// hint. VoiceAgentHint is the generation-oriented, mode-neutral phrasing
	// ("Prefer these names and product terms …") that the request folds into
	// the context block. Resolving once keeps the two consistent — a second
	// resolve can observe a different store state mid-request — and avoids
	// duplicating the store and template work on every Assist call.
	var customizationActions []speechkit.CustomizationAction
	if resolved, err := h.resolveAssistCustomization(ctx, opts.Locale); err != nil {
		slog.Debug("assist: resolve customization failed", "err", err)
	} else {
		transcript, customizationActions = h.applyResolvedTranscriptCustomization(ctx, resolved, transcript, opts.Locale)
		opts.VocabularyHint = resolved.VoiceAgentHint
	}

	started := time.Now()
	result, err := h.processor.Process(ctx, opts.request(transcript))
	latency := time.Since(started)
	if err != nil {
		slog.Warn("assist: pipeline error", "err", err, "transcript_chars", len(transcript)) // #nosec G706 -- slog writes structured fields; provider errors are attributes, not message format.
		writePipelineError(w, err, latency)
		return
	}
	if strings.TrimSpace(result.Text) == "" {
		httpx.WriteErrorWithDetails(w, http.StatusServiceUnavailable, "pipeline_unavailable", "Assist pipeline returned an empty result", map[string]any{
			"stage":      "llm",
			"category":   "empty_result",
			"retryable":  true,
			"latency_ms": latency.Milliseconds(),
		})
		return
	}

	// Respect TTS opt-out from the request. We cannot opt IN when the
	// service was built without a TTS router — Audio will already be empty
	// in that case.
	if ttsOverride != nil && !*ttsOverride {
		result.Audio = nil
		result.Format = ""
	}
	_ = ttsFormat // reserved for post-synthesis transcoding (v1.1)
	_ = ttsVoice  // reserved for per-request voice override (v1.1)

	resp := processResponse{
		Text:                 result.Text,
		SpeakText:            result.SpeakText,
		Action:               result.Action,
		Locale:               firstNonEmptyString(result.Locale, opts.Locale),
		Shortcut:             result.ShortcutID,
		Surface:              string(result.Surface),
		Kind:                 result.Kind,
		MessageID:            result.MessageID,
		ReasonCode:           result.ReasonCode,
		Transcript:           transcript,
		LatencyMs:            latency.Milliseconds(),
		SourceInfo:           source,
		AudioFormat:          result.Format,
		Speakers:             speakers,
		CustomizationActions: customizationActions,
	}
	if audioBytes := result.Audio.Bytes(); len(audioBytes) > 0 {
		resp.AudioBase64 = base64.StdEncoding.EncodeToString(audioBytes)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// ── helpers ─────────────────────────────────────────────────────────────────

// isConcreteLocale reports whether a language reported by an STT provider
// names an actual locale. Providers echo routing pseudo-values back on the
// result — Deepgram returns "multi" for multilingual code-switching — and
// those must not become the reply/TTS locale.
func isConcreteLocale(language string) bool {
	language = strings.TrimSpace(language)
	if language == "" {
		return false
	}
	switch strings.ToLower(language) {
	case "multi", "auto":
		return false
	}
	return true
}

func appendSpeakerContext(existing string, result *speaker.DiarizationResult) string {
	if result == nil || len(result.Segments) == 0 {
		return existing
	}
	var b strings.Builder
	if strings.TrimSpace(existing) != "" {
		b.WriteString(strings.TrimSpace(existing))
		b.WriteString("\n\n")
	}
	b.WriteString("Speaker transcript:\n")
	for _, segment := range result.Segments {
		label := firstNonEmptyString(segment.DisplayName, segment.Role, segment.SpeakerLabel)
		if label == "" {
			label = "speaker"
		}
		text := strings.TrimSpace(segment.Text)
		if text == "" {
			continue
		}
		b.WriteString("[")
		b.WriteString(label)
		b.WriteString("] ")
		b.WriteString(text)
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}
