package customize

import (
	"context"
	"testing"

	customtemplates "github.com/kombifyio/SpeechKit/internal/customize/templates"
	speechcustomize "github.com/kombifyio/SpeechKit/pkg/speechkit/customize"
	speechstorage "github.com/kombifyio/SpeechKit/pkg/speechkit/storage"
)

type fakeCustomizationStore struct {
	words        map[string][]speechcustomize.Word
	replacements map[string][]speechcustomize.Replacement
}

func (s fakeCustomizationStore) ListWords(ctx context.Context, opts speechcustomize.ListOptions) ([]speechcustomize.Word, error) {
	scopeKey := speechstorage.ScopeFromContext(ctx).Key()
	return append([]speechcustomize.Word(nil), s.words[scopeKey]...), nil
}

func (s fakeCustomizationStore) ListReplacements(ctx context.Context, opts speechcustomize.ListOptions) ([]speechcustomize.Replacement, error) {
	scopeKey := speechstorage.ScopeFromContext(ctx).Key()
	out := make([]speechcustomize.Replacement, 0, len(s.replacements[scopeKey]))
	for _, replacement := range s.replacements[scopeKey] {
		if opts.Stage != "" && replacement.Stage != opts.Stage {
			continue
		}
		if opts.Mode != "" && len(replacement.Modes) > 0 {
			matched := false
			for _, mode := range replacement.Modes {
				matched = matched || speechcustomize.NormalizeMode(mode) == speechcustomize.NormalizeMode(opts.Mode)
			}
			if !matched {
				continue
			}
		}
		out = append(out, replacement)
	}
	return out, nil
}

