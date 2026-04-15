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
)

// OllamaBaseURL is the Ollama REST API endpoint. Override in tests.
var OllamaBaseURL = "http://localhost:11434"

// Catalog returns all downloadable models, marking which are already present.
func Catalog(cfg *config.Config) []Item {
	modelsDir := ResolveWhisperModelsDir(cfg)
	selectedLocalModel := selectedWhisperModel(cfg)
	return []Item{
		// â”€â”€ Whisper STT local models â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
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
			Available:   FileIsPresent(filepath.Join(modelsDir, "ggml-small.bin")),
			Selected:    selectedLocalModel == "ggml-small.bin",
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
			Available:   FileIsPresent(filepath.Join(modelsDir, "ggml-large-v3-turbo.bin")),
			Selected:    selectedLocalModel == "ggml-large-v3-turbo.bin",
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
			Available:   FileIsPresent(filepath.Join(modelsDir, "ggml-large-v3.bin")),
			Selected:    selectedLocalModel == "ggml-large-v3.bin",
		},
		// â”€â”€ Ollama LLM models â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
		{
			ID:          "ollama.gemma4-e4b",
			ProfileID:   "utility.ollama.gemma4-e4b",
			Name:        "Gemma 4 E4B â€” Utility (3.3 GB)",
			Description: "Best local LLM for Assist Mode. Great quality on modern laptops. Requires Ollama.",
			SizeLabel:   "~3.3 GB",
			SizeBytes:   3_300_000_000,
			Kind:        KindOllama,
			OllamaModel: "gemma4:e4b",
			License:     "gemma",
			Available:   OllamaModelPresent("gemma4:e4b"),
			Recommended: true,
		},
		{
			ID:          "ollama.gemma4-e4b-assist",
			ProfileID:   "assist.ollama.gemma4-e4b",
			Name:        "Gemma 4 E4B â€” Assist (3.3 GB)",
			Description: "Local Assist model for reasoning and follow-ups. Same weights as Utility E4B.",
			SizeLabel:   "~3.3 GB",
			SizeBytes:   3_300_000_000,
			Kind:        KindOllama,
			OllamaModel: "gemma4:e4b",
			License:     "gemma",
			Available:   OllamaModelPresent("gemma4:e4b"),
		},
	}
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

// ResolveWhisperModelsDir returns the directory where whisper model files live.
func ResolveWhisperModelsDir(cfg *config.Config) string {
	if cfg != nil && cfg.Local.ModelPath != "" {
		return filepath.Dir(cfg.Local.ModelPath)
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

// OllamaModelPresent quickly checks if Ollama is running and the model is pulled.
// It uses a short timeout so it never blocks the catalog response significantly.
func OllamaModelPresent(model string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, OllamaBaseURL+"/api/tags", nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		return false
	}
	defer resp.Body.Close()
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
