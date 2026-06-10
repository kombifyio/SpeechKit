package provideropts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
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

func TestDefaultManifestsIncludeDeepgramSTT(t *testing.T) {
	manifest, ok := FindManifest("deepgram", ModalitySTT)
	if !ok {
		t.Fatal("missing deepgram STT manifest")
	}
	support := manifest.SupportByID()
	for _, id := range []OptionID{OptionLanguage, OptionSmartFormat, OptionKeyterms, OptionEndpointingMs} {
		if support[id].Status != SupportNative {
			t.Fatalf("%s support = %#v, want native", id, support[id])
		}
	}
}

func TestProviderOptionMatrixMatchesDefaultManifests(t *testing.T) {
	path := filepath.Join("..", "..", "..", "docs", "capabilities", "provider-option-matrix.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read provider option matrix: %v", err)
	}

	var matrix struct {
		OptionIDs []OptionID `json:"option_ids"`
		Rows      []struct {
			Provider   string                     `json:"provider"`
			Modality   string                     `json:"modality"`
			ProfileIDs []string                   `json:"profile_ids"`
			Supports   map[OptionID]SupportStatus `json:"supports"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(data, &matrix); err != nil {
		t.Fatalf("decode provider option matrix: %v", err)
	}

	optionIDs := map[OptionID]bool{}
	for _, id := range matrix.OptionIDs {
		optionIDs[id] = true
	}
	rows := map[string]struct {
		ProfileIDs []string
		Supports   map[OptionID]SupportStatus
	}{}
	for _, row := range matrix.Rows {
		rows[matrixRowKey(row.Provider, row.Modality)] = struct {
			ProfileIDs []string
			Supports   map[OptionID]SupportStatus
		}{ProfileIDs: row.ProfileIDs, Supports: row.Supports}
	}

	for _, manifest := range DefaultManifests() {
		row, ok := rows[matrixRowKey(manifest.Provider, manifest.Modality)]
		if !ok {
			t.Fatalf("matrix missing %s %s", manifest.Provider, manifest.Modality)
		}
		if !reflect.DeepEqual(row.ProfileIDs, manifest.ProfileIDs) {
			t.Fatalf("%s %s profile_ids = %#v, want %#v", manifest.Provider, manifest.Modality, row.ProfileIDs, manifest.ProfileIDs)
		}
		if len(row.Supports) != len(manifest.Options) {
			t.Fatalf("%s %s support count = %d, want %d", manifest.Provider, manifest.Modality, len(row.Supports), len(manifest.Options))
		}
		for _, opt := range manifest.Options {
			if !optionIDs[opt.ID] {
				t.Fatalf("matrix option_ids missing %s", opt.ID)
			}
			if got, ok := row.Supports[opt.ID]; !ok || got != opt.Status {
				t.Fatalf("%s %s %s support = %q (present=%v), want %q", manifest.Provider, manifest.Modality, opt.ID, got, ok, opt.Status)
			}
		}
	}
}

func matrixRowKey(provider, modality string) string {
	return provider + "/" + modality
}
