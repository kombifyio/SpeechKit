//go:build linux

package customization

import (
	"net/http"
	"strings"
	"time"

	customtemplates "github.com/kombifyio/SpeechKit/internal/customize/templates"
	"github.com/kombifyio/SpeechKit/internal/server/httpx"
	"github.com/kombifyio/SpeechKit/internal/store"
	speechcustomize "github.com/kombifyio/SpeechKit/pkg/speechkit/customize"
)

func (h *Handler) words(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "store_unavailable", "customization storage is not configured")
		return
	}
	switch r.Method {
	case http.MethodGet:
		ref, ok := requestScopeRef(w, r, speechcustomize.ScopeRef{})
		if !ok {
			return
		}
		ctx := customizationScopeContext(r.Context(), ref)
		words, err := h.store.ListWords(ctx, listOpts(r))
		writeList(w, "words", words, err)
	case http.MethodPost:
		if !requireAdmin(w, r) {
			return
		}
		var body wordsRequest
		if !decodeJSON(w, r, &body) {
			return
		}
		if body.Language == "" {
			body.Language = r.URL.Query().Get("language")
		}
		ref, ok := requestScopeRef(w, r, body.Scope)
		if !ok {
			return
		}
		ctx := customizationScopeContext(r.Context(), ref)
		if err := replaceWords(ctx, h.store, store.CustomizationReplaceOpts{Language: body.Language, Source: body.Source}, body.Words); err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "words_write_failed", err.Error())
			return
		}
		words, _ := h.store.ListWords(ctx, store.CustomizationListOpts{Language: body.Language})
		writeJSON(w, map[string]any{"words": words})
	default:
		methodNotAllowed(w)
	}
}

func (h *Handler) vocabulary(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "store_unavailable", "customization storage is not configured")
		return
	}
	switch r.Method {
	case http.MethodGet:
		ref, ok := requestScopeRef(w, r, speechcustomize.ScopeRef{})
		if !ok {
			return
		}
		ctx := customizationScopeContext(r.Context(), ref)
		opts := listOpts(r)
		words, err := h.store.ListWords(ctx, opts)
		if err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "words_list_failed", err.Error())
			return
		}
		replacements, err := h.store.ListReplacements(ctx, opts)
		if err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "replacements_list_failed", err.Error())
			return
		}
		folded, extras := speechcustomize.FoldVocabulary(words, replacements)
		writeJSON(w, map[string]any{"words": folded, "replacements": extras})
	case http.MethodPost:
		if !requireAdmin(w, r) {
			return
		}
		var body vocabularyRequest
		if !decodeJSON(w, r, &body) {
			return
		}
		if body.Language == "" {
			body.Language = r.URL.Query().Get("language")
		}
		ref, ok := requestScopeRef(w, r, body.Scope)
		if !ok {
			return
		}
		ctx := customizationScopeContext(r.Context(), ref)
		if err := replaceVocabulary(ctx, h.store, store.CustomizationReplaceOpts{Language: body.Language, Source: body.Source}, body.Words, body.Replacements); err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "vocabulary_write_failed", err.Error())
			return
		}
		opts := store.CustomizationListOpts{Language: body.Language, Source: body.Source, IncludeDisabled: true}
		words, _ := h.store.ListWords(ctx, opts)
		replacements, _ := h.store.ListReplacements(ctx, opts)
		folded, extras := speechcustomize.FoldVocabulary(words, replacements)
		writeJSON(w, map[string]any{"words": folded, "replacements": extras})
	default:
		methodNotAllowed(w)
	}
}

func (h *Handler) replacements(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "store_unavailable", "customization storage is not configured")
		return
	}
	switch r.Method {
	case http.MethodGet:
		ref, ok := requestScopeRef(w, r, speechcustomize.ScopeRef{})
		if !ok {
			return
		}
		ctx := customizationScopeContext(r.Context(), ref)
		replacements, err := h.store.ListReplacements(ctx, listOpts(r))
		writeList(w, "replacements", replacements, err)
	case http.MethodPost:
		if !requireAdmin(w, r) {
			return
		}
		var body replacementsRequest
		if !decodeJSON(w, r, &body) {
			return
		}
		if body.Language == "" {
			body.Language = r.URL.Query().Get("language")
		}
		ref, ok := requestScopeRef(w, r, body.Scope)
		if !ok {
			return
		}
		ctx := customizationScopeContext(r.Context(), ref)
		if err := replaceReplacements(ctx, h.store, store.CustomizationReplaceOpts{Language: body.Language, Source: body.Source}, body.Replacements); err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "replacements_write_failed", err.Error())
			return
		}
		replacements, _ := h.store.ListReplacements(ctx, store.CustomizationListOpts{Language: body.Language})
		writeJSON(w, map[string]any{"replacements": replacements})
	default:
		methodNotAllowed(w)
	}
}

