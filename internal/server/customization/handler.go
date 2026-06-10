//go:build linux

package customization

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	internalcustomize "github.com/kombifyio/SpeechKit/internal/customize"
	customtemplates "github.com/kombifyio/SpeechKit/internal/customize/templates"
	"github.com/kombifyio/SpeechKit/internal/server/httpx"
	"github.com/kombifyio/SpeechKit/internal/server/middleware"
	"github.com/kombifyio/SpeechKit/internal/store"
	speechcustomize "github.com/kombifyio/SpeechKit/pkg/speechkit/customize"
	speechstorage "github.com/kombifyio/SpeechKit/pkg/speechkit/storage"
)

type Handler struct {
	store           store.CustomizationStore
	activeTemplates []string
}

func New(customizationStore store.CustomizationStore, activeTemplateIDs ...[]string) *Handler {
	active := customtemplates.DefaultActiveTemplateIDs()
	if len(activeTemplateIDs) > 0 {
		active = customtemplates.NormalizeActiveTemplateIDs(activeTemplateIDs[0])
	}
	return &Handler{store: customizationStore, activeTemplates: active}
}

func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("/v1/words", h.words)
	mux.HandleFunc("/v1/replacements", h.replacements)
	mux.HandleFunc("/v1/lexicons", h.lexicons)
	mux.HandleFunc("/v1/rulesets", h.rulesets)
	mux.HandleFunc("/v1/customization/pack", h.pack)
	mux.HandleFunc("/v1/customization/templates", h.templates)
	mux.HandleFunc("/v1/customization/templates/", h.templatePack)
}

type wordsRequest struct {
	Language string                   `json:"language"`
	Source   string                   `json:"source"`
	Scope    speechcustomize.ScopeRef `json:"scope"`
	Words    []speechcustomize.Word   `json:"words"`
}

type replacementsRequest struct {
	Language     string                        `json:"language"`
	Source       string                        `json:"source"`
	Scope        speechcustomize.ScopeRef      `json:"scope"`
	Replacements []speechcustomize.Replacement `json:"replacements"`
}

type lexiconsRequest struct {
	Language string                    `json:"language"`
	Source   string                    `json:"source"`
	Scope    speechcustomize.ScopeRef  `json:"scope"`
	Lexicons []speechcustomize.Lexicon `json:"lexicons"`
}

type rulesetsRequest struct {
	Language string                    `json:"language"`
	Source   string                    `json:"source"`
	Scope    speechcustomize.ScopeRef  `json:"scope"`
	Rulesets []speechcustomize.Ruleset `json:"rulesets"`
}

type templatesResponse struct {
	Templates         []customtemplates.Template `json:"templates"`
	ActiveTemplateIDs []string                   `json:"activeTemplateIds"`
}

func (h *Handler) words(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "store_unavailable", "customization storage is not configured")
		return
	}
	switch r.Method {
	case http.MethodGet:
		ctx := customizationScopeContext(r.Context(), scopeRef(r, speechcustomize.ScopeRef{}))
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
		ctx := customizationScopeContext(r.Context(), scopeRef(r, body.Scope))
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

func (h *Handler) replacements(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "store_unavailable", "customization storage is not configured")
		return
	}
	switch r.Method {
	case http.MethodGet:
		ctx := customizationScopeContext(r.Context(), scopeRef(r, speechcustomize.ScopeRef{}))
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
		ctx := customizationScopeContext(r.Context(), scopeRef(r, body.Scope))
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
		ctx := customizationScopeContext(r.Context(), scopeRef(r, speechcustomize.ScopeRef{}))
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
		ctx := customizationScopeContext(r.Context(), scopeRef(r, body.Scope))
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
		ctx := customizationScopeContext(r.Context(), scopeRef(r, speechcustomize.ScopeRef{}))
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
		ctx := customizationScopeContext(r.Context(), scopeRef(r, body.Scope))
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
		ctx := customizationScopeContext(r.Context(), scopeRef(r, speechcustomize.ScopeRef{}))
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
		ctx := customizationScopeContext(r.Context(), scopeRef(r, speechcustomize.ScopeRef{}))
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

func importPack(ctx context.Context, s store.CustomizationStore, pack speechcustomize.Pack, source string) error {
	for language, words := range groupWords(pack.Words) {
		if err := replaceWords(ctx, s, store.CustomizationReplaceOpts{Language: language, Source: source}, withWordSource(words, source)); err != nil {
			return err
		}
	}
	for language, replacements := range groupReplacements(pack.Replacements) {
		if err := replaceReplacements(ctx, s, store.CustomizationReplaceOpts{Language: language, Source: source}, withReplacementSource(replacements, source)); err != nil {
			return err
		}
	}
	for language, lexicons := range groupLexicons(pack.Lexicons) {
		if err := replaceLexicons(ctx, s, store.CustomizationReplaceOpts{Language: language, Source: source}, withLexiconSource(lexicons, source)); err != nil {
			return err
		}
	}
	for language, rulesets := range groupRulesets(pack.Rulesets) {
		if err := replaceRulesets(ctx, s, store.CustomizationReplaceOpts{Language: language, Source: source}, withRulesetSource(rulesets, source)); err != nil {
			return err
		}
	}
	return nil
}

func listOpts(r *http.Request) store.CustomizationListOpts {
	return store.CustomizationListOpts{
		Language:        r.URL.Query().Get("language"),
		Mode:            speechcustomize.Mode(r.URL.Query().Get("mode")),
		Stage:           speechcustomize.Stage(r.URL.Query().Get("stage")),
		IncludeDisabled: strings.EqualFold(r.URL.Query().Get("include_disabled"), "true") || r.URL.Query().Get("include_disabled") == "1",
		Source:          r.URL.Query().Get("source"),
	}
}

func scopeRef(r *http.Request, fallback speechcustomize.ScopeRef) speechcustomize.ScopeRef {
	if scope := strings.TrimSpace(r.URL.Query().Get("scope")); scope != "" {
		return speechcustomize.ScopeRef{Kind: speechcustomize.ScopeKind(scope), Key: r.URL.Query().Get("scope_key")}
	}
	return fallback
}

func customizationScopeContext(ctx context.Context, ref speechcustomize.ScopeRef) context.Context {
	ref = speechcustomize.NormalizeScopeRef(ref)
	if ref.Kind == "" {
		return ctx
	}
	if scoped, ok := internalcustomize.StorageScopeForRef(speechstorage.ScopeFromContext(ctx), ref); ok {
		return speechstorage.WithScope(ctx, scoped)
	}
	return ctx
}

func replaceWords(ctx context.Context, s store.CustomizationStore, opts store.CustomizationReplaceOpts, words []speechcustomize.Word) error {
	if sourceStore, ok := s.(store.CustomizationSourceStore); ok {
		return sourceStore.ReplaceWordsWithOptions(ctx, opts, words)
	}
	return s.ReplaceWords(ctx, opts.Language, words)
}

func replaceReplacements(ctx context.Context, s store.CustomizationStore, opts store.CustomizationReplaceOpts, replacements []speechcustomize.Replacement) error {
	if sourceStore, ok := s.(store.CustomizationSourceStore); ok {
		return sourceStore.ReplaceReplacementsWithOptions(ctx, opts, replacements)
	}
	return s.ReplaceReplacements(ctx, opts.Language, replacements)
}

func replaceLexicons(ctx context.Context, s store.CustomizationStore, opts store.CustomizationReplaceOpts, lexicons []speechcustomize.Lexicon) error {
	if sourceStore, ok := s.(store.CustomizationSourceStore); ok {
		return sourceStore.ReplaceLexiconsWithOptions(ctx, opts, lexicons)
	}
	return s.ReplaceLexicons(ctx, opts.Language, lexicons)
}

func replaceRulesets(ctx context.Context, s store.CustomizationStore, opts store.CustomizationReplaceOpts, rulesets []speechcustomize.Ruleset) error {
	if sourceStore, ok := s.(store.CustomizationSourceStore); ok {
		return sourceStore.ReplaceRulesetsWithOptions(ctx, opts, rulesets)
	}
	return s.ReplaceRulesets(ctx, opts.Language, rulesets)
}

func packSource(pack speechcustomize.Pack) string {
	key := firstNonEmpty(pack.ID, pack.Name, "customization-pack")
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(key)), "pack:") {
		return key
	}
	return "pack:" + strings.TrimPrefix(speechcustomize.StableID("pack", key), "pack_")
}

func requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	if middleware.IdentityFromContext(r.Context()).Role == "admin" {
		return true
	}
	httpx.WriteError(w, http.StatusForbidden, "admin_required", "customization writes require an admin identity")
	return false
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer func() { _ = r.Body.Close() }()
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20))
	if err := dec.Decode(dst); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return false
	}
	return true
}

func writeList(w http.ResponseWriter, key string, value any, err error) {
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, key+"_read_failed", err.Error())
		return
	}
	writeJSON(w, map[string]any{key: value})
}

func writeJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(body)
}

func methodNotAllowed(w http.ResponseWriter) {
	w.Header().Set("Allow", http.MethodGet+", "+http.MethodPost)
	httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed on this resource")
}

func groupWords(words []speechcustomize.Word) map[string][]speechcustomize.Word {
	out := map[string][]speechcustomize.Word{}
	for _, word := range words {
		language := speechcustomize.NormalizeLanguage(word.Language)
		out[language] = append(out[language], word)
	}
	return out
}

func groupReplacements(replacements []speechcustomize.Replacement) map[string][]speechcustomize.Replacement {
	out := map[string][]speechcustomize.Replacement{}
	for _, replacement := range replacements {
		language := speechcustomize.NormalizeLanguage(replacement.Language)
		out[language] = append(out[language], replacement)
	}
	return out
}

func groupLexicons(lexicons []speechcustomize.Lexicon) map[string][]speechcustomize.Lexicon {
	out := map[string][]speechcustomize.Lexicon{}
	for _, lexicon := range lexicons {
		language := speechcustomize.NormalizeLanguage(lexicon.Language)
		out[language] = append(out[language], lexicon)
	}
	return out
}

func groupRulesets(rulesets []speechcustomize.Ruleset) map[string][]speechcustomize.Ruleset {
	out := map[string][]speechcustomize.Ruleset{}
	for _, ruleset := range rulesets {
		language := speechcustomize.NormalizeLanguage(ruleset.Language)
		out[language] = append(out[language], ruleset)
	}
	return out
}

func withWordSource(words []speechcustomize.Word, source string) []speechcustomize.Word {
	out := append([]speechcustomize.Word(nil), words...)
	for i := range out {
		out[i].Source = source
	}
	return out
}

func withReplacementSource(replacements []speechcustomize.Replacement, source string) []speechcustomize.Replacement {
	out := append([]speechcustomize.Replacement(nil), replacements...)
	for i := range out {
		out[i].Source = source
	}
	return out
}

func withLexiconSource(lexicons []speechcustomize.Lexicon, source string) []speechcustomize.Lexicon {
	out := append([]speechcustomize.Lexicon(nil), lexicons...)
	for i := range out {
		out[i].Source = source
	}
	return out
}

func withRulesetSource(rulesets []speechcustomize.Ruleset, source string) []speechcustomize.Ruleset {
	out := append([]speechcustomize.Ruleset(nil), rulesets...)
	for i := range out {
		out[i].Source = source
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
