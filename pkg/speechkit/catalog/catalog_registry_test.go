package catalog_test

import (
	"errors"
	"testing"

	speechkit "github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/catalog"
)

func customSTTProfile() speechkit.ProviderProfile {
	return speechkit.ProviderProfile{
		ID:            "stt.acme.whisper-turbo",
		Name:          "Acme Whisper Turbo",
		Mode:          speechkit.ModeDictation,
		ProviderKind:  speechkit.ProviderKindCloudProvider,
		ExecutionMode: speechkit.ExecutionModeSelfHostedHTTP,
		Capabilities:  []speechkit.Capability{speechkit.CapabilityTranscription, speechkit.CapabilitySTT},
	}
}

func TestCatalogWithExposesCustomProviderEverywhere(t *testing.T) {
	extended, err := catalog.DefaultCatalog().With(customSTTProfile())
	if err != nil {
		t.Fatalf("With: %v", err)
	}

	if _, ok := extended.Profile("stt.acme.whisper-turbo"); !ok {
		t.Fatal("registered profile is not resolvable by id")
	}

	if _, ok := catalog.DefaultCatalog().Profile("stt.acme.whisper-turbo"); ok {
		t.Fatal("With must not mutate the built-in catalog")
	}

	found := false
	for _, profile := range extended.ProfilesForMode(speechkit.ModeDictation) {
		if profile.ID == "stt.acme.whisper-turbo" {
			found = true
			if profile.Provider != "acme" {
				t.Fatalf("provider id = %q, want derived %q", profile.Provider, "acme")
			}
		}
	}
	if !found {
		t.Fatal("registered profile missing from ProfilesForMode(dictation)")
	}

	var row *catalog.ProviderMatrixRow
	for i := range extended.ProviderMatrix() {
		if extended.ProviderMatrix()[i].Provider == "acme" {
			r := extended.ProviderMatrix()[i]
			row = &r
		}
	}
	if row == nil {
		t.Fatal("registered provider missing from ProviderMatrix")
	}
	if row.DisplayName == "" {
		t.Fatal("matrix row has no display name")
	}

	hasDefault := false
	for _, def := range extended.ProviderDefaults() {
		if def.Provider == "acme" && def.Mode == speechkit.ModeDictation {
			hasDefault = true
		}
	}
	if !hasDefault {
		t.Fatal("registered provider has no dictation default")
	}

	allowed := extended.Filter(speechkit.RuntimePolicy{AllowedProfiles: []string{"stt.acme.whisper-turbo"}})
	if len(allowed) != 1 || allowed[0].ID != "stt.acme.whisper-turbo" {
		t.Fatalf("policy filter on registered profile returned %d profiles", len(allowed))
	}
}

func TestCatalogRejectsInvalidAndDuplicateProfiles(t *testing.T) {
	invalid := customSTTProfile()
	invalid.Capabilities = []speechkit.Capability{speechkit.CapabilityTTS}
	if _, err := catalog.DefaultCatalog().With(invalid); !errors.Is(err, catalog.ErrInvalidProfile) {
		t.Fatalf("mode-contract violation: got %v, want ErrInvalidProfile", err)
	}

	duplicate := customSTTProfile()
	duplicate.ID = catalog.DefaultProviderProfiles()[0].ID
	if _, err := catalog.DefaultCatalog().With(duplicate); !errors.Is(err, catalog.ErrDuplicateProfileID) {
		t.Fatalf("duplicate id: got %v, want ErrDuplicateProfileID", err)
	}

	if _, err := catalog.NewCatalog(customSTTProfile(), customSTTProfile()); !errors.Is(err, catalog.ErrDuplicateProfileID) {
		t.Fatalf("NewCatalog duplicate: got %v, want ErrDuplicateProfileID", err)
	}
}
