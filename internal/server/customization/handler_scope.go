//go:build linux

package customization

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	internalcustomize "github.com/kombifyio/SpeechKit/internal/customize"
	"github.com/kombifyio/SpeechKit/internal/server/httpx"
	"github.com/kombifyio/SpeechKit/internal/store"
	speechcustomize "github.com/kombifyio/SpeechKit/pkg/speechkit/customize"
	speechstorage "github.com/kombifyio/SpeechKit/pkg/speechkit/storage"
)

func listOpts(r *http.Request) store.CustomizationListOpts {
	return store.CustomizationListOpts{
		Language:        r.URL.Query().Get("language"),
		Mode:            speechcustomize.Mode(r.URL.Query().Get("mode")),
		Stage:           speechcustomize.Stage(r.URL.Query().Get("stage")),
		IncludeDisabled: strings.EqualFold(r.URL.Query().Get("include_disabled"), "true") || r.URL.Query().Get("include_disabled") == "1",
		Source:          r.URL.Query().Get("source"),
	}
}

func requestScopeRef(w http.ResponseWriter, r *http.Request, fallback speechcustomize.ScopeRef) (speechcustomize.ScopeRef, bool) {
	ref, explicit, problem := scopeRef(r, fallback)
	if problem != "" {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_scope", problem)
		return speechcustomize.ScopeRef{}, false
	}
	ref = speechcustomize.NormalizeScopeRef(ref)
	if explicit && scopeRequiresKey(ref.Kind) && strings.TrimSpace(ref.Key) == "" {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_scope", fmt.Sprintf("scope_key is required for %s scope", ref.Kind))
		return speechcustomize.ScopeRef{}, false
	}
	if explicit {
		if _, ok := internalcustomize.StorageScopeForRef(speechstorage.ScopeFromContext(r.Context()), ref); !ok {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_scope", fmt.Sprintf("scope %s is not resolvable", ref.Kind))
			return speechcustomize.ScopeRef{}, false
		}
	}
	return ref, true
}

const scopeKindProblem = "scope must be one of builtin, app, install, org, workspace, user, session"

// scopeRef reads the scope selection from the query. It returns the resolved
// ref, whether the caller selected a scope explicitly, and a non-empty problem
// message when the query violates the published contract. The contract
// (docs/server/openapi.v1.yaml) says an empty scope_key is not the same as
// omitting it and that scope_key only means something next to scope; both
// used to be accepted silently, which the OpenAPI fuzz gate reports as
// "API accepted schema-violating request".
func scopeRef(r *http.Request, fallback speechcustomize.ScopeRef) (speechcustomize.ScopeRef, bool, string) {
	query := r.URL.Query()
	keyValues, keyPresent := query["scope_key"]
	key := ""
	if keyPresent && len(keyValues) > 0 {
		key = strings.TrimSpace(keyValues[0])
	}
	if keyPresent && key == "" {
		return speechcustomize.ScopeRef{}, true, "scope_key must not be empty; omit it for unkeyed scopes"
	}
	if values, exists := query["scope"]; exists {
		scope := ""
		if len(values) > 0 {
			scope = values[0]
		}
		kind := speechcustomize.ScopeKind(scope)
		if !isPublishedScopeKind(kind) {
			return speechcustomize.ScopeRef{}, true, scopeKindProblem
		}
		return speechcustomize.ScopeRef{Kind: kind, Key: key}, true, ""
	}
	if keyPresent {
		return speechcustomize.ScopeRef{}, true, "scope_key requires scope"
	}
	if fallback.Kind != "" && !isPublishedScopeKind(fallback.Kind) {
		return speechcustomize.ScopeRef{}, true, scopeKindProblem
	}
	return fallback, strings.TrimSpace(string(fallback.Kind)) != "" || strings.TrimSpace(fallback.Key) != "", ""
}

func isPublishedScopeKind(kind speechcustomize.ScopeKind) bool {
	switch kind {
	case speechcustomize.ScopeBuiltin,
		speechcustomize.ScopeApp,
		speechcustomize.ScopeInstall,
		speechcustomize.ScopeOrg,
		speechcustomize.ScopeWorkspace,
		speechcustomize.ScopeUser,
		speechcustomize.ScopeSession:
		return true
	default:
		return false
	}
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

func scopeRequiresKey(kind speechcustomize.ScopeKind) bool {
	switch kind {
	case speechcustomize.ScopeOrg, speechcustomize.ScopeWorkspace, speechcustomize.ScopeUser, speechcustomize.ScopeSession:
		return true
	default:
		return false
	}
}
