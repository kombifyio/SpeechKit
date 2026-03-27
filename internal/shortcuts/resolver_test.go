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
