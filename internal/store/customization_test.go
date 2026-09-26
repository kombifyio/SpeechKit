package store

import (
	"context"
	"path/filepath"
	"testing"

	speechcustomize "github.com/kombifyio/SpeechKit/pkg/speechkit/customize"
)

func TestCustomizationStoreWordsReplacementsAndDictionaryProjection(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := New(StoreConfig{Backend: "sqlite", SQLitePath: dbPath, MaxAudioStorageMB: 100})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	customizationStore, ok := s.(CustomizationStore)
	if !ok {
		t.Fatal("sqlite store does not implement CustomizationStore")
	}

	ctx := context.Background()
	if err := customizationStore.ReplaceWords(ctx, "de", []speechcustomize.Word{
		{Term: "Kombify", Language: "de-DE", Source: "settings", Enabled: true},
		{Term: "AcmeOS", Language: "de", Source: "settings", Enabled: true},
	}); err != nil {
		t.Fatalf("ReplaceWords: %v", err)
	}
	if err := customizationStore.ReplaceReplacements(ctx, "de", []speechcustomize.Replacement{
		{
			Kind:     speechcustomize.KindSubstitution,
			Language: "de",
			Modes:    []speechcustomize.Mode{speechcustomize.ModeDictation, speechcustomize.ModeAssist},
			Stage:    speechcustomize.StagePostSTT,
			Match:    speechcustomize.Match{Type: speechcustomize.MatchSpokenAlias, Pattern: "kombi fire", WordBoundary: true},
			Output:   speechcustomize.ReplacementOutput{Text: "Kombify"},
			Enabled:  true,
			Source:   "settings",
		},
	}); err != nil {
		t.Fatalf("ReplaceReplacements: %v", err)
	}

	words, err := customizationStore.ListWords(ctx, CustomizationListOpts{Language: "de-DE"})
	if err != nil {
		t.Fatalf("ListWords: %v", err)
	}
	if len(words) != 2 {
		t.Fatalf("ListWords len = %d, want 2", len(words))
	}
	replacements, err := customizationStore.ListReplacements(ctx, CustomizationListOpts{Language: "de", Mode: speechcustomize.ModeAssist, Stage: speechcustomize.StagePostSTT})
	if err != nil {
		t.Fatalf("ListReplacements: %v", err)
	}
	if len(replacements) != 1 || replacements[0].Output.Text != "Kombify" {
		t.Fatalf("ListReplacements = %+v", replacements)
	}

	dictionaryStore := s.(UserDictionaryStore)
	entries, err := dictionaryStore.ListUserDictionaryEntries(ctx, "de")
	if err != nil {
		t.Fatalf("ListUserDictionaryEntries: %v", err)
	}
	if len(entries) != 2 || entries[0].Spoken != "kombi fire" || entries[0].Canonical != "Kombify" {
		t.Fatalf("dictionary projection = %+v", entries)
	}
}

func TestCustomizationStoreReplaceWordsWithOptionsIsSourceAware(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := New(StoreConfig{Backend: "sqlite", SQLitePath: dbPath, MaxAudioStorageMB: 100})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	customizationStore := s.(CustomizationStore)
	sourceStore := s.(CustomizationSourceStore)
	ctx := context.Background()
	if err := customizationStore.ReplaceWords(ctx, "de", []speechcustomize.Word{
		{Term: "UserTerm", Language: "de", Source: "settings", Enabled: true},
	}); err != nil {
		t.Fatalf("ReplaceWords settings: %v", err)
	}
	if err := sourceStore.ReplaceWordsWithOptions(ctx, CustomizationReplaceOpts{Language: "de", Source: "pack:demo"}, []speechcustomize.Word{
		{Term: "PackTerm", Language: "de", Source: "pack:demo", Enabled: true},
	}); err != nil {
		t.Fatalf("ReplaceWords pack: %v", err)
	}
	if err := sourceStore.ReplaceWordsWithOptions(ctx, CustomizationReplaceOpts{Language: "de", Source: "pack:demo"}, []speechcustomize.Word{
		{Term: "PackTerm2", Language: "de", Source: "pack:demo", Enabled: true},
	}); err != nil {
		t.Fatalf("ReplaceWords pack second import: %v", err)
	}
	words, err := customizationStore.ListWords(ctx, CustomizationListOpts{Language: "de", IncludeDisabled: true})
	if err != nil {
		t.Fatalf("ListWords: %v", err)
	}
	got := map[string]bool{}
	for _, word := range words {
		got[word.Term] = true
	}
	if !got["UserTerm"] || !got["PackTerm2"] || got["PackTerm"] {
		t.Fatalf("source-aware words = %+v", words)
	}
}

