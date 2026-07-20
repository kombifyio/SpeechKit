// Package wakewordcatalog is the kernel-neutral source of truth for the wake
// phrases SpeechKit trains and serves, across BOTH engine families:
//
//   - openWakeWord (ONNX): consumed host-side by the desktop reference app and
//     the box companion. Trained via tools/wakeword-training (livekit-wakeword).
//   - microWakeWord (TFLite + JSON manifest): consumed ON-DEVICE by ESPHome
//     voice satellites and the Kombify-Box firmware (esp-tflite-micro).
//
// One phrase (e.g. "hey_kombify") is therefore describable once here and served
// to either family. The package is pure data + accessors: no HTTP, no config,
// no build tag, so it links into the kernel, the Linux server adapter, and
// tooling alike.
//
// microWakeWord artifacts carry an Available flag. It is false until the
// microWakeWord candidate pipeline (tools/wakeword-training/microwakeword) has
// produced a .tflite, all manifest params have independent measurements, the
// exact bytes have passed physical validation, and publication is verified;
// the ONNX artifacts are already trained and published.
package wakewordcatalog

import "strings"

// modelsBase is the public origin that serves the wake-word model bytes. The
// SpeechKit server serves them from its own volume (synced from Cloudflare R2 at
// startup) at /v1/wakeword/files/<name>, so no third-party host is involved.
// Kept identical to the desktop download catalog (internal/downloads/catalog.go)
// so both consume the same artifacts; the two are intentionally NOT code-coupled
// to keep this kernel package free of the desktop-only download machinery.
const modelsBase = "https://speechkit.kombify.io/v1/wakeword/files/"

// hfBase is retained as the build key so File.URL = hfBase + "<name>" stays a
// one-liner; it now points at the kombify-hosted serving origin above.
const hfBase = modelsBase

// License is the SPDX-ish identifier for every SpeechKit-trained wake model.
// All phrases are trained from scratch (see tools/wakeword-training/README.md)
// specifically so the result is permissively licensed for OSS + commercial use.
const License = "apache-2.0"

// microWakeWord manifest constants (ESPHome micro_wake_word v2 schema).
const (
	MicroWakeWordType            = "micro"
	MicroWakeWordManifestVersion = 2
	DefaultMinimumESPHomeVersion = "2024.7.0"
	// MicroWakeWordFeatureStepSize is fixed by the micro_wake_word streaming
	// frontend (10 ms slices); it is not a per-model tunable.
	MicroWakeWordFeatureStepSize = 10
)

// FileArtifact is a downloadable model file: a stable URL plus an integrity
// hash and size. SHA256 is lower-case hex of the raw bytes.
type FileArtifact struct {
	URL       string
	SHA256    string
	SizeBytes int64
}

// present reports whether the artifact points at a real published file.
func (f FileArtifact) present() bool { return strings.TrimSpace(f.URL) != "" }

// MicroWakeWordParams mirrors the "micro" object of an ESPHome microWakeWord v2
// model manifest. ProbabilityCutoff, SlidingWindowSize and TensorArenaSize are
// evidence-bound before publication; FeatureStepSize is fixed (see the const
// above). TensorArenaSize comes from the target runtime rather than the model
// trainer.
type MicroWakeWordParams struct {
	ProbabilityCutoff     float64
	SlidingWindowSize     int
	FeatureStepSize       int
	TensorArenaSize       int
	MinimumESPHomeVersion string
}

// MicroWakeWordArtifact describes the on-device TFLite model plus the manifest
// parameters ESPHome / esp-tflite-micro need to run it. Available is false
// until governed publication has been independently verified at the serving
// origin.
type MicroWakeWordArtifact struct {
	Available bool
	File      FileArtifact
	Params    MicroWakeWordParams
}

// OpenWakeWordArtifact is the per-phrase conv-attention ONNX head. The shared
// melspectrogram + embedding frontends every phrase depends on are SharedMelspec
// and SharedEmbedding below.
type OpenWakeWordArtifact struct {
	File                 FileArtifact
	RecommendedThreshold float64
}

