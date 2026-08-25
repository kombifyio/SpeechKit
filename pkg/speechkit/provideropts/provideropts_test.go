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
	for _, id := range []OptionID{OptionSmartFormat, OptionKeyterms, OptionEndpointingMs} {
		if support[id].Status != SupportNative {
			t.Fatalf("%s support = %#v, want native", id, support[id])
		}
	}
	// language must be declared: omitting it made the resolver discard every
	// configured Deepgram language as unsupported, so the setting was inert.
	if support[OptionLanguage].Status != SupportNative {
		t.Fatalf("language support = %#v, want native", support[OptionLanguage])
	}
	if opt, ok := support[OptionDetectLanguage]; !ok || opt.Status != SupportUnsupported {
		t.Fatalf("detect_language support = %#v (present=%v), want unsupported", opt, ok)
	}
}

// Provider identifiers are the lookup key the STT adapters hand to the
// resolver. A provider without a manifest falls back to the empty manifest,
// which marks every option unsupported: the Settings form then never offers the
// provider's options and the vocabulary-bias router skips it entirely.
func TestSTTManifestsCoverEveryAdapterProviderID(t *testing.T) {
	for _, provider := range []string{
		"deepgram", "openai", "groq", "google", "assemblyai",
		"openrouter", "huggingface", "local", "ollama", "vps",
	} {
		if _, ok := FindManifest(provider, ModalitySTT); !ok {
			t.Errorf("missing %s STT manifest", provider)
		}
	}
}

func TestTTSManifestsCoverHuggingFaceAndPiper(t *testing.T) {
	for _, provider := range []string{"huggingface", "piper"} {
		manifest, ok := FindManifest(provider, ModalityTTS)
		if !ok {
			t.Fatalf("missing %s TTS manifest", provider)
		}
		support := manifest.SupportByID()
		if support[OptionVoice].Status == SupportUnsupported {
			t.Fatalf("%s voice must not be unsupported — Settings would drop voice overrides", provider)
		}
		if support[OptionLanguage].Status == SupportUnsupported {
			t.Fatalf("%s language must not be unsupported — Settings would drop locale overrides", provider)
		}
	}
}

func TestOpenAICompatibleSTTManifestsMatchAdapterFields(t *testing.T) {
	for _, provider := range []string{"openai", "groq", "ollama", "vps"} {
		manifest, ok := FindManifest(provider, ModalitySTT)
		if !ok {
			t.Fatalf("missing %s STT manifest", provider)
		}
		support := manifest.SupportByID()
		for id, wantKey := range map[OptionID]string{
			OptionLanguage:   "language",
			OptionPromptHint: "prompt",
		} {
			if support[id].Status != SupportNative || support[id].NativeKey != wantKey {
				t.Errorf("%s %s support = %q/%q, want native/%q", provider, id, support[id].Status, support[id].NativeKey, wantKey)
			}
		}
		// The adapter posts file/language/model/prompt and decodes only the
		// transcript text, so nothing else can be honored.
		for _, id := range []OptionID{OptionTimestamps, OptionPunctuation, OptionSmartFormat, OptionKeyterms, OptionSpeakerDiarization} {
			opt, ok := support[id]
			if !ok || opt.Status != SupportUnsupported {
				t.Errorf("%s %s support = %#v (declared=%v), want unsupported", provider, id, opt, ok)
			}
		}
	}
}

func TestLocalSTTManifestDeclaresNativeRequestFields(t *testing.T) {
	manifest, ok := FindManifest("local", ModalitySTT)
	if !ok {
		t.Fatal("missing local STT manifest")
	}
	support := manifest.SupportByID()
	// The bundled whisper-server receives both as their own multipart fields.
	if support[OptionLanguage].Status != SupportNative || support[OptionLanguage].NativeKey != "language" {
		t.Errorf("language support = %#v, want native/language", support[OptionLanguage])
	}
	if support[OptionPromptHint].Status != SupportNative || support[OptionPromptHint].NativeKey != "prompt" {
		t.Errorf("prompt_hint support = %#v, want native/prompt", support[OptionPromptHint])
	}
	if support[OptionVocabularyBias].Status != SupportDerived {
		t.Errorf("vocabulary_bias support = %#v, want derived", support[OptionVocabularyBias])
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
