package catalog

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

func TestProviderModelDescriptorFreshnessFieldsRoundTrip(t *testing.T) {
	row := ProviderModelDescriptor{
		Provider:             "deepgram",
		ModelID:              ModelDeepgramNova3,
		Mode:                 speechkit.ModeDictation,
		Name:                 "Nova-3",
		Lifecycle:            speechkit.ModelLifecycleGA,
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

// ModelFreshnessGateEnv names the environment variable that turns the
// wall-clock freshness SLA into a failing test. The scheduled
// model-freshness-gate workflow sets it; the default `go test ./...` run does
// not, so the unit suite stays deterministic regardless of the calendar.
const ModelFreshnessGateEnv = "SPEECHKIT_MODEL_FRESHNESS_GATE"

func TestDefaultModelRegistryFreshnessSLA(t *testing.T) {
	rows := DefaultModelRegistry()
	if missing := MissingFreshnessReports(rows); len(missing) > 0 {
		t.Fatalf("default/recommended rows missing LastVerifiedAt: %v", missing)
	}
	stale := StaleFreshnessReports(rows, time.Now().UTC())
	if len(stale) == 0 {
		return
	}
	if os.Getenv(ModelFreshnessGateEnv) == "" {
		t.Logf("default/recommended rows older than ModelFreshnessSLA (set %s=1 to fail): %v", ModelFreshnessGateEnv, stale)
		return
	}
	t.Fatalf("default/recommended rows older than ModelFreshnessSLA: %v", stale)
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