func TestCustomizationStoreReplaceWordsMergesDuplicateTerms(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := New(StoreConfig{Backend: "sqlite", SQLitePath: dbPath, MaxAudioStorageMB: 100})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	customizationStore := s.(CustomizationStore)
	ctx := context.Background()
	if err := customizationStore.ReplaceWords(ctx, "de", []speechcustomize.Word{
		{ID: "legacy_dictionary_word_1", Term: "Kombify", Language: "de", SoundsLike: []string{"kombi fire"}, Source: "settings", Enabled: true},
		{ID: "legacy_dictionary_word_2", Term: "kombify", Language: "de", SoundsLike: []string{"combi fy"}, Source: "settings", Enabled: true},
	}); err != nil {
		t.Fatalf("ReplaceWords: %v", err)
	}
	if err := customizationStore.ReplaceReplacements(ctx, "de", []speechcustomize.Replacement{
		{
			Kind:     speechcustomize.KindSynonym,
			Language: "de",
			Stage:    speechcustomize.StagePostSTT,
			Match:    speechcustomize.Match{Type: speechcustomize.MatchSpokenAlias, Pattern: "kombi fire", WordBoundary: true},
			Output:   speechcustomize.ReplacementOutput{Text: "Kombify"},
			Enabled:  true,
			Source:   "settings",
		},
		{
			Kind:     speechcustomize.KindSynonym,
			Language: "de",
			Stage:    speechcustomize.StagePostSTT,
			Match:    speechcustomize.Match{Type: speechcustomize.MatchSpokenAlias, Pattern: "combi fy", WordBoundary: true},
			Output:   speechcustomize.ReplacementOutput{Text: "Kombify"},
			Enabled:  true,
			Source:   "settings",
		},
	}); err != nil {
		t.Fatalf("ReplaceReplacements: %v", err)
	}

	words, err := customizationStore.ListWords(ctx, CustomizationListOpts{Language: "de", IncludeDisabled: true})
	if err != nil {
		t.Fatalf("ListWords: %v", err)
	}
	if len(words) != 1 || words[0].Term != "Kombify" {
		t.Fatalf("merged words = %+v", words)
	}
	if len(words[0].SoundsLike) != 2 {
		t.Fatalf("merged aliases = %+v", words[0].SoundsLike)
	}
	replacements, err := customizationStore.ListReplacements(ctx, CustomizationListOpts{Language: "de", IncludeDisabled: true})
	if err != nil {
		t.Fatalf("ListReplacements: %v", err)
	}
	if len(replacements) != 2 {
		t.Fatalf("shared-output replacements = %+v", replacements)
	}
}

func TestCustomizationStoreReplaceVocabularyWritesWordsAndAliasesTogether(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := New(StoreConfig{Backend: "sqlite", SQLitePath: dbPath, MaxAudioStorageMB: 100})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	vocabStore := s.(CustomizationVocabularyStore)
	ctx := context.Background()
	if err := vocabStore.ReplaceVocabularyWithOptions(ctx, CustomizationReplaceOpts{Language: "de", Source: "settings"}, []speechcustomize.Word{
		{Term: "Kombify", Language: "de", SoundsLike: []string{"kombi fire"}, Enabled: true, Source: "settings"},
	}, []speechcustomize.Replacement{
		{
			Kind:     speechcustomize.KindSubstitution,
			Language: "de",
			Stage:    speechcustomize.StagePostSTT,
			Match:    speechcustomize.Match{Type: speechcustomize.MatchPhrase, Pattern: "punkt", WordBoundary: true},
			Output:   speechcustomize.ReplacementOutput{Text: "."},
			Enabled:  true,
			Source:   "settings",
		},
	}); err != nil {
		t.Fatalf("ReplaceVocabulary: %v", err)
	}

	customizationStore := s.(CustomizationStore)
	words, err := customizationStore.ListWords(ctx, CustomizationListOpts{Language: "de", IncludeDisabled: true})
	if err != nil {
		t.Fatalf("ListWords: %v", err)
	}
	if len(words) != 1 || words[0].Term != "Kombify" {
		t.Fatalf("words = %+v", words)
	}
	replacements, err := customizationStore.ListReplacements(ctx, CustomizationListOpts{Language: "de", IncludeDisabled: true})
	if err != nil {
		t.Fatalf("ListReplacements: %v", err)
	}
	hasAlias, hasDot := false, false
	for _, replacement := range replacements {
		if replacement.Match.Pattern == "kombi fire" && replacement.Output.Text == "Kombify" {
			hasAlias = true
		}
		if replacement.Match.Pattern == "punkt" && replacement.Output.Text == "." {
			hasDot = true
		}
	}
	if !hasAlias || !hasDot {
		t.Fatalf("replacements = %+v", replacements)
	}
}
