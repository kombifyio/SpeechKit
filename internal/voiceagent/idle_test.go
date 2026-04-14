package voiceagent

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
