package assist

import (
	"testing"

	"github.com/kombifyio/SpeechKit/internal/shortcuts"
)

func TestDefaultUtilityRegistryDefinesAssistUtilities(t *testing.T) {
	registry := DefaultUtilityRegistry()

	for _, intent := range []shortcuts.Intent{
		shortcuts.IntentCopyLast,
		shortcuts.IntentInsertLast,
		shortcuts.IntentSummarize,
	} {
		def, ok := registry.Definition(intent)
		if !ok {
			t.Fatalf("utility intent %q missing", intent)
		}
		if def.ID == "" || def.DefaultSurface == "" || def.DefaultKind == "" {
			t.Fatalf("utility %q missing contract fields: %#v", intent, def)
		}
	}

	if registry.Supports(shortcuts.IntentQuickNote) {
		t.Fatal("quick note utility should stay disabled until the desktop executor owns a create-note path")
	}
}

func TestRouterUsesUtilityRegistryAfterExactShortcutResolution(t *testing.T) {
	registry := shortcuts.NewRegistry()
	registry.RegisterLexicon(shortcuts.IntentLexicon{
		Intent:  shortcuts.IntentCopyLast,
		Locale:  "en",
		Phrases: []shortcuts.Phrase{{Value: "copy last", Prefix: true}},
	})
	utilities := NewUtilityRegistry()
	router := NewRouter(
		WithResolver(shortcuts.NewResolver(registry)),
		WithUtilityRegistry(utilities),
	)

	decision := router.Decide("copy last please", ProcessOpts{Locale: "en"})
	if decision.Route != RouteDirectReply {
		t.Fatalf("route = %q, want direct reply when utility is disabled", decision.Route)
	}

	utilities.Register(UtilityDefinition{
		Intent:  shortcuts.IntentCopyLast,
		ID:      UtilityCopyLast,
		Label:   "Copy last",
		Enabled: true,
	})

	decision = router.Decide("copy last please", ProcessOpts{Locale: "en"})
	if decision.Route != RouteToolIntent {
		t.Fatalf("route = %q, want tool intent", decision.Route)
	}
	if decision.Utility.ID != UtilityCopyLast {
		t.Fatalf("utility = %q, want %q", decision.Utility.ID, UtilityCopyLast)
	}
}
