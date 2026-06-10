//go:build linux

package customization

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/store"
	speechcustomize "github.com/kombifyio/SpeechKit/pkg/speechkit/customize"
)

type fakeCustomizationStore struct {
	listCalls int
}

func (f *fakeCustomizationStore) ReplaceWords(context.Context, string, []speechcustomize.Word) error {
	return nil
}

func (f *fakeCustomizationStore) ListWords(context.Context, store.CustomizationListOpts) ([]speechcustomize.Word, error) {
	f.listCalls++
	return nil, nil
}

func (f *fakeCustomizationStore) RecordWordUsage(context.Context, string, string) error {
	return nil
}

func (f *fakeCustomizationStore) ReplaceReplacements(context.Context, string, []speechcustomize.Replacement) error {
	return nil
}

func (f *fakeCustomizationStore) ListReplacements(context.Context, store.CustomizationListOpts) ([]speechcustomize.Replacement, error) {
	f.listCalls++
	return nil, nil
}

func (f *fakeCustomizationStore) RecordReplacementUsage(context.Context, string) error {
	return nil
}

func (f *fakeCustomizationStore) ReplaceLexicons(context.Context, string, []speechcustomize.Lexicon) error {
	return nil
}

func (f *fakeCustomizationStore) ListLexicons(context.Context, store.CustomizationListOpts) ([]speechcustomize.Lexicon, error) {
	f.listCalls++
	return nil, nil
}

func (f *fakeCustomizationStore) ReplaceRulesets(context.Context, string, []speechcustomize.Ruleset) error {
	return nil
}

func (f *fakeCustomizationStore) ListRulesets(context.Context, store.CustomizationListOpts) ([]speechcustomize.Ruleset, error) {
	f.listCalls++
	return nil, nil
}

func TestReadEndpointsRejectInvalidScopeQuery(t *testing.T) {
	for _, tc := range []struct {
		name    string
		target  string
		handler func(http.ResponseWriter, *http.Request)
	}{
		{name: "words", target: "/v1/words?scope=bogus"},
		{name: "replacements", target: "/v1/replacements?scope=bogus"},
		{name: "lexicons", target: "/v1/lexicons?scope=bogus"},
		{name: "rulesets", target: "/v1/rulesets?scope=bogus"},
		{name: "pack", target: "/v1/customization/pack?scope=bogus"},
		{name: "strict enum", target: "/v1/replacements?scope=tenant"},
		{name: "empty scope", target: "/v1/replacements?scope="},
		{name: "spaced scope", target: "/v1/replacements?scope=%20org%20"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeCustomizationStore{}
			h := New(store)
			handler := tc.handler
			if handler == nil {
				switch tc.name {
				case "words":
					handler = h.words
				case "replacements", "strict enum", "empty scope", "spaced scope":
					handler = h.replacements
				case "lexicons":
					handler = h.lexicons
				case "rulesets":
					handler = h.rulesets
				case "pack":
					handler = h.pack
				}
			}

			rec := httptest.NewRecorder()
			handler(rec, httptest.NewRequest(http.MethodGet, tc.target, nil))

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
			}
			if store.listCalls != 0 {
				t.Fatalf("store was called %d times for invalid scope", store.listCalls)
			}
		})
	}
}
