package catalog

import (
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

// The Foundry assist profile moved from GPT-5.1 to the GPT-5.6 family; configs
// written before that must keep resolving to the same profile.
func TestFoundryAssistProfileAliasAndVariants(t *testing.T) {
	if got := speechkit.NormalizeProviderProfileID("assist.foundry.gpt-5.1"); got != "assist.foundry.gpt-5.6" {
		t.Fatalf("alias = %q", got)
	}
	profile, ok := DefaultCatalog().Profile("assist.foundry.gpt-5.1")
	if !ok {
		t.Fatal("legacy id must resolve through the catalog")
	}
	if profile.ID != "assist.foundry.gpt-5.6" || profile.ModelID != "gpt-5.6-terra" {
		t.Fatalf("profile = %s / %s", profile.ID, profile.ModelID)
	}
	want := map[string]bool{"gpt-5.6-terra": false, "gpt-5.6-sol": false, "gpt-5.6-luna": false, "MAI-Thinking-1": false}
	for _, variant := range profile.Variants {
		if _, listed := want[variant.ModelID]; listed {
			want[variant.ModelID] = true
		}
		if variant.ModelID == "gpt-5.6-terra" && !variant.Recommended {
			t.Fatal("Terra must be the recommended variant")
		}
	}
	for id, seen := range want {
		if !seen {
			t.Fatalf("variant %s missing", id)
		}
	}
}

// The GPT-4o speech profiles were retired; configs naming them resolve to
// the Microsoft models the resource serves without a deployment.
func TestRetiredFoundrySpeechProfilesResolveToMAI(t *testing.T) {
	catalog := DefaultCatalog()
	stt, ok := catalog.Profile("stt.foundry.gpt-4o-mini-transcribe")
	if !ok || stt.ID != "stt.foundry.mai-transcribe-2" {
		t.Fatalf("retired STT profile resolved to %q (ok=%v)", stt.ID, ok)
	}
	tts, ok := catalog.Profile("tts.foundry.gpt-4o-mini-tts")
	if !ok || tts.ID != "tts.foundry.mai-voice-2" {
		t.Fatalf("retired TTS profile resolved to %q (ok=%v)", tts.ID, ok)
	}
	current, ok := catalog.Profile("stt.foundry.gpt-transcribe")
	if !ok || current.ModelID != "gpt-transcribe" || current.Recommended {
		t.Fatalf("OpenAI-route STT profile = %+v (ok=%v)", current, ok)
	}
	for _, id := range []string{"stt.foundry.mai-transcribe-2", "assist.foundry.gpt-5.6", "realtime.foundry.voice-live"} {
		profile, ok := catalog.Profile(id)
		if !ok {
			t.Fatalf("profile %s missing", id)
		}
		for _, variant := range profile.Variants {
			lower := strings.ToLower(variant.ModelID)
			if strings.Contains(lower, "gpt-4") || strings.Contains(lower, "gpt-5-") || strings.Contains(lower, "gpt-5.4-mini") || strings.Contains(lower, "1.5") {
				t.Fatalf("profile %s still offers a previous-generation variant %q", id, variant.ModelID)
			}
		}
	}
}
