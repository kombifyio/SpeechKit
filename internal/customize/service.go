package customize

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"

	customtemplates "github.com/kombifyio/SpeechKit/internal/customize/templates"
	speechcustomize "github.com/kombifyio/SpeechKit/pkg/speechkit/customize"
	speechstorage "github.com/kombifyio/SpeechKit/pkg/speechkit/storage"
)

type WordStore interface {
	ListWords(ctx context.Context, opts speechcustomize.ListOptions) ([]speechcustomize.Word, error)
}

type ReplacementStore interface {
	ListReplacements(ctx context.Context, opts speechcustomize.ListOptions) ([]speechcustomize.Replacement, error)
}

type ServiceOptions struct {
	Store                   any
	ScopeOrder              []speechcustomize.ScopeRef
	ActiveTemplateIDs       []string
	DisableBuiltinTemplates bool
}

type Service struct {
	store                   any
	scopeOrder              []speechcustomize.ScopeRef
	activeTemplateIDs       []string
	disableBuiltinTemplates bool
	mu                      sync.RWMutex
	cache                   map[string]ResolvedSet
}

type ResolvedSet struct {
	Context        speechcustomize.Context
	Words          []speechcustomize.Word
	Replacements   []speechcustomize.Replacement
	Prompt         string
	Keyterms       []string
	VoiceAgentHint string
	ProviderBias   ProviderBias
	Applier        *CompiledApplier
}

func NewService(opts ServiceOptions) *Service {
	order := normalizeScopeOrder(opts.ScopeOrder)
	if len(order) == 0 {
		order = speechcustomize.DefaultDeviceScopeOrder()
	}
	activeTemplateIDs := customtemplates.NormalizeActiveTemplateIDs(opts.ActiveTemplateIDs)
	if opts.DisableBuiltinTemplates {
		activeTemplateIDs = nil
	}
	return &Service{
		store:                   opts.Store,
		scopeOrder:              order,
		activeTemplateIDs:       activeTemplateIDs,
		disableBuiltinTemplates: opts.DisableBuiltinTemplates,
		cache:                   map[string]ResolvedSet{},
	}
}

func (s *Service) Invalidate() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache = map[string]ResolvedSet{}
}

func (s *Service) Resolve(ctx context.Context, request speechcustomize.Context) (ResolvedSet, error) {
	if s == nil {
		return ResolvedSet{Context: request}, nil
	}
	request = normalizeContext(request)
	if !s.disableBuiltinTemplates && len(request.ActiveTemplateIDs) == 0 {
		request.ActiveTemplateIDs = append([]string(nil), s.activeTemplateIDs...)
	}
	if len(request.ScopeOrder) == 0 {
		request.ScopeOrder = append([]speechcustomize.ScopeRef(nil), s.scopeOrder...)
	}
	key := serviceCacheKey(ctx, request)
	s.mu.RLock()
	if cached, ok := s.cache[key]; ok {
		s.mu.RUnlock()
		return cached, nil
	}
	s.mu.RUnlock()

	resolved, err := s.resolveUncached(ctx, request)
	if err != nil {
		return ResolvedSet{Context: request}, err
	}
	s.mu.Lock()
	s.cache[key] = resolved
	s.mu.Unlock()
	return resolved, nil
}

func (s *Service) Apply(ctx context.Context, request speechcustomize.Context, text string) (ApplyResult, error) {
	resolved, err := s.Resolve(ctx, request)
	if err != nil {
		return ApplyResult{Text: text}, err
	}
	if resolved.Applier == nil {
		return ApplyResult{Text: text}, nil
	}
	return resolved.Applier.Apply(text), nil
}

