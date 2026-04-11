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
	return []Item{
		// ── Whisper STT local models ─────────────────────────────────────────
		{
			ID:          "whisper.ggml-small",
			ProfileID:   "stt.local.whispercpp",
			Name:        "Whisper Small Multilingual (466 MB)",
			Description: "Default local model. Good multilingual quality with a manageable download size.",
			SizeLabel:   "466 MB",
			SizeBytes:   484_264_096,
			Kind:        KindHTTP,
			URL:         "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-small.bin",
			License:     "mit",
			Available:   FileIsPresent(filepath.Join(modelsDir, "ggml-small.bin")),
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
			License:     "mit",
			Available:   FileIsPresent(filepath.Join(modelsDir, "ggml-large-v3.bin")),
		},
		// ── Ollama LLM models ─────────────────────────────────────────────────
		{
			ID:          "ollama.gemma4-e4b",
			ProfileID:   "utility.ollama.gemma4-e4b",
			Name:        "Gemma 4 E4B — Utility (3.3 GB)",
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
			Name:        "Gemma 4 E4B — Assist (3.3 GB)",
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
