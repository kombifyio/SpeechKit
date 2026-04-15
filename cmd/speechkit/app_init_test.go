package main

import (
	"testing"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/shortcuts"
)

func TestBuildShortcutResolverUsesConfiguredAliases(t *testing.T) {
	cfg := &config.Config{
		Shortcuts: config.ShortcutsConfig{
			Locale: map[string]config.ShortcutLocaleConfig{
				"de": {
					LeadingFillers: []string{"bitte"},
					Summarize:      []string{"kurzfassung"},
				},
			},
		},
	}

	resolution := buildShortcutResolver(cfg).Resolve("Bitte Kurzfassung in drei Punkten", "de-DE")

	if got, want := resolution.Intent, shortcuts.IntentSummarize; got != want {
		t.Fatalf("Intent = %q, want %q", got, want)
	}
	if got, want := resolution.Payload, "in drei punkten"; got != want {
		t.Fatalf("Payload = %q, want %q", got, want)
	}
}

func TestBuildShortcutResolverKeepsDefaultCatalog(t *testing.T) {
	cfg := &config.Config{}

	resolution := buildShortcutResolver(cfg).Resolve("summarize this in bullets", "en")

	if got, want := resolution.Intent, shortcuts.IntentSummarize; got != want {
		t.Fatalf("Intent = %q, want %q", got, want)
	}
	if got, want := resolution.Payload, "in bullets"; got != want {
		t.Fatalf("Payload = %q, want %q", got, want)
	}
}
