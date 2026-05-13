package live

import (
	"strings"
	"testing"
	"time"
)

func TestReminderPromptUsesConfiguredDuration(t *testing.T) {
	got := reminderPrompt("en", 2*time.Minute)
	if !strings.Contains(got, "2 minutes") {
		t.Fatalf("reminder prompt = %q, want configured duration", got)
	}
}

func TestReminderPromptFallsBackToDefaultDuration(t *testing.T) {
	got := reminderPrompt("en", 0)
	if !strings.Contains(got, "5 minutes") {
		t.Fatalf("reminder prompt = %q, want default duration", got)
	}
}

func TestReminderPromptUsesGermanLocale(t *testing.T) {
	got := reminderPrompt("de-DE", 3*time.Minute)
	if !strings.Contains(got, "3 Minuten") {
		t.Fatalf("german reminder prompt = %q, want configured german duration", got)
	}
}

func TestDeactivatePromptUsesLocale(t *testing.T) {
	if got := deactivatePrompt("de"); !strings.Contains(got, "Inaktivitaet") {
		t.Fatalf("german deactivate prompt = %q", got)
	}
	if got := deactivatePrompt("en"); !strings.Contains(got, "inactivity") {
		t.Fatalf("english deactivate prompt = %q", got)
	}
}
