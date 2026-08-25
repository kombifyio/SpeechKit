package speechkit

import (
	"encoding/json"
	"testing"
)

func TestProviderModelDescriptorFreshnessFieldsRoundTrip(t *testing.T) {
	row := ProviderModelDescriptor{
		Provider:             "deepgram",
		ModelID:              ModelDeepgramNova3,
		Mode:                 ModeDictation,
		Name:                 "Nova-3",
		Lifecycle:            ModelLifecycleGA,
		Default:              true,
		SourceURL:            "https://developers.deepgram.com/docs/models-languages-overview",
		ReleasedAt:           "2024-01-01",
		LastVerifiedAt:       "2026-08-25",
		MultilanguageCapable: true,
	}
	data, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got ProviderModelDescriptor
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.LastVerifiedAt != "2026-08-25" || !got.MultilanguageCapable {
		t.Fatalf("got = %#v", got)
	}
}

func TestMissingFreshnessReportsDefaultRows(t *testing.T) {
	missing := MissingFreshnessReports(DefaultModelRegistry())
	if len(missing) == 0 {
		t.Fatal("default registry is not yet vendor-dated; MissingFreshnessReports must be non-empty until glnc populate lands")
	}
	populated := MissingFreshnessReports([]ProviderModelDescriptor{{
		Provider:       "deepgram",
		ModelID:        ModelDeepgramNova3,
		Default:        true,
		LastVerifiedAt: "2026-08-25",
	}})
	if len(populated) != 0 {
		t.Fatalf("populated row still reported missing: %v", populated)
	}
}
