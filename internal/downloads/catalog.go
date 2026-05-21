package downloads

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/localllm"
	"github.com/kombifyio/SpeechKit/internal/netsec"
	"github.com/kombifyio/SpeechKit/internal/stt"
)

// OllamaBaseURL is the Ollama REST API endpoint. Override in tests.
var OllamaBaseURL = "http://localhost:11434"

// ollamaValidation permits loopback + http because Ollama runs on localhost.
var ollamaValidation = netsec.ValidationOptions{AllowLoopback: true, AllowHTTP: true}

var executablePath = os.Executable

// ollamaClient is a shared safe HTTP client with short timeout for catalog checks.
var ollamaClient = netsec.NewSafeHTTPClient(netsec.ClientOptions{Timeout: 2 * time.Second, DialValidation: &ollamaValidation})

// StatusOptions controls which runtime checks are layered onto the static
// artifact catalog.
type StatusOptions struct {
	ProbeRuntimes bool
	ProbeOllama   bool
}

var (
	DefaultStatusOptions   = StatusOptions{ProbeRuntimes: true, ProbeOllama: true}
	ReadinessStatusOptions = StatusOptions{ProbeRuntimes: true, ProbeOllama: false}
)

// ArtifactCatalog returns static downloadable and pullable model artifacts.
// Runtime state is resolved separately so catalog consumers can decide which
// probes are appropriate for their context.
func ArtifactCatalog() []Item {
	return []Item{
		{
			ID:          "whisper.ggml-small",
			ProfileID:   "stt.local.whispercpp",
			Name:        "Whisper Small Multilingual (466 MB)",
			Description: "Lightweight fallback local model with good multilingual quality and the smallest download size.",
			SizeLabel:   "466 MB",
			SizeBytes:   484_264_096,
			Kind:        KindHTTP,
			URL:         "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-small.bin",
			SHA256:      "1be3a9b2063867b937e64e2ec7483364a79917e157fa98c5d94b5c1fffea987b",
			License:     "mit",
		},
		{
			ID:          "whisper.ggml-large-v3-turbo",
			ProfileID:   "stt.local.whispercpp",
			Name:        "Whisper Large v3 Turbo",
			Description: "Recommended local Whisper.cpp model with a much better accuracy-speed balance than Small while staying lighter than full Large v3.",
			SizeLabel:   "~1.6 GB",
			SizeBytes:   1_624_555_275,
			Kind:        KindHTTP,
			URL:         "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-large-v3-turbo.bin",
			SHA256:      "1fc70f774d38eb169993ac391eea357ef47c88757ef72ee5943879b7e8e2bc69",
			License:     "mit",
			Recommended: true,
		},
		{
			ID:          "whisper.ggml-large-v3",
			ProfileID:   "stt.local.whispercpp",
			Name:        "Whisper Large v3 (Open Source)",
			Description: "Larger open-source Whisper.cpp model when you want the strongest local transcription quality.",
			SizeLabel:   "~3.1 GB",
			SizeBytes:   3_100_000_000,
			Kind:        KindHTTP,
			URL:         "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-large-v3.bin",
			SHA256:      "64d18257a82c05de9a8e4953fa0e3cdcc1f0822fca32c257fca5a4e1e06d8e2d",
			License:     "mit",
		},
		{
			ID:          "llamacpp.gemma-4-e4b-it-q4-k-m",
			ProfileID:   "assist.builtin.gemma4-e4b",
			Name:        "Gemma 4 E4B IT Q4_K_M (GGUF)",
			Description: "Recommended balanced Gemma 4 local Assist model for SpeechKit's bundled llama.cpp runtime.",
			SizeLabel:   "~5.3 GB",
			SizeBytes:   5_335_289_824,
			Kind:        KindHTTP,
			URL:         "https://huggingface.co/ggml-org/gemma-4-E4B-it-GGUF/resolve/main/gemma-4-E4B-it-Q4_K_M.gguf",
			SHA256:      "90ce98129eb3e8cc57e62433d500c97c624b1e3af1fcc85dd3b55ad7e0313e9f",
			License:     "gemma",
			Recommended: true,
		},
		{
			ID:          "llamacpp.gemma-4-e2b-it-q8-0",
			ProfileID:   "assist.builtin.gemma4-e4b",
			Name:        "Gemma 4 E2B IT Q8_0 (GGUF)",
			Description: "Smaller Gemma 4 local Assist model for lighter devices using the official ggml-org Q8_0 build.",
			SizeLabel:   "~5.0 GB",
			SizeBytes:   4_967_494_592,
			Kind:        KindHTTP,
			URL:         "https://huggingface.co/ggml-org/gemma-4-E2B-it-GGUF/resolve/main/gemma-4-E2B-it-Q8_0.gguf",
			SHA256:      "e049411c01fb7a81161768c52e38828970e55a64e22738957adcbe51d20f1c8e",
			License:     "gemma",
		},
		{
			ID:          "llamacpp.gemma-4-e4b-it-q4-k-m-voice",
			ProfileID:   "realtime.builtin.pipeline",
			Name:        "Gemma 4 E4B IT Q4_K_M - Voice Agent (GGUF)",
			Description: "Recommended balanced Gemma 4 local model for the Voice Agent pipeline fallback with SpeechKit's bundled llama.cpp runtime.",
			SizeLabel:   "~5.3 GB",
			SizeBytes:   5_335_289_824,
			Kind:        KindHTTP,
			URL:         "https://huggingface.co/ggml-org/gemma-4-E4B-it-GGUF/resolve/main/gemma-4-E4B-it-Q4_K_M.gguf",
			SHA256:      "90ce98129eb3e8cc57e62433d500c97c624b1e3af1fcc85dd3b55ad7e0313e9f",
			License:     "gemma",
			Recommended: true,
		},
		{
			ID:          "llamacpp.gemma-4-e2b-it-q8-0-voice",
			ProfileID:   "realtime.builtin.pipeline",
			Name:        "Gemma 4 E2B IT Q8_0 - Voice Agent (GGUF)",
			Description: "Smaller Gemma 4 local model for the Voice Agent pipeline fallback using the official ggml-org Q8_0 build.",
			SizeLabel:   "~5.0 GB",
			SizeBytes:   4_967_494_592,
			Kind:        KindHTTP,
			URL:         "https://huggingface.co/ggml-org/gemma-4-E2B-it-GGUF/resolve/main/gemma-4-E2B-it-Q8_0.gguf",
			SHA256:      "e049411c01fb7a81161768c52e38828970e55a64e22738957adcbe51d20f1c8e",
			License:     "gemma",
		},
		{
			ID:          "ollama.gemma4-e4b-dictate",
			ProfileID:   "stt.ollama.gemma4-e4b-transcribe",
			Name:        "Gemma 4 E4B - Dictation (Ollama, 3.3 GB)",
			Description: "Ollama-managed local provider model exposed through SpeechKit's Dictation transcription adapter.",
			SizeLabel:   "~3.3 GB",
			SizeBytes:   3_300_000_000,
			Kind:        KindOllama,
			OllamaModel: "gemma4:e4b",
			License:     "gemma",
		},
		{
			ID:          "ollama.gemma4-e4b",
			ProfileID:   "utility.ollama.gemma4-e4b",
			Name:        "Gemma 4 E4B - Utility (Ollama, 3.3 GB)",
			Description: "Ollama-managed local provider model for utility routing and quick actions.",
			SizeLabel:   "~3.3 GB",
			SizeBytes:   3_300_000_000,
			Kind:        KindOllama,
			OllamaModel: "gemma4:e4b",
			License:     "gemma",
			Recommended: true,
		},
		{
			ID:          "ollama.gemma4-e4b-assist",
			ProfileID:   "assist.ollama.gemma4-e4b",
			Name:        "Gemma 4 E4B - Assist (Ollama, 3.3 GB)",
			Description: "Ollama-managed local provider model for Assist reasoning and follow-ups.",
			SizeLabel:   "~3.3 GB",
			SizeBytes:   3_300_000_000,
			Kind:        KindOllama,
			OllamaModel: "gemma4:e4b",
			License:     "gemma",
		},
		{
			ID:          "ollama.gemma4-e4b-voice",
			ProfileID:   "realtime.ollama.gemma4-e4b-pipeline",
			Name:        "Gemma 4 E4B - Voice Agent (Ollama, 3.3 GB)",
			Description: "Ollama-managed local provider model for the Voice Agent pipeline fallback and session summaries.",
			SizeLabel:   "~3.3 GB",
			SizeBytes:   3_300_000_000,
			Kind:        KindOllama,
			OllamaModel: "gemma4:e4b",
			License:     "gemma",
		},

		// Wake-word models. The two "shared" entries (melspectrogram +
		// embedding) are mandatory whenever wake-word is enabled; the
		// phrase entries are user-selected (one of five). ProfileID
		// "wakeword.shared" groups the always-needed files; phrase
		// models use "wakeword.phrase.<id>" so the dashboard can render
		// them as a "pick one" group. Trained 2026-05 via
		// livekit-wakeword. All Apache-2.0. See
		// docs/wakeword.md + tools/wakeword-training/README.md.
		{
			ID:          "wakeword.shared.melspec",
			ProfileID:   "wakeword.shared",
			Name:        "Wake-word: melspectrogram model (shared)",
			Description: "openWakeWord-compatible melspectrogram frontend. Required by every wake phrase.",
			SizeLabel:   "~1.1 MB",
			SizeBytes:   1_087_958,
			Kind:        KindHTTP,
			URL:         "https://huggingface.co/Soulcreek2/speechkit-wakeword-models/resolve/main/melspectrogram.onnx",
			SHA256:      "ba2b0e0f8b7b875369a2c89cb13360ff53bac436f2895cced9f479fa65eb176f",
			License:     "apache",
			Recommended: true,
		},
		{
			ID:          "wakeword.shared.embedding",
			ProfileID:   "wakeword.shared",
			Name:        "Wake-word: embedding model (shared)",
			Description: "openWakeWord-compatible embedding network. Required by every wake phrase.",
			SizeLabel:   "~1.3 MB",
			SizeBytes:   1_326_578,
			Kind:        KindHTTP,
			URL:         "https://huggingface.co/Soulcreek2/speechkit-wakeword-models/resolve/main/embedding_model.onnx",
			SHA256:      "70d164290c1d095d1d4ee149bc5e00543250a7316b59f31d056cff7bd3075c1f",
			License:     "apache",
			Recommended: true,
		},
		{
			ID:          "wakeword.phrase.hey_quby",
			ProfileID:   "wakeword.phrase",
			Name:        "Wake-phrase: Hey Quby (Cubi / Kubi)",
			Description: "SpeechKit brand default. Trained on both Cubi and Kubi pronunciations. Recommended threshold 0.22, measured FPPH 0.06/h.",
			SizeLabel:   "~165 KB",
			SizeBytes:   164_607,
			Kind:        KindHTTP,
			URL:         "https://huggingface.co/Soulcreek2/speechkit-wakeword-models/resolve/main/hey_quby.onnx",
			SHA256:      "d2219a70af63a12750b8d0a21fda38688b96841d21f564e57f99af7ba56951a6",
			License:     "apache",
			Recommended: true,
		},
		{
			ID:          "wakeword.phrase.hey_computer",
			ProfileID:   "wakeword.phrase",
			Name:        "Wake-phrase: Hey Computer",
			Description: "Star Trek classic. Four syllables, very distinct phonemes — best-in-class FAR. Recommended threshold 0.10, measured FPPH 0.00/h.",
			SizeLabel:   "~165 KB",
			SizeBytes:   164_607,
			Kind:        KindHTTP,
			URL:         "https://huggingface.co/Soulcreek2/speechkit-wakeword-models/resolve/main/hey_computer.onnx",
			SHA256:      "3acbd9ffff04beba2d16ebdfd0d4c734d65fecdd22446f25f4d0afa6e5d7606b",
			License:     "apache",
		},
		{
			ID:          "wakeword.phrase.hey_jarvis",
			ProfileID:   "wakeword.phrase",
			Name:        "Wake-phrase: Hey Jarvis",
			Description: "Marvel-popular wake phrase. Strong J/RV/S consonants. Recommended threshold 0.45, measured FPPH 0.06/h.",
			SizeLabel:   "~165 KB",
			SizeBytes:   164_607,
			Kind:        KindHTTP,
			URL:         "https://huggingface.co/Soulcreek2/speechkit-wakeword-models/resolve/main/hey_jarvis.onnx",
			SHA256:      "7256019a18029c7bea33abc3344f7a3d0e07655cf4a5f18e65c9b6329eac3fb6",
			License:     "apache",
		},
		{
			ID:          "wakeword.phrase.hey_mira",
			ProfileID:   "wakeword.phrase",
			Name:        "Wake-phrase: Hey Mira",
			Description: "Short brand-style alternative. 3 syllables, equally natural in German and English. Recommended threshold 0.08, measured FPPH 0.11/h.",
			SizeLabel:   "~165 KB",
			SizeBytes:   164_607,
			Kind:        KindHTTP,
			URL:         "https://huggingface.co/Soulcreek2/speechkit-wakeword-models/resolve/main/hey_mira.onnx",
			SHA256:      "cb1f371f3a61dccc43c47bc79145f504b1e2e2ed1ab233a427494fb57378e794",
			License:     "apache",
		},
		{
			ID:          "wakeword.phrase.hey_kombify",
			ProfileID:   "wakeword.phrase",
			Name:        "Wake-phrase: Hey Kombify",
			Description: "Organisation brand. 4 syllables, three distinct consonant onsets (K/B/F). English pronunciation only. Recommended threshold 0.55, measured FPPH 0.00/h.",
			SizeLabel:   "~165 KB",
			SizeBytes:   164_607,
			Kind:        KindHTTP,
			URL:         "https://huggingface.co/Soulcreek2/speechkit-wakeword-models/resolve/main/hey_kombify.onnx",
			SHA256:      "24c6d2d1c235892362ebf12b0055801d2f8461f856e15d704c3d8262304f4c9f",
			License:     "apache",
		},
	}
}

