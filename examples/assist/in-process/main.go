// Example: fully in-process Assist — text in, one answer out, no SpeechKit
// server. The host owns the LLM call (any OpenAI-compatible chat endpoint) and
// SpeechKit's assist.Service owns the one-shot pipeline, with optional spoken
// output through pkg/speechkit/tts.
//
// Local llama.cpp, no cloud key:
//
//	llama-server -m model.gguf --port 8080
//	SPEECHKIT_LLM_BASE_URL=http://127.0.0.1:8080/v1 go run ./examples/assist/in-process
//
// OpenAI (also enables spoken output):
//
//	OPENAI_API_KEY=sk-... go run ./examples/assist/in-process
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/assist"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/tts"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	apiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("SPEECHKIT_LLM_BASE_URL")), "/")
	model := strings.TrimSpace(os.Getenv("SPEECHKIT_LLM_MODEL"))
	if baseURL == "" {
		if apiKey == "" {
			return errors.New("not configured: set SPEECHKIT_LLM_BASE_URL (e.g. http://127.0.0.1:8080/v1 for llama.cpp) or OPENAI_API_KEY")
		}
		baseURL = "https://api.openai.com/v1"
	}
	if model == "" {
		model = "gpt-6-luna" // OpenAI's efficient tier; llama.cpp ignores the name
	}

	opts := assist.Options{Generator: chatGenerator(baseURL, model, apiKey)}
	if apiKey != "" { // optional spoken output via the public TTS router
		opts.TTSRouter = tts.NewRouter(tts.StrategyCloudFirst, tts.NewOpenAI(tts.OpenAIOpts{APIKey: apiKey}))
		opts.TTSEnabled, opts.TTSBestEffort = true, true
	}
	svc, err := assist.NewService(opts)
	if err != nil {
		return fmt.Errorf("assist service: %w", err)
	}

	fmt.Printf("Assist ready (%s, model %s). Type a request; empty line quits.\n", baseURL, model)
	in := bufio.NewScanner(os.Stdin)
	for fmt.Print("> "); in.Scan(); fmt.Print("> ") {
		line := strings.TrimSpace(in.Text())
		if line == "" {
			break
		}
		res, err := svc.Process(context.Background(), speechkit.AssistRequest{Text: line, Locale: "en"})
		if err != nil {
			fmt.Fprintln(os.Stderr, "assist error:", err)
			continue
		}
		fmt.Println("assist:", res.Text)
		if n := res.Audio.Len(); n > 0 {
			fmt.Printf("        (+%d bytes of %s audio)\n", n, res.Format)
		}
	}
	return in.Err()
}

// chatGenerator calls an OpenAI-compatible /chat/completions endpoint.
func chatGenerator(baseURL, model, apiKey string) assist.Generator {
	client := &http.Client{Timeout: 60 * time.Second}
	return assist.GenerateFunc(func(ctx context.Context, req speechkit.AssistRequest) (speechkit.AssistResult, error) {
		body, err := json.Marshal(map[string]any{
			"model": model,
			"messages": []map[string]string{
				{"role": "system", "content": "You are a concise voice assistant. Answer in one or two short sentences."},
				{"role": "user", "content": req.Text},
			},
		})
		if err != nil {
			return speechkit.AssistResult{}, err
		}
		hr, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(body))
		if err != nil {
			return speechkit.AssistResult{}, err
		}
		hr.Header.Set("Content-Type", "application/json")
		if apiKey != "" {
			hr.Header.Set("Authorization", "Bearer "+apiKey)
		}
		resp, err := client.Do(hr)
		if err != nil {
			return speechkit.AssistResult{}, fmt.Errorf("llm request: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			return speechkit.AssistResult{}, fmt.Errorf("llm status %s", resp.Status)
		}
		var out struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || len(out.Choices) == 0 {
			return speechkit.AssistResult{}, errors.New("llm returned no choices")
		}
		return speechkit.AssistResult{
			Text:    strings.TrimSpace(out.Choices[0].Message.Content),
			Surface: speechkit.AssistSurfacePanel,
			Locale:  req.Locale,
		}, nil
	})
}
