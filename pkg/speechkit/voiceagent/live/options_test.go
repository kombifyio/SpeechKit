package live

import (
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
)

func TestResolveLiveOptionsUsesRequestAndGlobalDefaults(t *testing.T) {
	resolved := ResolveLiveOptions("deepgram", "realtime.deepgram.voice-agent", LiveConfig{
		Locale:         "de-DE",
		Voice:          "aura-2-viktoria-de",
		VocabularyHint: "Kombify\nSpeechKit\nkombify",
		Options: provideropts.Values{
			provideropts.OptionTurnDetection: true,
		},
	}, nil, nil)

	if resolved.Locale != "de-DE" {
		t.Fatalf("Locale = %q, want de-DE", resolved.Locale)
	}
	if resolved.Voice != "aura-2-viktoria-de" {
		t.Fatalf("Voice = %q", resolved.Voice)
	}
	if got := resolved.Keyterms; len(got) != 2 || got[0] != "Kombify" || got[1] != "SpeechKit" {
		t.Fatalf("Keyterms = %#v", got)
	}
	if !resolved.TurnDetection {
		t.Fatal("TurnDetection = false, want true")
	}
}

func TestResolveLiveOptionsReportsUnsupportedEndpointing(t *testing.T) {
	resolved := ResolveLiveOptions("google", "realtime.google.gemini-native-audio", LiveConfig{
		Options: provideropts.Values{
			provideropts.OptionEndpointingMs: 450,
		},
	}, nil, nil)

	if len(resolved.Effective.Unsupported) != 1 {
		t.Fatalf("Unsupported = %+v, want endpointing report", resolved.Effective.Unsupported)
	}
	if resolved.Effective.Unsupported[0].ID != provideropts.OptionEndpointingMs {
		t.Fatalf("Unsupported ID = %q", resolved.Effective.Unsupported[0].ID)
	}
}

func TestResolveLiveOptionsGlobalTurnDetectionFalseIsEffective(t *testing.T) {
	resolved := ResolveLiveOptions("google", "realtime.google.gemini-native-audio", LiveConfig{
		Options: provideropts.Values{
			provideropts.OptionTurnDetection: false,
		},
		Policies: LivePolicies{
			ActivityDetection: ActivityDetectionPolicy{
				Automatic:         true,
				SilenceDurationMs: 700,
			},
		},
	}, nil, nil)

	if resolved.TurnDetection {
		t.Fatal("TurnDetection = true, want false from global default")
	}
	if resolved.TurnDetectionSource != provideropts.SourceGlobalDefault {
		t.Fatalf("TurnDetectionSource = %q, want global", resolved.TurnDetectionSource)
	}
	if !resolved.HasTurnDetectionOverride() {
		t.Fatal("HasTurnDetectionOverride = false, want global setting to be applied to provider")
	}
}

func TestResolveLiveOptionsProviderTurnDetectionBeatsGlobalDefault(t *testing.T) {
	resolved := ResolveLiveOptions("openai", "realtime.openai.gpt-realtime-2", LiveConfig{
		Options: provideropts.Values{
			provideropts.OptionTurnDetection: true,
		},
		ProviderOptions: provideropts.Values{
			provideropts.OptionTurnDetection: false,
		},
	}, nil, nil)

	if resolved.TurnDetection {
		t.Fatal("TurnDetection = true, want provider override false")
	}
	if resolved.TurnDetectionSource != provideropts.SourceProviderOverride {
		t.Fatalf("TurnDetectionSource = %q, want provider override", resolved.TurnDetectionSource)
	}
}