// Catalog returns all artifacts with the default UI-level status probes.
func Catalog(ctx context.Context, cfg *config.Config) []Item {
	return CatalogWithStatus(ctx, cfg, DefaultStatusOptions)
}

func CatalogWithStatus(ctx context.Context, cfg *config.Config, options StatusOptions) []Item {
	items := ArtifactCatalog()
	ApplyStatus(ctx, cfg, items, options)
	return items
}

func ApplyStatus(ctx context.Context, cfg *config.Config, items []Item, options StatusOptions) {
	whisperRuntimeReady := false
	whisperRuntimeProblem := ""
	whisperRuntimeChecked := false
	localLLMRuntimeReady := false
	localLLMRuntimeProblem := ""
	localLLMRuntimeChecked := false

	for i := range items {
		item := &items[i]
		switch item.Kind {
		case KindHTTP:
			filename := filepath.Base(item.URL)
			if filename == "" || filename == "." {
				continue
			}
			item.Available = modelFileAvailable(*item, cfg)
			item.Selected = selectedArtifactModel(cfg, item.ProfileID) == filename
			if options.ProbeRuntimes && item.ProfileID == "stt.local.whispercpp" {
				if !whisperRuntimeChecked {
					whisperRuntimeReady, whisperRuntimeProblem = whisperRuntimeAvailability()
					whisperRuntimeChecked = true
				}
				item.RuntimeReady = whisperRuntimeReady
				item.RuntimeProblem = whisperRuntimeProblem
			} else if options.ProbeRuntimes && localLLMRuntimeProfile(item.ProfileID) {
				if !localLLMRuntimeChecked {
					localLLMRuntimeReady, localLLMRuntimeProblem = localLLMRuntimeAvailability(ctx, cfg)
					localLLMRuntimeChecked = true
				}
				item.RuntimeReady = localLLMRuntimeReady
				item.RuntimeProblem = localLLMRuntimeProblem
			}
		case KindOllama:
			if options.ProbeOllama && item.OllamaModel != "" {
				item.Available = OllamaModelPresent(ctx, item.OllamaModel)
			}
		default:
		}
	}
}

