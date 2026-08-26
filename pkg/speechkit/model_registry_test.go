package speechkit

import (
	"encoding/json"
	"testing"
	"time"
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

func TestDefaultModelRegistryFreshnessSLA(t *testing.T) {
	rows := DefaultModelRegistry()
	if missing := MissingFreshnessReports(rows); len(missing) > 0 {
		t.Fatalf("default/recommended rows missing LastVerifiedAt: %v", missing)
	}
	if stale := StaleFreshnessReports(rows, time.Now().UTC()); len(stale) > 0 {
		t.Fatalf("default/recommended rows older than ModelFreshnessSLA: %v", stale)
	}
}

func TestStaleFreshnessReportsUsesVerifiedDay(t *testing.T) {
	now := time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC)
	old := ProviderModelDescriptor{
		Provider:       "deepgram",
		ModelID:        ModelDeepgramNova3,
		Default:        true,
		LastVerifiedAt: "2026-01-01",
	}
	if got := StaleFreshnessReports([]ProviderModelDescriptor{old}, now); len(got) == 0 {
		t.Fatal("expected a January verification to be stale in August")
	}
	fresh := old
	fresh.LastVerifiedAt = "2026-08-24"
	if got := StaleFreshnessReports([]ProviderModelDescriptor{fresh}, now); len(got) != 0 {
		t.Fatalf("day-old verification reported stale: %v", got)
	}
}
