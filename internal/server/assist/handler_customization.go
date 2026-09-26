//go:build linux

package assist

import (
	"context"
	"log/slog"
	"strings"

	internalcustomize "github.com/kombifyio/SpeechKit/internal/customize"
	"github.com/kombifyio/SpeechKit/internal/store"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	speechcustomize "github.com/kombifyio/SpeechKit/pkg/speechkit/customize"
	speechstorage "github.com/kombifyio/SpeechKit/pkg/speechkit/storage"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
)

func (h *Handler) resolveAssistCustomization(ctx context.Context, locale string) (internalcustomize.ResolvedSet, error) {
	service := internalcustomize.NewService(internalcustomize.ServiceOptions{
		Store:             h.store,
		ScopeOrder:        speechcustomize.DefaultServerScopeOrder(),
		ActiveTemplateIDs: h.activeTemplates,
	})
	return service.Resolve(ctx, speechcustomize.Context{
		Mode:              speechcustomize.ModeAssist,
		Language:          locale,
		Stage:             speechcustomize.StagePostSTT,
		ActiveTemplateIDs: h.activeTemplates,
	})
}

func (h *Handler) applyCustomizationHints(ctx context.Context, opts stt.TranscribeOpts) (stt.TranscribeOpts, []speechcustomize.Replacement) {
	resolved, err := h.resolveAssistCustomization(ctx, opts.Language)
	if err != nil {
		slog.Debug("assist: resolve customization failed", "err", err)
		return opts, nil
	}
	if resolved.Prompt != "" && !strings.Contains(opts.Prompt, resolved.Prompt) {
		if strings.TrimSpace(opts.Prompt) == "" {
			opts.Prompt = resolved.Prompt
		} else {
			opts.Prompt = strings.TrimSpace(opts.Prompt) + "\n" + resolved.Prompt
		}
	}
	opts.Keyterms = mergeKeyterms(opts.Keyterms, resolved.Keyterms)
	return opts, resolved.Replacements
}

// applyResolvedTranscriptCustomization applies the post-STT replacement rules
// from an already-resolved customization set, recording usage for any matches.
// Callers resolve once (see processTranscript) so the transcript replacement
// and the LLM vocabulary hint always come from the same snapshot.
func (h *Handler) applyResolvedTranscriptCustomization(ctx context.Context, resolved internalcustomize.ResolvedSet, transcript, locale string) (string, []speechkit.CustomizationAction) {
	if resolved.Applier == nil {
		return transcript, nil
	}
	applied := resolved.Applier.Apply(transcript)
	if len(applied.Matches) > 0 {
		h.recordCustomizationUsage(ctx, resolved.Replacements, applied.Matches, locale)
	}
	return applied.Text, internalcustomize.PublicActions(applied.Actions)
}

func (h *Handler) recordCustomizationUsage(ctx context.Context, replacements []speechcustomize.Replacement, matches []internalcustomize.MatchRecord, language string) {
	if h.store == nil || len(matches) == 0 {
		return
	}
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
	baseScope := speechstorage.ScopeFromContext(ctx)
	for _, match := range matches {
		usageCtx := ctx
		replacement := replacementsByID[match.ReplacementID]
		if replacement.Scope != nil {
			if scoped, ok := internalcustomize.StorageScopeForRef(baseScope, *replacement.Scope); ok {
				usageCtx = speechstorage.WithScope(ctx, scoped)
			}
		}
		if replacementStore != nil && match.ReplacementID != "" {
			if err := replacementStore.RecordReplacementUsage(usageCtx, match.ReplacementID); err != nil {
				slog.Debug("assist: record replacement usage failed", "err", err)
			}
		}
		term := match.Term
		if strings.TrimSpace(replacement.Output.Text) != "" {
			term = replacement.Output.Text
		}
		if wordStore != nil {
			if err := wordStore.RecordWordUsage(usageCtx, term, language); err != nil {
				slog.Debug("assist: record word usage failed", "err", err)
			}
		}
		if dictionaryStore != nil {
			if err := dictionaryStore.RecordUserDictionaryUsage(ctx, term, language); err != nil {
				slog.Debug("assist: record dictionary usage failed", "err", err)
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
		key := strings.ToLower(strings.TrimSpace(value))
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		merged = append(merged, strings.TrimSpace(value))
	}
	return merged
}
