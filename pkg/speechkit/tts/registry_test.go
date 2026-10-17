package tts

import "testing"

func TestBuildRouter_AssemblesEnabledProviders(t *testing.T) {
	r, ok, _ := BuildRouter(StrategyCloudFirst, EnabledProviders{
		OpenAI: &OpenAIOpts{APIKey: "k", Voice: "nova"},
	})
	if !ok || r == nil {
		t.Fatal("expected an enabled router")
	}
	if got := len(r.providers); got != 1 {
		t.Fatalf("router has %d providers, want 1", got)
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
		PreferredProfileID: "tts.openai.tts-1",
	})
	if !ok {
		t.Fatal("expected an enabled router")
	}
	if r.providers[0].Name() != "openai" {
		t.Errorf("preferred profile should pin openai first; got %q", r.providers[0].Name())
	}
	found := false
	for _, n := range notes {
		if n == "TTS: model_selection profile tts.openai.tts-1 pinned provider openai first" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a pinning note; got %v", notes)
	}
}

// xAI is opt-in and never a default: with no pinned profile it speaks only
// after every other registered provider, local engines included; naming its
// profile is what puts it first.
func TestBuildRouter_XAIRegistersBehindEveryOtherProvider(t *testing.T) {
	enabled := EnabledProviders{
		XAI:              &XAIOpts{APIKey: "k"},
		OpenAI:           &OpenAIOpts{APIKey: "k"},
		SelfHostedOpenAI: &SelfHostedOpenAIOpts{BaseURL: "http://127.0.0.1:8880"},
	}
	r, ok, _ := BuildRouter(StrategyCloudFirst, enabled)
	if !ok {
		t.Fatal("expected an enabled router")
	}
	if last := r.providers[len(r.providers)-1].Name(); last != "xai" {
		t.Fatalf("last provider = %q, want xai behind the local engine", last)
	}

	enabled.PreferredProfileID = "tts.xai.grok-voice"
	r, _, _ = BuildRouter(StrategyCloudFirst, enabled)
	if first := r.providers[0].Name(); first != "xai" {
		t.Fatalf("first provider = %q, want an explicitly pinned xai", first)
	}
}
