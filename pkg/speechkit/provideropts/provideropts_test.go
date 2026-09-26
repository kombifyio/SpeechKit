package provideropts

import (
	"testing"
)

func TestResolvePrecedenceAndUnsupportedReports(t *testing.T) {
	manifest := manifest("test", "Test", ModalitySTT, nil, []OptionSupport{
		native(OptionLanguage, TypeString, "Language", "language", "https://example.com"),
		unsupported(OptionEndpointingMs, TypeInt, "Endpointing", "not available"),
	})

	resolved := Resolve(ResolveInput{
		Manifest: manifest,
		ProviderDefaults: Values{
			OptionLanguage: "de",
		},
		GlobalDefaults: Values{
			OptionLanguage:      "en",
			OptionEndpointingMs: 250,
		},
		ProviderOverrides: Values{
			OptionLanguage: "fr",
		},
		RequestOverrides: Values{
			OptionLanguage: "it",
		},
	})

	if got := resolved.String(OptionLanguage); got != "it" {
		t.Fatalf("language = %q, want request override", got)
	}
	if got := resolved.Options[OptionLanguage].Source; got != SourceRequestOverride {
		t.Fatalf("language source = %q", got)
	}
	if len(resolved.Unsupported) != 1 || resolved.Unsupported[0].ID != OptionEndpointingMs {
		t.Fatalf("unsupported = %#v, want endpointing report", resolved.Unsupported)
	}
}

// Without a manifest the resolver still carries the value through but reports
// it as unsupported, which is what silently disqualifies the option upstream.
func TestResolveWithoutManifestReportsConfiguredOptionsUnsupported(t *testing.T) {
	resolved := Resolve(ResolveInput{
		Manifest:       ProviderOptionManifest{Provider: "unmapped", Modality: ModalitySTT},
		GlobalDefaults: Values{OptionLanguage: "de"},
	})
	if got := resolved.String(OptionLanguage); got != "de" {
		t.Fatalf("language = %q, want de", got)
	}
	if len(resolved.Unsupported) != 1 || resolved.Unsupported[0].ID != OptionLanguage {
		t.Fatalf("unsupported = %#v, want a language report", resolved.Unsupported)
	}
	if resolved.Options[OptionLanguage].Support.Status != SupportUnsupported {
		t.Fatalf("language support = %#v, want unsupported", resolved.Options[OptionLanguage].Support)
	}
}

// Every provider must state where it stands on retention. A missing entry
// would let a retention policy silently assume the safe answer for a vendor
// that keeps the audio.
func TestEveryManifestDeclaresANoStorePosition(t *testing.T) {
	for _, manifest := range DefaultManifests() {
		var found *OptionSupport
		for index := range manifest.Options {
			if manifest.Options[index].ID == OptionNoStore {
				found = &manifest.Options[index]
				break
			}
		}
		if found == nil {
			t.Fatalf("%s/%s declares no position on %s", manifest.Provider, manifest.Modality, OptionNoStore)
		}
		if found.Notes == "" && found.EvidenceURL == "" {
			t.Errorf("%s/%s claims %q for no-store without evidence or a note",
				manifest.Provider, manifest.Modality, found.Status)
		}
		if found.Status == SupportNative && found.NativeKey == "" {
			t.Errorf("%s/%s claims native no-store without naming the request field",
				manifest.Provider, manifest.Modality)
		}
	}
}
