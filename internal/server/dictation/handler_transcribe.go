//go:build linux

package dictation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	internalcustomize "github.com/kombifyio/SpeechKit/internal/customize"
	"github.com/kombifyio/SpeechKit/internal/server/audio"
	"github.com/kombifyio/SpeechKit/internal/server/httpx"
	"github.com/kombifyio/SpeechKit/internal/server/storageauth"
	"github.com/kombifyio/SpeechKit/internal/store"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	speechcustomize "github.com/kombifyio/SpeechKit/pkg/speechkit/customize"
	speechstorage "github.com/kombifyio/SpeechKit/pkg/speechkit/storage"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
)

// transcribeAndReply buffers the reader, decodes, and hands off.
func (h *Handler) transcribeAndReply(w http.ResponseWriter, r *http.Request, reader io.Reader, contentType string, opts stt.TranscribeOpts) {
	lr := io.LimitReader(reader, h.maxBytes+1)
	raw, err := io.ReadAll(lr)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "read_failed",
			"failed to read audio: "+err.Error())
		return
	}
	if int64(len(raw)) > h.maxBytes {
		httpx.WriteError(w, http.StatusRequestEntityTooLarge, "payload_too_large",
			fmt.Sprintf("audio exceeds maximum %d bytes", h.maxBytes))
		return
	}
	h.transcribeBytes(w, r, raw, contentType, opts)
}

