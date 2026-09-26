package skills_test

import (
	"context"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/assist/shortcuts"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/assist/skills"
)

func TestDefaultUtilityRegistryDefinesAssistUtilities(t *testing.T) {
	registry := skills.DefaultUtilityRegistry()

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
	if registry.Supports(shortcuts.IntentHomeAssistant) {
		t.Fatal("home assistant must stay opt-in in the default registry so settings catalogs report it as default_enabled=false")
	}
	if got := len(registry.List()); got != 13 {
		t.Fatalf("List() = %d definitions, want the 13 built-in utilities", got)
	}
}

func TestUtilityRegistryRegisterFillsDefaults(t *testing.T) {
	registry := skills.NewUtilityRegistry()
	registry.Register(skills.UtilityDefinition{Intent: shortcuts.IntentCopyLast, Enabled: true})
	registry.Register(skills.UtilityDefinition{Intent: shortcuts.IntentNone, Enabled: true})

	def, ok := registry.Definition(shortcuts.IntentCopyLast)
	if !ok {
		t.Fatal("registered utility must be enabled")
	}
	if def.ID != skills.UtilityCopyLast || def.DefaultSurface != speechkit.AssistSurfaceActionAck || def.DefaultKind != "utility_action" {
		t.Fatalf("defaults not applied: %#v", def)
	}
	if got := len(registry.List()); got != 1 {
		t.Fatalf("List() = %d definitions, want the intent-less registration ignored", got)
	}
}

func TestMatcherUsesUtilityRegistryAfterExactShortcutResolution(t *testing.T) {
	registry := shortcuts.NewRegistry()
	registry.RegisterLexicon(shortcuts.IntentLexicon{
		Intent:  shortcuts.IntentCopyLast,
		Locale:  "en",
		Phrases: []shortcuts.Phrase{{Value: "copy last", Prefix: true}},
	})
	utilities := skills.NewUtilityRegistry()
	req := speechkit.AssistRequest{Text: "copy last please", Locale: "en"}

	cat := skills.New(skills.Options{Resolver: shortcuts.NewResolver(registry), Registry: utilities})
	if _, matched, err := cat.Matcher().MatchTool(context.Background(), req); err != nil || matched {
		t.Fatalf("matched=%v err=%v, want no match while the utility is disabled", matched, err)
	}

	utilities.Register(skills.UtilityDefinition{
		Intent:  shortcuts.IntentCopyLast,
		ID:      skills.UtilityCopyLast,
		Label:   "Copy last",
		Enabled: true,
	})
	cat = skills.New(skills.Options{Resolver: shortcuts.NewResolver(registry), Registry: utilities})
	call, matched, err := cat.Matcher().MatchTool(context.Background(), req)
	if err != nil || !matched {
		t.Fatalf("matched=%v err=%v, want a tool match once the utility is enabled", matched, err)
	}
	if call.Intent != string(shortcuts.IntentCopyLast) || call.Payload != "please" {
		t.Fatalf("call = %#v, want copy_last with payload \"please\"", call)
	}
	if !cat.Registry().Supports(shortcuts.IntentCopyLast) {
		t.Fatal("Registry() must expose the effective registry")
	}
}

func TestMatcherUsesInjectedResolver(t *testing.T) {
	registry := shortcuts.NewRegistry()
	registry.RegisterLeadingFillers("de", "bitte")
	registry.RegisterLexicon(shortcuts.IntentLexicon{
		Intent: shortcuts.IntentSummarize,
		Locale: "de",
		Phrases: []shortcuts.Phrase{
			{Value: "kurzfassung", Prefix: true},
		},
	})

	cat := skills.New(skills.Options{Resolver: shortcuts.NewResolver(registry)})
	call, matched, err := cat.Matcher().MatchTool(context.Background(), speechkit.AssistRequest{
		Text:   "Bitte Kurzfassung in drei Punkten",
		Locale: "de-DE",
	})
	if err != nil || !matched {
		t.Fatalf("matched=%v err=%v, want the configured alias to match", matched, err)
	}
	if got, want := call.Intent, string(shortcuts.IntentSummarize); got != want {
		t.Fatalf("Intent = %q, want %q", got, want)
	}
	if got, want := call.Payload, "in drei punkten"; got != want {
		t.Fatalf("Payload = %q, want %q", got, want)
	}
	if got, want := call.Transcript, "Bitte Kurzfassung in drei Punkten"; got != want {
		t.Fatalf("Transcript = %q, want the full utterance %q", got, want)
	}
}

func TestCatalogAlwaysEnablesHomeAssistantWithoutMutatingHostRegistry(t *testing.T) {
	host := skills.NewUtilityRegistry()
	cat := skills.New(skills.Options{Registry: host})

	def, ok := cat.Registry().Definition(shortcuts.IntentHomeAssistant)
	if !ok {
		t.Fatal("the catalog must claim the Home Assistant intent even when the host registry omits it")
	}
	if def.ID != skills.UtilityHomeAssistant || def.Input != skills.UtilityInputUtterance || !def.Enabled {
		t.Fatalf("effective Home Assistant definition = %#v", def)
	}
	if host.Supports(shortcuts.IntentHomeAssistant) {
		t.Fatal("the host registry must not be mutated by New")
	}
}
