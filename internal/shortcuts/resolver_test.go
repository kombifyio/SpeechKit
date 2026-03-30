package shortcuts

import "testing"

func TestResolveSummarizeWithGermanPrefix(t *testing.T) {
	resolution := Resolve("Bitte fassen wir dies zusammen")

	if resolution.Intent != IntentSummarize {
		t.Fatalf("Intent = %q, want %q", resolution.Intent, IntentSummarize)
	}
	if got, want := resolution.Alias, "fassen wir dies zusammen"; got != want {
		t.Fatalf("Alias = %q, want %q", got, want)
	}
	if got, want := resolution.Payload, ""; got != want {
		t.Fatalf("Payload = %q, want %q", got, want)
	}
}

func TestResolveSummarizeWithEnglishSuffix(t *testing.T) {
	resolution := Resolve("Summarize this article")

	if resolution.Intent != IntentSummarize {
		t.Fatalf("Intent = %q, want %q", resolution.Intent, IntentSummarize)
	}
	if got, want := resolution.Alias, "summarize this"; got != want {
		t.Fatalf("Alias = %q, want %q", got, want)
	}
	if got, want := resolution.Payload, "article"; got != want {
		t.Fatalf("Payload = %q, want %q", got, want)
	}
}

func TestResolveCopyLastAliases(t *testing.T) {
	resolution := Resolve("Copy last transcription")

	if resolution.Intent != IntentCopyLast {
		t.Fatalf("Intent = %q, want %q", resolution.Intent, IntentCopyLast)
	}
	if got, want := resolution.Alias, "copy last transcription"; got != want {
		t.Fatalf("Alias = %q, want %q", got, want)
	}
}

func TestResolveDoesNotTriggerSummarizeMidSentence(t *testing.T) {
	resolution := Resolve("Please keep this paragraph and then summarize this tomorrow")

	if resolution.Intent != IntentNone {
		t.Fatalf("Intent = %q, want %q", resolution.Intent, IntentNone)
	}
}

func TestResolveDoesNotTriggerGermanAliasMidSentence(t *testing.T) {
	resolution := Resolve("Wir sollten den Text erst lesen und dann fassen wir dies zusammen spaeter")

	if resolution.Intent != IntentNone {
		t.Fatalf("Intent = %q, want %q", resolution.Intent, IntentNone)
	}
}

func TestResolveZusammenfassenWithPayload(t *testing.T) {
	resolution := Resolve("Zusammenfassen in zwei saetzen")

	if resolution.Intent != IntentSummarize {
		t.Fatalf("Intent = %q, want %q", resolution.Intent, IntentSummarize)
	}
	if got, want := resolution.Alias, "zusammenfassen"; got != want {
		t.Fatalf("Alias = %q, want %q", got, want)
	}
	if got, want := resolution.Payload, "in zwei saetzen"; got != want {
		t.Fatalf("Payload = %q, want %q", got, want)
	}
}

func TestResolveZusammenfassungAlias(t *testing.T) {
	resolution := Resolve("Zusammenfassung in Stichpunkten")

	if resolution.Intent != IntentSummarize {
		t.Fatalf("Intent = %q, want %q", resolution.Intent, IntentSummarize)
	}
	if got, want := resolution.Alias, "zusammenfassung"; got != want {
		t.Fatalf("Alias = %q, want %q", got, want)
	}
	if got, want := resolution.Payload, "in stichpunkten"; got != want {
		t.Fatalf("Payload = %q, want %q", got, want)
	}
}

func TestResolveMultilingualCopyLast(t *testing.T) {
	tests := []struct {
		input string
		lang  string
	}{
		{"copy that", "en"},
		{"kopiere das letzte", "de"},
		{"kopier das", "de"},
		{"in die Zwischenablage", "de"},
		{"copier le dernier", "fr"},
		{"copiar lo ultimo", "es"},
		{"bitte kopier das", "de-polite"},
		{"please copy that", "en-polite"},
		{"s'il te plait copier", "fr-polite"},
		{"por favor copiar", "es-polite"},
	}
	for _, tt := range tests {
		t.Run(tt.lang+"_"+tt.input, func(t *testing.T) {
			res := Resolve(tt.input)
			if res.Intent != IntentCopyLast {
				t.Fatalf("Intent = %q, want %q for %q (%s)", res.Intent, IntentCopyLast, tt.input, tt.lang)
			}
		})
	}
}

func TestResolveMultilingualInsertLast(t *testing.T) {
	tests := []struct {
		input string
		lang  string
	}{
		{"paste that", "en"},
		{"insert that", "en"},
		{"einfuegen", "de"},
		{"fueg das ein", "de"},
		{"coller ca", "fr"},
		{"pegar eso", "es"},
	}
	for _, tt := range tests {
		t.Run(tt.lang+"_"+tt.input, func(t *testing.T) {
			res := Resolve(tt.input)
			if res.Intent != IntentInsertLast {
				t.Fatalf("Intent = %q, want %q for %q (%s)", res.Intent, IntentInsertLast, tt.input, tt.lang)
			}
		})
	}
}

func TestResolveMultilingualSummarize(t *testing.T) {
	tests := []struct {
		input string
		lang  string
	}{
		{"summarize", "en"},
		{"give me a summary", "en"},
		{"sum it up", "en"},
		{"zusammenfassen", "de"},
		{"fass zusammen", "de"},
		{"fass das zusammen", "de"},
		{"kurz zusammenfassen", "de"},
		{"mach eine zusammenfassung", "de"},
		{"resume ca", "fr"},
		{"fais un resume", "fr"},
		{"resumir esto", "es"},
		{"haz un resumen", "es"},
		{"bitte zusammenfassen", "de-polite"},
		{"could you please summarize", "en-polite"},
	}
	for _, tt := range tests {
		t.Run(tt.lang+"_"+tt.input, func(t *testing.T) {
			res := Resolve(tt.input)
			if res.Intent != IntentSummarize {
				t.Fatalf("Intent = %q, want %q for %q (%s)", res.Intent, IntentSummarize, tt.input, tt.lang)
			}
		})
	}
}

func TestResolveMultilingualQuickNote(t *testing.T) {
	tests := []struct {
		input string
		lang  string
	}{
		{"quick note buy milk", "en"},
		{"take a note meeting at 3pm", "en"},
		{"notiz einkaufen gehen", "de"},
		{"schnelle notiz termin um 15 uhr", "de"},
		{"note rapide acheter du pain", "fr"},
		{"nota rapida comprar leche", "es"},
	}
	for _, tt := range tests {
		t.Run(tt.lang+"_"+tt.input, func(t *testing.T) {
			res := Resolve(tt.input)
			if res.Intent != IntentQuickNote {
				t.Fatalf("Intent = %q, want %q for %q (%s)", res.Intent, IntentQuickNote, tt.input, tt.lang)
			}
			if res.Payload == "" {
				t.Fatalf("expected payload for %q (%s)", tt.input, tt.lang)
			}
		})
	}
}