func (h *Handler) transcribeBytes(w http.ResponseWriter, r *http.Request, raw []byte, contentType string, opts stt.TranscribeOpts) {
	resp, failure := h.transcribe(r, raw, contentType, opts)
	if failure != nil {
		httpx.WriteError(w, failure.status, failure.code, failure.message)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// transcribeFailure is a mapped transcription error: HTTP status plus the
// stable error-envelope code and message.
type transcribeFailure struct {
	status  int
	code    string
	message string
}

// transcribe decodes raw audio, routes it through the STT kernel, applies
// customization and persists the transcript. It is shared by the native
// dictation route and the OpenAI-compatible /v1/audio/transcriptions route so
// both run the identical kernel path; only the wire shapes differ.
func (h *Handler) transcribe(r *http.Request, raw []byte, contentType string, opts stt.TranscribeOpts) (*transcribeResponse, *transcribeFailure) {
	decoded, err := audio.DecodeWithLimits(r.Context(), raw, contentType, h.decodeLimits)
	if err != nil {
		if errors.Is(err, audio.ErrUnsupportedFormat) {
			return nil, &transcribeFailure{http.StatusUnsupportedMediaType, "unsupported_audio_format", err.Error()}
		}
		if errors.Is(err, audio.ErrDecodedAudioTooLarge) {
			return nil, &transcribeFailure{http.StatusRequestEntityTooLarge, "payload_too_large", err.Error()}
		}
		return nil, &transcribeFailure{http.StatusBadRequest, "invalid_audio", "failed to decode audio: " + err.Error()}
	}
	if len(decoded.PCM) == 0 {
		return nil, &transcribeFailure{http.StatusBadRequest, "empty_audio", "decoded audio contains no samples"}
	}

	started := time.Now()
	durationSecs := float64(decoded.DurationMs) / 1000.0
	ctx := r.Context()
	opts, replacements := h.applyCustomizationHints(ctx, opts)
	result, err := h.router.Route(ctx, decoded.PCM, durationSecs, opts)
	latency := time.Since(started)

	if err != nil {
		// Router errors map to 503 (provider unavailable) in v1 since we
		// don't yet distinguish transient vs permanent. Revisit in v1.1.
		slog.Warn("dictation: router error", // #nosec G706 -- slog writes structured fields; request-derived values are attributes, not message format.
			"err", err,
			"duration_ms", decoded.DurationMs,
			"format", decoded.SourceFormat,
		)
		return nil, &transcribeFailure{http.StatusServiceUnavailable, "provider_unavailable", "no STT provider could satisfy the request: " + err.Error()}
	}
	if result == nil {
		return nil, &transcribeFailure{http.StatusInternalServerError, "empty_result", "STT router returned nil result"}
	}
	if strings.TrimSpace(result.Text) == "" {
		return nil, &transcribeFailure{http.StatusUnprocessableEntity, "empty_transcript", "STT returned an empty transcript; the audio may contain no speech"}
	}
	var customizationActions []speechkit.CustomizationAction
	if len(replacements) > 0 {
		applied, err := internalcustomize.Apply(result.Text, replacements, speechcustomize.StagePostSTT)
		if err != nil {
			slog.Debug("dictation: customization replacements skipped", "err", err)
		} else {
			result.Text = applied.Text
			customizationActions = internalcustomize.PublicActions(applied.Actions)
			h.recordCustomizationUsage(ctx, replacements, applied.Matches, firstNonEmpty(result.Language, opts.Language))
		}
	}

	resp := &transcribeResponse{
		Text:       result.Text,
		Language:   firstNonEmpty(result.Language, opts.Language),
		DurationMs: decoded.DurationMs,
		LatencyMs:  latency.Milliseconds(),
		Provider:   result.Provider,
		Model:      result.Model,
		Confidence: result.Confidence,
		SourceInfo: &sourceMeta{
			Format:     decoded.SourceFormat,
			SampleRate: decoded.SourceRate,
			Channels:   decoded.SourceCh,
			DurationMs: decoded.DurationMs,
		},
		Speakers:             result.Speakers,
		CustomizationActions: customizationActions,
	}
	if h.store != nil {
		persistCtx := storageauth.ContextWithRequestOwner(ctx, r)
		audioAsset := audioAssetInput(raw, contentType, decoded.SourceFormat, decoded.DurationMs)
		var err error
		switch saver := h.store.(type) {
		case store.TranscriptionSpeakerStore:
			err = saver.SaveTranscriptionWithAudioAndSpeakers(persistCtx, result.Text, resp.Language, result.Provider, result.Model, decoded.DurationMs, latency.Milliseconds(), audioAsset, result.Speakers)
		case store.TranscriptionAudioStore:
			err = saver.SaveTranscriptionWithAudio(persistCtx, result.Text, resp.Language, result.Provider, result.Model, decoded.DurationMs, latency.Milliseconds(), audioAsset)
		default:
			err = h.store.SaveTranscription(persistCtx, result.Text, resp.Language, result.Provider, result.Model, decoded.DurationMs, latency.Milliseconds(), raw)
		}
		if err != nil {
			slog.Warn("dictation: persist transcript failed", "err", err)
		}
	}
	return resp, nil
}

func (h *Handler) applyCustomizationHints(ctx context.Context, opts stt.TranscribeOpts) (stt.TranscribeOpts, []speechcustomize.Replacement) {
	service := internalcustomize.NewService(internalcustomize.ServiceOptions{
		Store:             h.store,
		ScopeOrder:        speechcustomize.DefaultServerScopeOrder(),
		ActiveTemplateIDs: h.activeTemplates,
	})
	resolved, err := service.Resolve(ctx, speechcustomize.Context{
		Mode:              speechcustomize.ModeDictation,
		Language:          opts.Language,
		Stage:             speechcustomize.StagePostSTT,
		ActiveTemplateIDs: h.activeTemplates,
	})
	if err != nil {
		slog.Debug("dictation: resolve customization failed", "err", err)
		return opts, nil
	}
	if resolved.Prompt != "" && !strings.Contains(opts.Prompt, resolved.Prompt) {
		if strings.TrimSpace(opts.Prompt) == "" || strings.TrimSpace(opts.Prompt) == h.defaultPrompt {
			opts.Prompt = resolved.Prompt
		} else {
			opts.Prompt = strings.TrimSpace(opts.Prompt) + "\n" + resolved.Prompt
		}
	}
	opts.Keyterms = mergeKeyterms(opts.Keyterms, resolved.Keyterms)
	return opts, resolved.Replacements
}

func (h *Handler) recordCustomizationUsage(ctx context.Context, replacements []speechcustomize.Replacement, matches []internalcustomize.MatchRecord, language string) {
	wordStore, _ := h.store.(store.WordStore)
	dictionaryStore, _ := h.store.(store.UserDictionaryStore)
	replacementStore, _ := h.store.(store.ReplacementStore)
	if wordStore == nil && dictionaryStore == nil && replacementStore == nil {
		return
	}
	replacementsByID := map[string]speechcustomize.Replacement{}
	for _, replacement := range replacements {
		replacementsByID[replacement.ID] = replacement
	}
	for _, match := range matches {
		usageCtx := ctx
		replacement := replacementsByID[match.ReplacementID]
		if replacement.Scope != nil {
			if scoped, ok := internalcustomize.StorageScopeForRef(speechstorage.ScopeFromContext(ctx), *replacement.Scope); ok {
				usageCtx = speechstorage.WithScope(ctx, scoped)
			}
		}
		if replacementStore != nil && match.ReplacementID != "" {
			if err := replacementStore.RecordReplacementUsage(usageCtx, match.ReplacementID); err != nil {
				slog.Debug("dictation: record replacement usage failed", "err", err)
			}
		}
		term := match.Term
		if strings.TrimSpace(replacement.Output.Text) != "" {
			term = replacement.Output.Text
		}
		if wordStore != nil {
			if err := wordStore.RecordWordUsage(usageCtx, term, language); err != nil {
				slog.Debug("dictation: record word usage failed", "err", err)
			}
		}
		if dictionaryStore != nil {
			if err := dictionaryStore.RecordUserDictionaryUsage(ctx, term, language); err != nil {
				slog.Debug("dictation: record dictionary usage failed", "err", err)
			}
		}
	}
}

func mergeKeyterms(existing, next []string) []string {
	if len(next) == 0 {
		return existing
	}
	merged := append([]string(nil), existing...)
	seen := map[string]struct{}{}
	for _, value := range merged {
		seen[strings.ToLower(strings.TrimSpace(value))] = struct{}{}
	}
	for _, value := range next {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		merged = append(merged, trimmed)
	}
	return merged
}
