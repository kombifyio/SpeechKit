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
//
// SPEECHKIT_LLM_BASE_URL must be https, or plain http on a loopback host.
// OPENAI_API_KEY is sent only to api.openai.com unless
// SPEECHKIT_LLM_SEND_OPENAI_KEY_TO names the custom host. See README.md.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/assist"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
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
	rawBaseURL := strings.TrimSpace(os.Getenv("SPEECHKIT_LLM_BASE_URL"))
	model := strings.TrimSpace(os.Getenv("SPEECHKIT_LLM_MODEL"))
	if rawBaseURL == "" {
		if apiKey == "" {
			return errors.New("not configured: set SPEECHKIT_LLM_BASE_URL (e.g. http://127.0.0.1:8080/v1 for llama.cpp) or OPENAI_API_KEY")
		}
		rawBaseURL = openAIBaseURL
	}
	endpoint, err := parseLLMBaseURL(rawBaseURL)
	if err != nil {
		return err
	}
	llmKey := llmAPIKey(endpoint, apiKey, os.Getenv("SPEECHKIT_LLM_SEND_OPENAI_KEY_TO"))
	if model == "" {
		model = "gpt-6-luna" // OpenAI's efficient tier; llama.cpp ignores the name
	}
	baseURL := endpoint.String()

	opts := assist.Options{Generator: chatGenerator(endpoint, model, llmKey)}
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

const (
	openAIBaseURL = "https://api.openai.com/v1"
	openAIHost    = "api.openai.com"
)

// parseLLMBaseURL validates SPEECHKIT_LLM_BASE_URL before anything is sent to
// it. The URL must be absolute https, or plain http on a loopback host (a
// local llama.cpp or Ollama), so a prompt never leaves the machine
// unencrypted. Credentials, query strings and fragments are refused: the base
// URL names an API root, nothing more.
func parseLLMBaseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("SPEECHKIT_LLM_BASE_URL: %w", err)
	}
	if u.Hostname() == "" {
		return nil, fmt.Errorf("SPEECHKIT_LLM_BASE_URL %q: need an absolute URL with a host", raw)
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("SPEECHKIT_LLM_BASE_URL %q: credentials, query and fragment are not allowed", raw)
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !isLoopbackHost(u.Hostname()) {
			return nil, fmt.Errorf("SPEECHKIT_LLM_BASE_URL %q: plain http is allowed only for a loopback host; use https", raw)
		}
	default:
		return nil, fmt.Errorf("SPEECHKIT_LLM_BASE_URL %q: scheme must be https (or http on loopback)", raw)
	}
	u.Path = strings.TrimRight(u.Path, "/")
	return u, nil
}

// isLoopbackHost reports whether host is localhost or a loopback IP literal.
// Other names are not resolved: a name that resolves to loopback today can
// resolve elsewhere tomorrow.
func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// llmAPIKey decides whether OPENAI_API_KEY goes to the chat endpoint. It does
// for api.openai.com. For any other host it does only when sendKeyTo names
// exactly that host, so pointing SPEECHKIT_LLM_BASE_URL somewhere else never
// hands the OpenAI key to it by accident.
func llmAPIKey(endpoint *url.URL, openAIKey, sendKeyTo string) string {
	host := strings.ToLower(endpoint.Hostname())
	if host == openAIHost && endpoint.Scheme == "https" {
		return openAIKey
	}
	if optIn := strings.ToLower(strings.TrimSpace(sendKeyTo)); optIn != "" && optIn == host {
		return openAIKey
	}
	return ""
}

// chatGenerator calls an OpenAI-compatible /chat/completions endpoint under a
// base URL that parseLLMBaseURL accepted.
func chatGenerator(endpoint *url.URL, model, apiKey string) assist.Generator {
	client := netsec.NewClient(netsec.ClientOptions{Timeout: 60 * time.Second})
	chatURL := endpoint.JoinPath("chat", "completions").String()
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
		hr, err := http.NewRequestWithContext(ctx, http.MethodPost, chatURL, bytes.NewReader(body)) // #nosec G704 -- chatURL is built from a base URL parseLLMBaseURL accepted: https, or http on a loopback host only
		if err != nil {
			return speechkit.AssistResult{}, err
		}
		hr.Header.Set("Content-Type", "application/json")
		if apiKey != "" {
			hr.Header.Set("Authorization", "Bearer "+apiKey)
		}
		resp, err := client.Do(hr) // #nosec G704 -- hr targets chatURL, validated by parseLLMBaseURL (https, or http on loopback only)
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