func (s *Service) resolveUncached(ctx context.Context, request speechcustomize.Context) (ResolvedSet, error) {
	baseScope := speechstorage.ScopeFromContext(ctx)
	wordStore, hasWords := s.store.(WordStore)
	replacementStore, hasReplacements := s.store.(ReplacementStore)
	wordsByKey := map[string]speechcustomize.Word{}
	replacementsByKey := map[string]speechcustomize.Replacement{}

	addActiveTemplatePacks(request, wordsByKey, replacementsByKey)

	for _, ref := range request.ScopeOrder {
		ref = speechcustomize.NormalizeScopeRef(ref)
		if ref.Kind == "" {
			continue
		}
		storageScope, ok := StorageScopeForRef(baseScope, ref)
		if !ok {
			continue
		}
		scopedCtx := speechstorage.WithScope(ctx, storageScope)
		if hasWords {
			words, err := wordStore.ListWords(scopedCtx, speechcustomize.ListOptions{
				Language: request.Language,
				Mode:     request.Mode,
				Tags:     request.Tags,
			})
			if err != nil {
				return ResolvedSet{}, err
			}
			for _, word := range words {
				word = speechcustomize.WithDefaultsWord(word)
				if !word.Enabled || !tagsMatch(request.Tags, word.Tags) {
					continue
				}
				word.Scope = scopeRefPtr(ref)
				wordsByKey[wordResolveKey(word)] = word
			}
		}
		if hasReplacements {
			replacements, err := replacementStore.ListReplacements(scopedCtx, speechcustomize.ListOptions{
				Language: request.Language,
				Mode:     request.Mode,
				Stage:    request.Stage,
				Tags:     request.Tags,
			})
			if err != nil {
				return ResolvedSet{}, err
			}
			for _, replacement := range replacements {
				replacement = speechcustomize.WithDefaultsReplacement(replacement)
				if !replacement.Enabled || !tagsMatch(request.Tags, replacement.Tags) {
					continue
				}
				replacement.Scope = scopeRefPtr(ref)
				replacementsByKey[replacementResolveKey(replacement)] = replacement
			}
		}
	}

	words := values(wordsByKey)
	sort.SliceStable(words, func(i, j int) bool {
		if strings.EqualFold(words[i].Term, words[j].Term) {
			return words[i].ID < words[j].ID
		}
		return strings.ToLower(words[i].Term) < strings.ToLower(words[j].Term)
	})
	seedSynonymsFromWords(words, replacementsByKey)
	replacements := values(replacementsByKey)
	sort.SliceStable(replacements, func(i, j int) bool {
		if replacements[i].Priority != replacements[j].Priority {
			return replacements[i].Priority > replacements[j].Priority
		}
		return replacements[i].ID < replacements[j].ID
	})
	applier, err := CompileApplier(replacements, request)
	if err != nil {
		return ResolvedSet{}, err
	}
	providerBias := BuildProviderBias(words)
	return ResolvedSet{
		Context:        request,
		Words:          words,
		Replacements:   replacements,
		Prompt:         BuildPrompt(words),
		Keyterms:       BuildKeyterms(words),
		VoiceAgentHint: BuildVoiceAgentHint(words),
		ProviderBias:   providerBias,
		Applier:        applier,
	}, nil
}

func StorageScopeForRef(base speechstorage.Scope, ref speechcustomize.ScopeRef) (speechstorage.Scope, bool) {
	base = speechstorage.NormalizeScope(base)
	ref = speechcustomize.NormalizeScopeRef(ref)
	switch ref.Kind {
	case speechcustomize.ScopeBuiltin, speechcustomize.ScopeApp:
		return speechstorage.Scope{
			InstallID: firstNonEmpty(base.InstallID, speechstorage.LocalInstallID),
			Labels: map[string]string{
				"customization_scope": string(ref.Kind),
				"customization_key":   firstNonEmpty(ref.Key, "default"),
			},
		}, true
	case speechcustomize.ScopeInstall:
		return speechstorage.Scope{InstallID: firstNonEmpty(ref.Key, base.InstallID, speechstorage.LocalInstallID)}, true
	case speechcustomize.ScopeOrg:
		key := firstNonEmpty(ref.Key, base.TenantID)
		if key == "" {
			return speechstorage.Scope{}, false
		}
		return speechstorage.Scope{InstallID: firstNonEmpty(base.InstallID, speechstorage.LocalInstallID), TenantID: key}, true
	case speechcustomize.ScopeWorkspace:
		key := firstNonEmpty(ref.Key, base.Labels["workspace"])
		if key == "" {
			return speechstorage.Scope{}, false
		}
		return speechstorage.Scope{InstallID: firstNonEmpty(base.InstallID, speechstorage.LocalInstallID), Labels: map[string]string{"workspace": key}}, true
	case speechcustomize.ScopeUser:
		key := firstNonEmpty(ref.Key, base.UserID)
		if key == "" {
			return base, true
		}
		return speechstorage.Scope{InstallID: firstNonEmpty(base.InstallID, speechstorage.LocalInstallID), TenantID: base.TenantID, UserID: key}, true
	case speechcustomize.ScopeSession:
		key := firstNonEmpty(ref.Key, base.Labels["session"])
		if key == "" {
			return speechstorage.Scope{}, false
		}
		return speechstorage.Scope{InstallID: firstNonEmpty(base.InstallID, speechstorage.LocalInstallID), TenantID: base.TenantID, UserID: base.UserID, Labels: map[string]string{"session": key}}, true
	default:
		return speechstorage.Scope{}, false
	}
}

func normalizeContext(context speechcustomize.Context) speechcustomize.Context {
	context.Mode = speechcustomize.NormalizeMode(context.Mode)
	context.Language = speechcustomize.NormalizeLanguage(context.Language)
	context.Stage = speechcustomize.NormalizeStage(context.Stage)
	context.Persona = strings.TrimSpace(context.Persona)
	context.ActiveTemplateIDs = customtemplates.NormalizeTemplateIDs(context.ActiveTemplateIDs)
	context.ScopeOrder = normalizeScopeOrder(context.ScopeOrder)
	return context
}

