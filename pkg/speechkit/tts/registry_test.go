package tts

import "testing"

func TestBuildRouter_AssemblesEnabledProviders(t *testing.T) {
	r, ok, _ := BuildRouter(StrategyCloudFirst, EnabledProviders{
		OpenAI: &OpenAIOpts{APIKey: "k", Voice: "nova"},
		Google: &GoogleOpts{APIKey: "k"},
	})
	if !ok || r == nil {
		t.Fatal("expected an enabled router")
	}
	if got := len(r.providers); got != 2 {
		t.Fatalf("router has %d providers, want 2", got)
	}
	if r.providers[0].Name() != "openai" {
		t.Errorf("first provider = %q, want openai (declaration order)", r.providers[0].Name())
	}
}

func TestBuildRouter_EmptyIsNotEnabled(t *testing.T) {
	r, ok, _ := BuildRouter(StrategyCloudFirst, EnabledProviders{})
	if ok || r != nil {
		t.Fatalf("empty set should yield ok=false, nil router; got ok=%v router=%v", ok, r)
	}
}

func TestBuildRouter_PinsPreferredProfile(t *testing.T) {
	r, ok, notes := BuildRouter(StrategyCloudFirst, EnabledProviders{
		OpenAI:             &OpenAIOpts{APIKey: "k"},
		Google:             &GoogleOpts{APIKey: "k"},
		PreferredProfileID: "tts.google.studio-o-de", // maps to "google"
	})
	if !ok {
		t.Fatal("expected an enabled router")
	}
	if r.providers[0].Name() != "google" {
		t.Errorf("preferred profile should pin google first; got %q", r.providers[0].Name())
	}
	found := false
	for _, n := range notes {
		if n == "TTS: model_selection profile tts.google.studio-o-de pinned provider google first" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a pinning note; got %v", notes)
	}
}