// Model is one wake phrase available across both engine families.
type Model struct {
	ID               string   // stable slug, e.g. "hey_kombify"
	WakeWord         string   // spoken phrase, e.g. "Hey Kombify"
	DisplayName      string   // human label for dashboards
	Description      string   // short tuning/pronunciation note
	TrainedLanguages []string // BCP-47-ish language tags the model was trained on
	OpenWakeWord     OpenWakeWordArtifact
	MicroWakeWord    MicroWakeWordArtifact
}

// SharedMelspec and SharedEmbedding are the phrase-independent openWakeWord
// frontend models. Every openWakeWord phrase requires both, alongside its own
// per-phrase head. microWakeWord models do NOT use these (the TFLite model is
// self-contained).
var (
	SharedMelspec = FileArtifact{
		URL:       hfBase + "melspectrogram.onnx",
		SHA256:    "ba2b0e0f8b7b875369a2c89cb13360ff53bac436f2895cced9f479fa65eb176f",
		SizeBytes: 1_087_958,
	}
	SharedEmbedding = FileArtifact{
		URL:       hfBase + "embedding_model.onnx",
		SHA256:    "70d164290c1d095d1d4ee149bc5e00543250a7316b59f31d056cff7bd3075c1f",
		SizeBytes: 1_326_578,
	}
)

