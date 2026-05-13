package profiles

import (
	"testing"

	"github.com/kombifyio/SpeechKit/internal/config"
	desktopruntime "github.com/kombifyio/SpeechKit/internal/desktop/runtime"
	"github.com/kombifyio/SpeechKit/internal/models"
)

func TestModalityForModeMapsThreeModes(t *testing.T) {
	cases := map[string]models.Modality{
		desktopruntime.ModeDictate:    models.ModalitySTT,
		desktopruntime.ModeVoiceAgent: models.ModalityRealtimeVoice,
		desktopruntime.ModeAssist:     models.ModalityAssist,
		"":                            models.ModalityAssist, // default
		"unknown":                     models.ModalityAssist, // default
	}
	for mode, want := range cases {
		if got := ModalityForMode(mode); got != want {
			t.Errorf("ModalityForMode(%q) = %v, want %v", mode, got, want)
		}
	}
}

func TestNormalizeModeSelectionDropsDuplicateFallback(t *testing.T) {
	sel := NormalizeModeSelection(config.ModeModelSelection{
		PrimaryProfileID:  "  alpha  ",
		FallbackProfileID: "alpha",
	})
	if sel.PrimaryProfileID != "alpha" {
		t.Errorf("PrimaryProfileID = %q, want %q", sel.PrimaryProfileID, "alpha")
	}
	if sel.FallbackProfileID != "" {
		t.Errorf("FallbackProfileID = %q, want empty (duplicate of primary)", sel.FallbackProfileID)
	}
}

func TestFindCatalogProfileEmptyIDReturnsFalse(t *testing.T) {
	_, ok := FindCatalogProfile(models.Catalog{}, "")
	if ok {
		t.Fatal("FindCatalogProfile(_, \"\") returned true, want false")
	}
}

func TestFindCatalogProfileLocatesByID(t *testing.T) {
	catalog := models.Catalog{Profiles: []models.Profile{
		{ID: "a"}, {ID: "b"}, {ID: "c"},
	}}
	profile, ok := FindCatalogProfile(catalog, "b")
	if !ok || profile.ID != "b" {
		t.Fatalf("FindCatalogProfile = (%v, %v), want ({b}, true)", profile, ok)
	}
}

func TestModeSelectionForModeReturnsZeroForNilCfg(t *testing.T) {
	if got := ModeSelectionForMode(nil, desktopruntime.ModeAssist); got != (config.ModeModelSelection{}) {
		t.Errorf("ModeSelectionForMode(nil, _) = %v, want zero", got)
	}
}
