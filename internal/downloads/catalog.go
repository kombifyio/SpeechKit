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
			ID:          "llamacpp.gemma-3-4b-it-q4-k-m",
			ProfileID:   "assist.builtin.gemma4-e4b",
			Name:        "Gemma 3 4B IT Q4_K_M (GGUF)",
			Description: "Recommended balanced local Assist model for SpeechKit's bundled llama.cpp runtime.",
			SizeLabel:   "~2.5 GB",
			SizeBytes:   2_490_000_000,
			Kind:        KindHTTP,
			URL:         "https://huggingface.co/ggml-org/gemma-3-4b-it-GGUF/resolve/main/gemma-3-4b-it-Q4_K_M.gguf",
			SHA256:      "882e8d2db44dc554fb0ea5077cb7e4bc49e7342a1f0da57901c0802ea21a0863",
			License:     "gemma",
			Recommended: true,
		},
		{
			ID:          "llamacpp.gemma-3-4b-it-q8-0",
			ProfileID:   "assist.builtin.gemma4-e4b",
			Name:        "Gemma 3 4B IT Q8_0 (GGUF)",
			Description: "Higher-fidelity local Assist model for SpeechKit's bundled llama.cpp runtime.",
			SizeLabel:   "~4.1 GB",
			SizeBytes:   4_130_000_000,
			Kind:        KindHTTP,
			URL:         "https://huggingface.co/ggml-org/gemma-3-4b-it-GGUF/resolve/main/gemma-3-4b-it-Q8_0.gguf",
			SHA256:      "97b06383df48336e7d2f9b56b6ce545e0fa476407a62c0bd081b53447a58e644",
			License:     "gemma",
		},
		{
			ID:          "llamacpp.gemma-3-4b-it-q4-k-m-voice",
			ProfileID:   "realtime.builtin.pipeline",
			Name:        "Gemma 3 4B IT Q4_K_M - Voice Agent (GGUF)",
			Description: "Recommended balanced local model for the Voice Agent pipeline fallback with SpeechKit's bundled llama.cpp runtime.",
			SizeLabel:   "~2.5 GB",
			SizeBytes:   2_490_000_000,
			Kind:        KindHTTP,
			URL:         "https://huggingface.co/ggml-org/gemma-3-4b-it-GGUF/resolve/main/gemma-3-4b-it-Q4_K_M.gguf",
			SHA256:      "882e8d2db44dc554fb0ea5077cb7e4bc49e7342a1f0da57901c0802ea21a0863",
			License:     "gemma",
			Recommended: true,
		},
		{
			ID:          "llamacpp.gemma-3-4b-it-q8-0-voice",
			ProfileID:   "realtime.builtin.pipeline",
			Name:        "Gemma 3 4B IT Q8_0 - Voice Agent (GGUF)",
			Description: "Higher-fidelity local model for the Voice Agent pipeline fallback with SpeechKit's bundled llama.cpp runtime.",
			SizeLabel:   "~4.1 GB",
			SizeBytes:   4_130_000_000,
			Kind:        KindHTTP,
			URL:         "https://huggingface.co/ggml-org/gemma-3-4b-it-GGUF/resolve/main/gemma-3-4b-it-Q8_0.gguf",
			SHA256:      "97b06383df48336e7d2f9b56b6ce545e0fa476407a62c0bd081b53447a58e644",
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
			item.Available = FileIsPresent(filepath.Join(artifactModelDir(*item, cfg), filename))
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

func artifactModelDir(item Item, cfg *config.Config) string {
	if item.ProfileID == "stt.local.whispercpp" {
		return ResolveWhisperModelsDir(cfg)
	}
	return ResolveLocalLLMModelsDir(cfg)
}

func selectedArtifactModel(cfg *config.Config, profileID string) string {
	if profileID == "stt.local.whispercpp" {
		return selectedWhisperModel(cfg)
	}
	return selectedLocalLLMModelForProfile(cfg, profileID)
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
