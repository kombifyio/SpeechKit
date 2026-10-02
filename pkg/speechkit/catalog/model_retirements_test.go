package catalog

import (
	"strings"
	"testing"
)

// A retirement must land on a model that is itself current: a successor that
// is retired too would send a migrated config through a second, silent hop,
// and a cycle would never settle.
func TestRetiredModelsResolveToCurrentSuccessors(t *testing.T) {
	for _, row := range RetiredModels() {
		got, ok := ReplacementModelID(row.Provider, row.ModelID)
		if !ok || got == "" || got == row.ModelID {
			t.Fatalf("%s/%s resolves to %q (ok=%v)", row.Provider, row.ModelID, got, ok)
		}
		if _, retired := ReplacementModelID(row.Provider, got); retired {
			t.Fatalf("%s/%s resolves to %q, which is retired as well", row.Provider, row.ModelID, got)
		}
	}
}

// The catalog must never offer a model it retired: a profile, variant or
// registry row naming one would put the retired id back on the wire the
// moment a user selects it.
func TestCatalogOffersNoRetiredModel(t *testing.T) {
	retired := func(provider, modelID string) bool {
		_, ok := ReplacementModelID(provider, modelID)
		return ok
	}
	for _, profile := range DefaultProviderProfiles() {
		provider := ProviderIDForProfile(profile)
		if provider == "foundry" {
			// Foundry model ids are default deployment names, never migrated.
			continue
		}
		if retired(provider, profile.ModelID) {
			t.Errorf("profile %s offers retired model %s", profile.ID, profile.ModelID)
		}
		for _, variant := range profile.Variants {
			// TTS variants carry "<model>|<voice>".
			model, _, _ := strings.Cut(variant.ModelID, "|")
			if retired(provider, model) {
				t.Errorf("profile %s variant %s offers retired model %s", profile.ID, variant.ID, model)
			}
		}
	}
	for _, row := range DefaultModelRegistry() {
		if retired(row.Provider, row.ModelID) {
			t.Errorf("registry row %s/%s is retired", row.Provider, row.ModelID)
		}
	}
}