func modelFileAvailable(item Item, cfg *config.Config) bool {
	_, ok := AvailableArtifactModelPath(item, cfg)
	return ok
}

// AvailableArtifactModelPath returns the first on-disk file backing a catalog
// item. For whisper.cpp this includes the bundled starter model in addition to
// the user-writable download directory.
func AvailableArtifactModelPath(item Item, cfg *config.Config) (string, bool) {
	filename := filepath.Base(item.URL)
	for _, path := range artifactModelPaths(item, cfg, filename) {
		if FileIsPresent(path) {
			return path, true
		}
	}
	return "", false
}

func artifactModelPaths(item Item, cfg *config.Config, filename string) []string {
	if strings.TrimSpace(filename) == "" || filename == "." {
		return nil
	}
	paths := []string{filepath.Join(artifactModelDir(item, cfg), filename)}
	if item.ProfileID == "stt.local.whispercpp" {
		if cfg != nil {
			if modelPath := strings.TrimSpace(cfg.Local.ModelPath); modelPath != "" && filepath.Base(modelPath) == filename {
				paths = append(paths, modelPath)
			}
		}
		if bundled := bundledWhisperModelPath(filename); bundled != "" {
			paths = append(paths, bundled)
		}
	} else if wakewordArtifactProfile(item.ProfileID) {
		if bundled := bundledWakewordModelPath(filename); bundled != "" {
			paths = append(paths, bundled)
		}
	}
	return dedupePaths(paths)
}

