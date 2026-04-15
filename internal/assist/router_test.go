package assist

import (
	"testing"

	"github.com/kombifyio/SpeechKit/internal/shortcuts"
)

func TestRouterUsesInjectedResolver(t *testing.T) {
	registry := shortcuts.NewRegistry()
	registry.RegisterLeadingFillers("de", "bitte")
	registry.RegisterLexicon(shortcuts.IntentLexicon{
		Intent: shortcuts.IntentSummarize,
		Locale: "de",
		Phrases: []shortcuts.Phrase{
			{Value: "kurzfassung", Prefix: true},
		},
	})

	router := NewRouter(WithResolver(shortcuts.NewResolver(registry)))
	decision := router.Decide("Bitte Kurzfassung in drei Punkten", ProcessOpts{Locale: "de-DE"})

	if got, want := decision.Route, RouteToolIntent; got != want {
		t.Fatalf("Route = %q, want %q", got, want)
	}
	if got, want := decision.Intent, shortcuts.IntentSummarize; got != want {
		t.Fatalf("Intent = %q, want %q", got, want)
	}
	if got, want := decision.Payload, "in drei punkten"; got != want {
		t.Fatalf("Payload = %q, want %q", got, want)
	}
}