func addActiveTemplatePacks(request speechcustomize.Context, wordsByKey map[string]speechcustomize.Word, replacementsByKey map[string]speechcustomize.Replacement) {
	if len(request.ActiveTemplateIDs) == 0 {
		return
	}
	for _, pack := range customtemplates.ActiveBuiltinPacks(request.ActiveTemplateIDs) {
		for _, word := range pack.Words {
			word = speechcustomize.WithDefaultsWord(word)
			if !templateWordMatchesContext(word, request) {
				continue
			}
			word.Scope = scopeRefPtr(speechcustomize.ScopeRef{Kind: speechcustomize.ScopeBuiltin})
			wordsByKey[wordResolveKey(word)] = word
		}
		for _, replacement := range pack.Replacements {
			replacement = speechcustomize.WithDefaultsReplacement(replacement)
			if !templateReplacementMatchesContext(replacement, request) {
				continue
			}
			replacement.Scope = scopeRefPtr(speechcustomize.ScopeRef{Kind: speechcustomize.ScopeBuiltin})
			replacementsByKey[replacementResolveKey(replacement)] = replacement
		}
	}
}

func templateWordMatchesContext(word speechcustomize.Word, request speechcustomize.Context) bool {
	if !word.Enabled || strings.TrimSpace(word.Term) == "" {
		return false
	}
	return languageMatches(word.Language, request.Language)
}

func templateReplacementMatchesContext(replacement speechcustomize.Replacement, request speechcustomize.Context) bool {
	if !replacement.Enabled || strings.TrimSpace(replacement.Match.Pattern) == "" || !replacementHasRunnableOutput(replacement) {
		return false
	}
	if !languageMatches(replacement.Language, request.Language) {
		return false
	}
	if request.Stage != "" && replacement.Stage != request.Stage {
		return false
	}
	if request.Mode != "" && len(replacement.Modes) > 0 && !replacementHasMode(replacement, request.Mode) {
		return false
	}
	return true
}

func languageMatches(itemLanguage, requestLanguage string) bool {
	itemLanguage = speechcustomize.NormalizeLanguage(itemLanguage)
	requestLanguage = speechcustomize.NormalizeLanguage(requestLanguage)
	return requestLanguage == "" || itemLanguage == "" || itemLanguage == requestLanguage
}

func normalizeScopeOrder(order []speechcustomize.ScopeRef) []speechcustomize.ScopeRef {
	out := make([]speechcustomize.ScopeRef, 0, len(order))
	for _, ref := range order {
		ref = speechcustomize.NormalizeScopeRef(ref)
		if ref.Kind != "" {
			out = append(out, ref)
		}
	}
	return out
}

func serviceCacheKey(ctx context.Context, request speechcustomize.Context) string {
	payload := struct {
		Scope   string                  `json:"scope"`
		Request speechcustomize.Context `json:"request"`
	}{
		Scope:   speechstorage.ScopeFromContext(ctx).Key(),
		Request: request,
	}
	raw, _ := json.Marshal(payload)
	return string(raw)
}

func seedSynonymsFromWords(words []speechcustomize.Word, replacementsByKey map[string]speechcustomize.Replacement) {
	for _, word := range words {
		if !word.Enabled {
			continue
		}
		for _, alias := range speechcustomize.NormalizeAliasList(word.Term, word.SoundsLike) {
			derived := speechcustomize.SynonymReplacement(word.Term, alias, word.Language, word.Source)
			derived.Enabled = true
			derived.Scope = word.Scope
			key := replacementResolveKey(derived)
			if _, exists := replacementsByKey[key]; exists {
				continue
			}
			replacementsByKey[key] = derived
		}
	}
}

func wordResolveKey(word speechcustomize.Word) string {
	return strings.ToLower(word.Language + "\x00" + strings.TrimSpace(word.Term))
}

func replacementResolveKey(replacement speechcustomize.Replacement) string {
	modes := make([]string, 0, len(replacement.Modes))
	for _, mode := range replacement.Modes {
		if normalized := speechcustomize.NormalizeMode(mode); normalized != "" {
			modes = append(modes, string(normalized))
		}
	}
	sort.Strings(modes)
	return strings.ToLower(strings.Join([]string{
		string(replacement.Kind),
		replacement.Language,
		string(replacement.Stage),
		string(replacement.Match.Type),
		strings.TrimSpace(replacement.Match.Pattern),
		strings.Join(modes, ","),
	}, "\x00"))
}

func scopeRefPtr(ref speechcustomize.ScopeRef) *speechcustomize.ScopeRef {
	ref = speechcustomize.NormalizeScopeRef(ref)
	return &ref
}

func values[T any](m map[string]T) []T {
	out := make([]T, 0, len(m))
	for _, value := range m {
		out = append(out, value)
	}
	return out
}