func bundledWhisperModelPath(filename string) string {
	if strings.TrimSpace(filename) == "" || filename == "." {
		return ""
	}
	exe, err := executablePath()
	if err != nil || strings.TrimSpace(exe) == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(exe), "models", filename)
}

func bundledWakewordModelPath(filename string) string {
	if strings.TrimSpace(filename) == "" || filename == "." {
		return ""
	}
	exe, err := executablePath()
	if err != nil || strings.TrimSpace(exe) == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(exe), "models", "wakeword", filename)
}

func dedupePaths(paths []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		clean := filepath.Clean(strings.TrimSpace(path))
		if clean == "." || seen[clean] {
			continue
		}
		seen[clean] = true
		out = append(out, clean)
	}
	return out
}

func artifactModelDir(item Item, cfg *config.Config) string {
	if item.ProfileID == "stt.local.whispercpp" {
		return ResolveWhisperModelsDir(cfg)
	}
	if wakewordArtifactProfile(item.ProfileID) {
		return ResolveWakewordModelsDir(cfg)
	}
	return ResolveLocalLLMModelsDir(cfg)
}

func selectedArtifactModel(cfg *config.Config, profileID string) string {
	if profileID == "stt.local.whispercpp" {
		return selectedWhisperModel(cfg)
	}
	if wakewordArtifactProfile(profileID) {
		return selectedWakewordModel(cfg, profileID)
	}
	return selectedLocalLLMModelForProfile(cfg, profileID)
}