// models is the checked-in registry. openWakeWord data is real (trained
// 2026-05, published to hfBase). microWakeWord entries are scaffolded with
// Available=false: the .tflite + measured params are filled in when the
// microWakeWord dual-export training publishes them (Track C, part 1). Until
// then the model-serving endpoint reports the microWakeWord variant as pending
// and serves only the openWakeWord artifacts.
var models = []Model{
	{
		ID:               "hey_quby",
		WakeWord:         "Hey Quby",
		DisplayName:      "Hey Quby (Cubi / Kubi)",
		Description:      "SpeechKit brand default. Trained on both Cubi and Kubi pronunciations.",
		TrainedLanguages: []string{"en", "de"},
		OpenWakeWord: OpenWakeWordArtifact{
			File:                 FileArtifact{URL: hfBase + "hey_quby.onnx", SHA256: "d2219a70af63a12750b8d0a21fda38688b96841d21f564e57f99af7ba56951a6", SizeBytes: 164_607},
			RecommendedThreshold: 0.22,
		},
	},
	{
		ID:               "hey_computer",
		WakeWord:         "Hey Computer",
		DisplayName:      "Hey Computer",
		Description:      "Star Trek classic. Four syllables, very distinct phonemes — best-in-class FAR.",
		TrainedLanguages: []string{"en"},
		OpenWakeWord: OpenWakeWordArtifact{
			File:                 FileArtifact{URL: hfBase + "hey_computer.onnx", SHA256: "3acbd9ffff04beba2d16ebdfd0d4c734d65fecdd22446f25f4d0afa6e5d7606b", SizeBytes: 164_607},
			RecommendedThreshold: 0.10,
		},
	},
	{
		ID:               "hey_jarvis",
		WakeWord:         "Hey Jarvis",
		DisplayName:      "Hey Jarvis",
		Description:      "Marvel-popular wake phrase. Strong J/RV/S consonants.",
		TrainedLanguages: []string{"en"},
		OpenWakeWord: OpenWakeWordArtifact{
			File:                 FileArtifact{URL: hfBase + "hey_jarvis.onnx", SHA256: "7256019a18029c7bea33abc3344f7a3d0e07655cf4a5f18e65c9b6329eac3fb6", SizeBytes: 164_607},
			RecommendedThreshold: 0.45,
		},
	},
	{
		ID:               "hey_mira",
		WakeWord:         "Hey Mira",
		DisplayName:      "Hey Mira",
		Description:      "Short brand-style alternative. 3 syllables, equally natural in German and English.",
		TrainedLanguages: []string{"en", "de"},
		OpenWakeWord: OpenWakeWordArtifact{
			File:                 FileArtifact{URL: hfBase + "hey_mira.onnx", SHA256: "cb1f371f3a61dccc43c47bc79145f504b1e2e2ed1ab233a427494fb57378e794", SizeBytes: 164_607},
			RecommendedThreshold: 0.08,
		},
	},
	{
		ID:               "hey_kombify",
		WakeWord:         "Hey Kombify",
		DisplayName:      "Hey Kombify",
		Description:      "Organisation brand. 4 syllables, three distinct consonant onsets (K/B/F). English pronunciation only.",
		TrainedLanguages: []string{"en"},
		OpenWakeWord: OpenWakeWordArtifact{
			File:                 FileArtifact{URL: hfBase + "hey_kombify.onnx", SHA256: "24c6d2d1c235892362ebf12b0055801d2f8461f856e15d704c3d8262304f4c9f", SizeBytes: 164_607},
			RecommendedThreshold: 0.55,
		},
	},

	// ── single-word phrases (no "Hey") ─────────────────────────────────────
	// Per the Kombify-Box vision: ONE word, switchable Jarvis ⇄ Kombify, exactly
	// one active at a time.
	//
	// "kombify" ships a published single-word openWakeWord head (kombify.onnx,
	// trained 2026-07, threshold 0.50, 99.65% recall) for the host / companion
	// path. Its microWakeWord (.tflite) on-device variant for the ESP32 firmware
	// is still pending, so the model hub serves the openWakeWord artifact today
	// and answers `microwakeword_pending` for the manifest/tflite routes.
	//
	// "jarvis" has no single-word model yet (only the two-word "hey_jarvis"
	// above): it is registered so the hub knows the id and reports it pending
	// rather than 404. Publishing either variant requires evidence-bound params,
	// physical validation, publication approval, and serving-origin verification
	// before the artifact is filled and Available is set to true.
	{
		ID:               "kombify",
		WakeWord:         "Kombify",
		DisplayName:      "Kombify (single word)",
		Description:      "Single-word brand wake, no \"Hey\". Three distinct consonant onsets (K/B/F). openWakeWord head published; on-device microWakeWord variant pending.",
		TrainedLanguages: []string{"en"},
		OpenWakeWord: OpenWakeWordArtifact{
			File:                 FileArtifact{URL: hfBase + "kombify.onnx", SHA256: "1cf8e8d80f2c9515fbcfbd36e99d537eb8fb87132657c03b6a4e65f606db2769", SizeBytes: 99_190},
			RecommendedThreshold: 0.50,
		},
		MicroWakeWord: MicroWakeWordArtifact{
			Available: false, // flip true when tools/wakeword-training/microwakeword publishes kombify.tflite
		},
	},
	{
		ID:               "jarvis",
		WakeWord:         "Jarvis",
		DisplayName:      "Jarvis (single word)",
		Description:      "Single-word wake, no \"Hey\". Two syllables, strong J/RV/S onsets. No single-word model published yet (use \"hey_jarvis\" meanwhile).",
		TrainedLanguages: []string{"en"},
		MicroWakeWord: MicroWakeWordArtifact{
			Available: false, // flip true when tools/wakeword-training/microwakeword publishes jarvis.tflite
		},
	},
}

// All returns a copy of the model registry. Callers must not mutate the shared
// slice contents; the returned slice header is safe to range over.
func All() []Model {
	out := make([]Model, len(models))
	copy(out, models)
	return out
}

// ByID returns the model with the given slug (case-insensitive) and whether it
// was found.
func ByID(id string) (Model, bool) {
	want := strings.ToLower(strings.TrimSpace(id))
	for _, m := range models {
		if strings.ToLower(m.ID) == want {
			return m, true
		}
	}
	return Model{}, false
}

// HasOpenWakeWord reports whether the model ships a published openWakeWord head.
func (m Model) HasOpenWakeWord() bool { return m.OpenWakeWord.File.present() }

// HasMicroWakeWord reports whether a published microWakeWord TFLite exists for
// this phrase (Available AND a real file URL).
func (m Model) HasMicroWakeWord() bool {
	return m.MicroWakeWord.Available && m.MicroWakeWord.File.present()
}