func (h *Handler) lexicons(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "store_unavailable", "customization storage is not configured")
		return
	}
	switch r.Method {
	case http.MethodGet:
		ref, ok := requestScopeRef(w, r, speechcustomize.ScopeRef{})
		if !ok {
			return
		}
		ctx := customizationScopeContext(r.Context(), ref)
		lexicons, err := h.store.ListLexicons(ctx, listOpts(r))
		writeList(w, "lexicons", lexicons, err)
	case http.MethodPost:
		if !requireAdmin(w, r) {
			return
		}
		var body lexiconsRequest
		if !decodeJSON(w, r, &body) {
			return
		}
		ref, ok := requestScopeRef(w, r, body.Scope)
		if !ok {
			return
		}
		ctx := customizationScopeContext(r.Context(), ref)
		if err := replaceLexicons(ctx, h.store, store.CustomizationReplaceOpts{Language: firstNonEmpty(body.Language, r.URL.Query().Get("language")), Source: body.Source}, body.Lexicons); err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "lexicons_write_failed", err.Error())
			return
		}
		writeJSON(w, map[string]any{"lexicons": body.Lexicons})
	default:
		methodNotAllowed(w)
	}
}

func (h *Handler) rulesets(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "store_unavailable", "customization storage is not configured")
		return
	}
	switch r.Method {
	case http.MethodGet:
		ref, ok := requestScopeRef(w, r, speechcustomize.ScopeRef{})
		if !ok {
			return
		}
		ctx := customizationScopeContext(r.Context(), ref)
		rulesets, err := h.store.ListRulesets(ctx, listOpts(r))
		writeList(w, "rulesets", rulesets, err)
	case http.MethodPost:
		if !requireAdmin(w, r) {
			return
		}
		var body rulesetsRequest
		if !decodeJSON(w, r, &body) {
			return
		}
		ref, ok := requestScopeRef(w, r, body.Scope)
		if !ok {
			return
		}
		ctx := customizationScopeContext(r.Context(), ref)
		if err := replaceRulesets(ctx, h.store, store.CustomizationReplaceOpts{Language: firstNonEmpty(body.Language, r.URL.Query().Get("language")), Source: body.Source}, body.Rulesets); err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "rulesets_write_failed", err.Error())
			return
		}
		writeJSON(w, map[string]any{"rulesets": body.Rulesets})
	default:
		methodNotAllowed(w)
	}
}

func (h *Handler) pack(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "store_unavailable", "customization storage is not configured")
		return
	}
	switch r.Method {
	case http.MethodGet:
		opts := listOpts(r)
		ref, ok := requestScopeRef(w, r, speechcustomize.ScopeRef{})
		if !ok {
			return
		}
		ctx := customizationScopeContext(r.Context(), ref)
		words, err := h.store.ListWords(ctx, opts)
		if err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "pack_read_failed", err.Error())
			return
		}
		replacements, err := h.store.ListReplacements(ctx, opts)
		if err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "pack_read_failed", err.Error())
			return
		}
		lexicons, _ := h.store.ListLexicons(ctx, opts)
		rulesets, _ := h.store.ListRulesets(ctx, opts)
		writeJSON(w, speechcustomize.Pack{
			SchemaVersion: speechcustomize.PackSchemaVersion,
			Words:         words,
			Replacements:  replacements,
			Lexicons:      lexicons,
			Rulesets:      rulesets,
			CreatedAt:     time.Now().UTC(),
		})
	case http.MethodPost:
		if !requireAdmin(w, r) {
			return
		}
		var body speechcustomize.Pack
		if !decodeJSON(w, r, &body) {
			return
		}
		if err := speechcustomize.ValidatePack(body); err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "pack_invalid", err.Error())
			return
		}
		ref, ok := requestScopeRef(w, r, speechcustomize.ScopeRef{})
		if !ok {
			return
		}
		ctx := customizationScopeContext(r.Context(), ref)
		if err := importPack(ctx, h.store, body, firstNonEmpty(r.URL.Query().Get("source"), packSource(body))); err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "pack_import_failed", err.Error())
			return
		}
		writeJSON(w, map[string]any{"status": "imported"})
	default:
		methodNotAllowed(w)
	}
}

func (h *Handler) templates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, templatesResponse{
		Templates:         customtemplates.ListTemplates(),
		ActiveTemplateIDs: append([]string(nil), h.activeTemplates...),
	})
}

func (h *Handler) templatePack(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/customization/templates/"), "/")
	id, suffix, ok := strings.Cut(rest, "/")
	if !ok || suffix != "pack" || strings.TrimSpace(id) == "" {
		http.NotFound(w, r)
		return
	}
	pack, _, found := customtemplates.ResolveTemplatePack(id, r.URL.Query().Get("version"))
	if !found {
		httpx.WriteError(w, http.StatusNotFound, "template_not_found", "customization template not found")
		return
	}
	writeJSON(w, pack)
}