func wakewordArtifactProfile(profileID string) bool {
	return strings.HasPrefix(strings.TrimSpace(profileID), "wakeword.")
}

func selectedWakewordModel(cfg *config.Config, profileID string) string {
	if cfg == nil {
		return ""
	}
	switch strings.TrimSpace(profileID) {
	case "wakeword.phrase":
		if modelPath := strings.TrimSpace(cfg.Wakeword.ModelPath); modelPath != "" && !strings.HasSuffix(strings.ToLower(modelPath), ".txt") {
			return filepath.Base(modelPath)
		}
		if phraseID := strings.TrimSpace(cfg.Wakeword.PhraseID); phraseID != "" {
			return strings.ToLower(phraseID) + ".onnx"
		}
	case "wakeword.shared":
		// The shared profile contains two files (melspectrogram + embedding),
		// so a single profile-level selected filename would be ambiguous.
		return ""
	default:
	}
	return ""
}

func selectedWhisperModel(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	if modelPath := strings.TrimSpace(cfg.Local.ModelPath); modelPath != "" {
		return filepath.Base(modelPath)
	}
	return strings.TrimSpace(cfg.Local.Model)
}

func selectedLocalLLMModelForProfile(cfg *config.Config, profileID string) string {
	if cfg == nil {
		return ""
	}
	hasModeSpecificModel := strings.TrimSpace(cfg.LocalLLM.AssistModel) != "" ||
		strings.TrimSpace(cfg.LocalLLM.UtilityModel) != "" ||
		strings.TrimSpace(cfg.LocalLLM.AgentModel) != ""
	switch strings.TrimSpace(profileID) {
	case "assist.builtin.gemma4-e4b":
		if model := strings.TrimSpace(cfg.LocalLLM.AssistModel); model != "" {
			return filepath.Base(model)
		}
	case "utility.builtin.gemma4-e4b":
		if model := strings.TrimSpace(cfg.LocalLLM.UtilityModel); model != "" {
			return filepath.Base(model)
		}
	case "realtime.builtin.pipeline":
		if model := strings.TrimSpace(cfg.LocalLLM.AgentModel); model != "" {
			return filepath.Base(model)
		}
	default:
	}
	if hasModeSpecificModel {
		return ""
	}
	if modelPath := strings.TrimSpace(cfg.LocalLLM.ModelPath); modelPath != "" {
		return filepath.Base(modelPath)
	}
	if model := strings.TrimSpace(cfg.LocalLLM.Model); model != "" {
		return filepath.Base(model)
	}
	return ""
}