func TestServiceResolveScopePrecedence(t *testing.T) {
	base := speechstorage.Scope{InstallID: speechstorage.LocalInstallID, UserID: "alice", TenantID: "acme"}
	appScope, ok := StorageScopeForRef(base, speechcustomize.ScopeRef{Kind: speechcustomize.ScopeApp})
	if !ok {
		t.Fatal("app scope unresolved")
	}
	userScope, ok := StorageScopeForRef(base, speechcustomize.ScopeRef{Kind: speechcustomize.ScopeUser})
	if !ok {
		t.Fatal("user scope unresolved")
	}
	store := fakeCustomizationStore{words: map[string][]speechcustomize.Word{
		appScope.Key(): {
			{ID: "word_shared", Term: "AcmeOS", Language: "de", Source: "developer", Enabled: true, Weight: 0.4},
			{ID: "word_app", Term: "Kombify", Language: "de", Source: "developer", Enabled: true},
		},
		userScope.Key(): {
			{ID: "word_shared", Term: "AcmeOS", Language: "de", Source: "settings", Enabled: true, Weight: 1.0},
		},
	}}
	service := NewService(ServiceOptions{Store: store, ScopeOrder: []speechcustomize.ScopeRef{{Kind: speechcustomize.ScopeApp}, {Kind: speechcustomize.ScopeUser}}})
	resolved, err := service.Resolve(speechstorage.WithScope(context.Background(), base), speechcustomize.Context{Language: "de"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(resolved.Words) != 2 {
		t.Fatalf("words = %+v", resolved.Words)
	}
	for _, word := range resolved.Words {
		if word.ID == "word_shared" && word.Weight != 1.0 {
			t.Fatalf("user word did not override app word: %+v", word)
		}
	}
}

func TestServiceApplyRespectsModeFilter(t *testing.T) {
	base := speechstorage.Scope{InstallID: speechstorage.LocalInstallID}
	installScope, ok := StorageScopeForRef(base, speechcustomize.ScopeRef{Kind: speechcustomize.ScopeInstall})
	if !ok {
		t.Fatal("install scope unresolved")
	}
	store := fakeCustomizationStore{replacements: map[string][]speechcustomize.Replacement{
		installScope.Key(): {
			{
				ID:      "assist_only",
				Kind:    speechcustomize.KindSubstitution,
				Stage:   speechcustomize.StagePostSTT,
				Modes:   []speechcustomize.Mode{speechcustomize.ModeAssist},
				Match:   speechcustomize.Match{Type: speechcustomize.MatchPhrase, Pattern: "alpha", WordBoundary: true},
				Output:  speechcustomize.ReplacementOutput{Text: "ASSIST"},
				Enabled: true,
			},
			{
				ID:      "dictation_only",
				Kind:    speechcustomize.KindSubstitution,
				Stage:   speechcustomize.StagePostSTT,
				Modes:   []speechcustomize.Mode{speechcustomize.ModeDictation},
				Match:   speechcustomize.Match{Type: speechcustomize.MatchPhrase, Pattern: "beta", WordBoundary: true},
				Output:  speechcustomize.ReplacementOutput{Text: "DICTATION"},
				Enabled: true,
			},
		},
	}}
	service := NewService(ServiceOptions{Store: store, ScopeOrder: []speechcustomize.ScopeRef{{Kind: speechcustomize.ScopeInstall}}})
	applied, err := service.Apply(speechstorage.WithScope(context.Background(), base), speechcustomize.Context{Mode: speechcustomize.ModeDictation, Stage: speechcustomize.StagePostSTT}, "alpha beta")
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if applied.Text != "alpha DICTATION" {
		t.Fatalf("text = %q, want alpha DICTATION", applied.Text)
	}
}

func TestServiceAppliesNativeStandardPunctuationTemplate(t *testing.T) {
	service := NewService(ServiceOptions{ActiveTemplateIDs: []string{customtemplates.StandardPunctuationID}})
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "digit version", in: "3 Punkt 5 Punkt 7", want: "3.5.7"},
		{name: "spoken digit version", in: "drei Punkt fünf Punkt sieben", want: "3.5.7"},
		{name: "ordinary sentence", in: "der Punkt ist wichtig", want: "der Punkt ist wichtig"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			applied, err := service.Apply(context.Background(), speechcustomize.Context{
				Mode:     speechcustomize.ModeDictation,
				Language: "de",
				Stage:    speechcustomize.StagePostSTT,
			}, tc.in)
			if err != nil {
				t.Fatalf("Apply: %v", err)
			}
			if applied.Text != tc.want {
				t.Fatalf("Apply(%q) = %q, want %q", tc.in, applied.Text, tc.want)
			}
		})
	}
}

func TestServiceUserScopeOverridesNativeTemplateReplacement(t *testing.T) {
	pack, _, ok := customtemplates.ResolveTemplatePack(customtemplates.StandardPunctuationID, "")
	if !ok || len(pack.Replacements) == 0 {
		t.Fatal("standard punctuation template missing replacements")
	}
	base := speechstorage.Scope{InstallID: speechstorage.LocalInstallID, UserID: "alice"}
	userScope, ok := StorageScopeForRef(base, speechcustomize.ScopeRef{Kind: speechcustomize.ScopeUser})
	if !ok {
		t.Fatal("user scope unresolved")
	}
	override := pack.Replacements[0]
	override.ID = "user_override"
	override.Output = speechcustomize.ReplacementOutput{Text: "custom"}
	override.Source = "settings"
	store := fakeCustomizationStore{replacements: map[string][]speechcustomize.Replacement{
		userScope.Key(): {override},
	}}
	service := NewService(ServiceOptions{
		Store:             store,
		ScopeOrder:        []speechcustomize.ScopeRef{{Kind: speechcustomize.ScopeUser}},
		ActiveTemplateIDs: []string{customtemplates.StandardPunctuationID},
	})
	applied, err := service.Apply(speechstorage.WithScope(context.Background(), base), speechcustomize.Context{
		Mode:     speechcustomize.ModeDictation,
		Language: "de",
		Stage:    speechcustomize.StagePostSTT,
	}, "3 Punkt 5")
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if applied.Text != "custom" {
		t.Fatalf("Apply = %q, want custom", applied.Text)
	}
}
