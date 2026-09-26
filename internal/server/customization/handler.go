//go:build linux

package customization

import (
	"net/http"

	customtemplates "github.com/kombifyio/SpeechKit/internal/customize/templates"
	"github.com/kombifyio/SpeechKit/internal/store"
	speechcustomize "github.com/kombifyio/SpeechKit/pkg/speechkit/customize"
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
	mux.HandleFunc("/v1/customization/vocabulary", h.vocabulary)
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

type vocabularyRequest struct {
	Language     string                        `json:"language"`
	Source       string                        `json:"source"`
	Scope        speechcustomize.ScopeRef      `json:"scope"`
	Words        []speechcustomize.Word        `json:"words"`
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