// ResolveWhisperModelsDir returns the directory where whisper model files live.
func ResolveWhisperModelsDir(cfg *config.Config) string {
	if cfg != nil {
		if dir := strings.TrimSpace(cfg.General.ModelDownloadDir); dir != "" {
			return filepath.Clean(dir)
		}
	}
	if cfg != nil && cfg.Local.ModelPath != "" {
		return filepath.Dir(cfg.Local.ModelPath)
	}
	lad := os.Getenv("LOCALAPPDATA")
	if lad != "" {
		return filepath.Join(lad, "SpeechKit", "models")
	}
	return "models"
}

// ResolveWakewordModelsDir returns the directory where wake-word ONNX
// model files live. Mirrors the resolution order used for whisper/LLM
// models so all on-disk artifacts share one user-configurable directory.
//
// Wake-word models are downloaded on first activation when
// cfg.Wakeword.Enabled is set and the model path is missing on disk. The
// catalog entry that drives that download lives in ArtifactCatalog() once
// a published .onnx URL is available; until then, power-users can place a
// custom hey_quby.onnx (trained via tools/wakeword-training/hey_quby.yaml)
// directly into this directory.
func ResolveWakewordModelsDir(cfg *config.Config) string {
	if cfg != nil {
		if dir := strings.TrimSpace(cfg.General.ModelDownloadDir); dir != "" {
			return filepath.Join(filepath.Clean(dir), "wakeword")
		}
	}
	lad := os.Getenv("LOCALAPPDATA")
	if lad != "" {
		return filepath.Join(lad, "SpeechKit", "models", "wakeword")
	}
	return filepath.Join("models", "wakeword")
}

// ResolveLocalLLMModelsDir returns the directory where local llama.cpp GGUF files live.
func ResolveLocalLLMModelsDir(cfg *config.Config) string {
	if cfg != nil {
		if dir := strings.TrimSpace(cfg.General.ModelDownloadDir); dir != "" {
			return filepath.Clean(dir)
		}
	}
	if cfg != nil && cfg.LocalLLM.ModelPath != "" {
		return filepath.Dir(cfg.LocalLLM.ModelPath)
	}
	lad := os.Getenv("LOCALAPPDATA")
	if lad != "" {
		return filepath.Join(lad, "SpeechKit", "models")
	}
	return "models"
}

// FileIsPresent returns true if the given path exists and is a regular file.
func FileIsPresent(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}

func whisperRuntimeAvailability() (bool, string) {
	if _, err := stt.FindWhisperBinary(); err != nil {
		return false, "Local runtime missing: whisper-server binary missing. Re-install SpeechKit to restore local STT."
	}
	return true, ""
}

func localLLMRuntimeProfile(profileID string) bool {
	switch profileID {
	case "assist.builtin.gemma4-e4b", "utility.builtin.gemma4-e4b", "realtime.builtin.pipeline":
		return true
	default:
		return false
	}
}

func localLLMRuntimeAvailability(ctx context.Context, cfg *config.Config) (bool, string) {
	_ = ctx
	_ = cfg
	if _, err := localllm.FindServerBinary(); err != nil {
		return false, "Local LLM runtime missing: llama-server binary missing. Re-install SpeechKit to restore local Assist and Voice Agent."
	}
	return true, ""
}

// OllamaModelPresent quickly checks if Ollama is running and the model is pulled.
// It uses a short timeout so it never blocks the catalog response significantly.
func OllamaModelPresent(ctx context.Context, model string) bool {
	ctx, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
	defer cancel()
	endpoint, err := netsec.BuildEndpoint(OllamaBaseURL, "api/tags", ollamaValidation)
	if err != nil {
		return false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return false
	}
	resp, err := ollamaClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		return false
	}
	defer resp.Body.Close() //nolint:errcheck // response body close error is not actionable
	var result struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if json.NewDecoder(resp.Body).Decode(&result) != nil {
		return false
	}
	prefix := strings.SplitN(model, ":", 2)[0]
	for _, m := range result.Models {
		if m.Name == model || strings.HasPrefix(m.Name, prefix+":") {
			return true
		}
	}
	return false
}
