// Command sk-localprobe verifies that the SpeechKit kernel libraries
// produce working Dictation, Assist, and Voice Agent results against
// LOCAL models only (Whisper.cpp + Gemma via llama-server). It is
// driven by install-e2e-windows.yml after a silent NSIS install to
// prove the post-install kernel works end-to-end with no cloud keys.
//
// sk-localprobe does NOT need a running SpeechKit.exe. It imports the
// same kernel packages (internal/stt, internal/localllm,
// internal/voiceagent/cascaded) the Wails app uses and exercises them
// directly. This decouples the local-only guarantee from any Wails
// initialization path while still proving the production library code
// works against the installed binaries + models.
//
// Required flags:
//
//	--whisper-model    path to ggml whisper .bin file
//	--gemma-model      path to Gemma .gguf file (or any GGUF the local
//	                   llama-server supports)
//	--fixtures         testdata/e2e root with dictation/, assist/,
//	                   voiceagent/ subdirs
//
// Optional:
//
//	--report           JSON report path. Default sk-localprobe-report.json
//	--timeout          per-mode timeout. Default 5m (cold first-run can
//	                   take a few minutes when llama warms up).
//	--skip-voiceagent  skip the two-turn voice-agent scenario (debug)
//
// Exit codes:
//
//	0 — all three modes passed
//	1 — at least one mode failed
//	2 — setup error (binary not found, model missing, etc.)
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/internal/ai/flows"
	"github.com/kombifyio/SpeechKit/internal/localllm"
	"github.com/kombifyio/SpeechKit/internal/stt"
	"github.com/kombifyio/SpeechKit/internal/voiceagent/cascaded"
)

func main() {
	whisperModel := flag.String("whisper-model", "", "absolute path to ggml whisper .bin file (required)")
	gemmaModel := flag.String("gemma-model", "", "absolute path to Gemma .gguf file (required)")
	fixturesDir := flag.String("fixtures", "testdata/e2e", "fixtures root containing dictation/, assist/, voiceagent/")
	reportPath := flag.String("report", "sk-localprobe-report.json", "JSON report output")
	timeout := flag.Duration("timeout", 5*time.Minute, "per-mode timeout")
	skipVA := flag.Bool("skip-voiceagent", false, "skip the two-turn voice-agent scenario")
	flag.Parse()

	if *whisperModel == "" || *gemmaModel == "" {
		fmt.Fprintln(os.Stderr, "sk-localprobe: --whisper-model and --gemma-model are required")
		os.Exit(2)
	}
	if _, err := os.Stat(*whisperModel); err != nil {
		fmt.Fprintf(os.Stderr, "sk-localprobe: whisper model not found: %v\n", err)
		os.Exit(2)
	}
	if _, err := os.Stat(*gemmaModel); err != nil {
		fmt.Fprintf(os.Stderr, "sk-localprobe: gemma model not found: %v\n", err)
		os.Exit(2)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rep := &report{Modes: map[string]*modeResult{}}

	whisperPort, err := freePort()
	if err != nil {
		failSetup(rep, "free port (whisper)", err, *reportPath)
	}
	llamaPort, err := freePort()
	if err != nil {
		failSetup(rep, "free port (llama)", err, *reportPath)
	}

	fmt.Printf("==> starting whisper.cpp on :%d using %s\n", whisperPort, *whisperModel)
	whisper := stt.NewLocalProvider(whisperPort, *whisperModel, "")
	if err := whisper.StartServer(ctx); err != nil {
		failSetup(rep, "whisper start", err, *reportPath)
	}
	defer whisper.StopServer()

	fmt.Printf("==> starting llama-server on :%d using %s\n", llamaPort, *gemmaModel)
	llama := localllm.NewServer(llamaPort, *gemmaModel, "")
	if err := llama.StartServer(ctx); err != nil {
		failSetup(rep, "llama start", err, *reportPath)
	}
	defer llama.StopServer()

	llamaBaseURL := fmt.Sprintf("http://127.0.0.1:%d", llamaPort)

	rep.Modes["dictation"] = runDictation(ctx, whisper, *fixturesDir, *timeout)
	rep.Modes["assist"] = runAssist(ctx, llamaBaseURL, *fixturesDir, *timeout)
	if *skipVA {
		rep.Modes["voiceagent"] = &modeResult{Skipped: true, Want: "skipped via --skip-voiceagent"}
	} else {
		rep.Modes["voiceagent"] = runVoiceAgent(ctx, whisper, llamaBaseURL, *fixturesDir, *timeout)
	}

	writeReport(*reportPath, rep)
	failed := 0
	for name, m := range rep.Modes {
		if m.Skipped {
			fmt.Printf("SKIP  %s\n", name)
			continue
		}
		if !m.Pass {
			failed++
			fmt.Printf("FAIL  %s: %s\n", name, m.Err)
			continue
		}
		fmt.Printf("PASS  %s  (%s) got=%q\n", name, m.Latency, m.Got)
	}
	if failed > 0 {
		fmt.Printf("\nFAIL %d/%d modes\n", failed, len(rep.Modes))
		os.Exit(1)
	}
	fmt.Printf("\nOK %d/%d modes\n", len(rep.Modes), len(rep.Modes))
}

// ── reports ─────────────────────────────────────────────────────────────────

type report struct {
	Modes map[string]*modeResult `json:"modes"`
}

type modeResult struct {
	Pass    bool   `json:"pass"`
	Skipped bool   `json:"skipped,omitempty"`
	Latency string `json:"latency,omitempty"`
	Got     string `json:"got,omitempty"`
	Want    string `json:"want,omitempty"`
	Err     string `json:"err,omitempty"`
}

func failSetup(rep *report, label string, err error, reportPath string) {
	rep.Modes[label] = &modeResult{Err: err.Error()}
	writeReport(reportPath, rep)
	fmt.Fprintf(os.Stderr, "sk-localprobe: setup error %s: %v\n", label, err)
	os.Exit(2)
}

func writeReport(path string, r *report) {
	data, _ := json.MarshalIndent(r, "", "  ")
	_ = os.WriteFile(path, data, 0o644)
}

// ── scenarios ───────────────────────────────────────────────────────────────

func runDictation(ctx context.Context, whisper *stt.LocalProvider, fixDir string, timeout time.Duration) *modeResult {
	wavPath := filepath.Join(fixDir, "dictation/hello-world.wav")
	wav, err := os.ReadFile(wavPath)
	if err != nil {
		return &modeResult{Err: "read fixture: " + err.Error()}
	}
	wantPath := filepath.Join(fixDir, "dictation/hello-world.expected.txt")
	want, err := os.ReadFile(wantPath)
	if err != nil {
		return &modeResult{Err: "read expected: " + err.Error()}
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	result, err := whisper.Transcribe(ctx, wav, stt.TranscribeOpts{Language: "en"})
	dur := time.Since(start)
	if err != nil {
		return &modeResult{Err: "transcribe: " + err.Error(), Latency: dur.String()}
	}
	if result == nil {
		return &modeResult{Err: "transcribe returned nil result", Latency: dur.String()}
	}
	return &modeResult{
		Pass:    matchTranscript(result.Text, strings.TrimSpace(string(want))),
		Latency: dur.String(),
		Got:     result.Text,
		Want:    strings.TrimSpace(string(want)),
	}
}

func runAssist(ctx context.Context, llamaBaseURL, fixDir string, timeout time.Duration) *modeResult {
	wantPath := filepath.Join(fixDir, "assist/llm-shortq.expected-prefix.txt")
	wantPrefix := ""
	if data, err := os.ReadFile(wantPath); err == nil {
		wantPrefix = strings.TrimSpace(string(data))
	}

	// Assist uses the LLM directly. We send a fixed question and assert
	// the response is non-empty and (when an expected-prefix is
	// provided) starts with the expected token.
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	answer, err := chatCompletion(ctx, llamaBaseURL, "You are a concise math tutor. Answer in 1-3 words.", "What is two plus two?", 64)
	dur := time.Since(start)
	if err != nil {
		return &modeResult{Err: "chat completion: " + err.Error(), Latency: dur.String()}
	}
	got := strings.TrimSpace(answer)
	if got == "" {
		return &modeResult{Err: "empty LLM response", Latency: dur.String()}
	}
	if wantPrefix != "" && !strings.Contains(strings.ToLower(got), strings.ToLower(wantPrefix)) {
		return &modeResult{
			Err:     fmt.Sprintf("expected response to contain %q, got %q", wantPrefix, got),
			Latency: dur.String(),
			Got:     got,
			Want:    wantPrefix,
		}
	}
	return &modeResult{
		Pass:    true,
		Latency: dur.String(),
		Got:     got,
		Want:    "non-empty LLM response (containing '" + wantPrefix + "' if pinned)",
	}
}

func runVoiceAgent(ctx context.Context, whisper *stt.LocalProvider, llamaBaseURL, fixDir string, timeout time.Duration) *modeResult {
	t1Path := filepath.Join(fixDir, "voiceagent/turn1.wav")
	t2Path := filepath.Join(fixDir, "voiceagent/turn2.wav")
	t1, err := os.ReadFile(t1Path)
	if err != nil {
		return &modeResult{Err: "read turn1: " + err.Error()}
	}
	t2, err := os.ReadFile(t2Path)
	if err != nil {
		return &modeResult{Err: "read turn2: " + err.Error()}
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	provider := cascaded.NewProvider(cascaded.Deps{
		STT:   &whisperRouterAdapter{local: whisper},
		Agent: &llamaAgentAdapter{baseURL: llamaBaseURL, system: "You are a helpful assistant. Respond in one sentence."},
		Config: cascaded.Config{
			SilenceTurnMs: 200,
			MinTurnMs:     100,
			HistoryTurns:  3,
		},
	})
	if err := provider.Connect(ctx, cascaded.SessionConfig{Locale: "en"}); err != nil {
		return &modeResult{Err: "cascaded connect: " + err.Error()}
	}
	defer provider.Close()

	// Per-turn budget: whisper transcription + llama inference must both
	// complete. On a 2-core GH-hosted runner the whisper step alone
	// takes ~70 s; allow 5 min per turn so we never time out before the
	// real path finishes.
	turnBudget := 5 * time.Minute

	start := time.Now()
	turns, err := pushTurnAndCollect(ctx, provider, t1, turnBudget)
	if err != nil {
		return &modeResult{Err: "turn1: " + err.Error(), Latency: time.Since(start).String()}
	}
	if len(turns) == 0 || turns[0] == "" {
		return &modeResult{Err: "turn1 produced empty response", Latency: time.Since(start).String()}
	}
	turn1Resp := turns[0]

	turns, err = pushTurnAndCollect(ctx, provider, t2, turnBudget)
	if err != nil {
		return &modeResult{Err: "turn2: " + err.Error(), Latency: time.Since(start).String()}
	}
	if len(turns) == 0 || turns[0] == "" {
		return &modeResult{Err: "turn2 produced empty response", Latency: time.Since(start).String()}
	}
	turn2Resp := turns[0]

	return &modeResult{
		Pass:    true,
		Latency: time.Since(start).String(),
		Got:     turn1Resp + " || " + turn2Resp,
		Want:    "two non-empty assistant turns",
	}
}

// pushTurnAndCollect feeds one audio turn to the provider, signals the
// stream end, and collects assistant output until the budget expires
// or an OutputTranscriptDone arrives.
func pushTurnAndCollect(ctx context.Context, p *cascaded.Provider, audio []byte, budget time.Duration) ([]string, error) {
	if err := p.SendAudio(audio); err != nil {
		return nil, err
	}
	if err := p.SendAudioStreamEnd(); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(budget)
	var responses []string
	for time.Now().Before(deadline) {
		recvCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		msg, err := p.Receive(recvCtx)
		cancel()
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				continue
			}
			return responses, err
		}
		if msg == nil {
			continue
		}
		if msg.OutputTranscript != "" && msg.OutputTranscriptDone {
			responses = append(responses, msg.OutputTranscript)
			return responses, nil
		}
	}
	return responses, errors.New("no output transcript within budget")
}

// ── adapters ────────────────────────────────────────────────────────────────

type whisperRouterAdapter struct {
	local *stt.LocalProvider
}

func (w *whisperRouterAdapter) Route(ctx context.Context, audio []byte, _ float64, opts stt.TranscribeOpts) (*stt.Result, error) {
	return w.local.Transcribe(ctx, audio, opts)
}

type llamaAgentAdapter struct {
	baseURL string
	system  string
}

func (a *llamaAgentAdapter) Run(ctx context.Context, in flows.AgentInput) (flows.AgentOutput, error) {
	system := a.system
	if in.SystemPrompt != "" {
		system = in.SystemPrompt
	}
	answer, err := chatCompletion(ctx, a.baseURL, system, in.Utterance, 256)
	if err != nil {
		return flows.AgentOutput{}, err
	}
	return flows.AgentOutput{Text: answer, Action: "display"}, nil
}

// ── helpers ─────────────────────────────────────────────────────────────────

func chatCompletion(ctx context.Context, baseURL, system, user string, maxTokens int) (string, error) {
	payload := map[string]any{
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
		"max_tokens":  maxTokens,
		"temperature": 0.2,
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("chat completion %d: %s", resp.StatusCode, raw)
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("decode chat completion: %w; raw=%s", err, raw)
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("chat completion: no choices in response: %s", raw)
	}
	return parsed.Choices[0].Message.Content, nil
}

// matchTranscript supports either a plain text comparison (case-
// insensitive, whitespace-trimmed) OR a regex when expected starts
// with "(?" or "^".
func matchTranscript(actual, expected string) bool {
	actual = strings.TrimSpace(actual)
	expected = strings.TrimSpace(expected)
	if strings.HasPrefix(expected, "(?") || strings.HasPrefix(expected, "^") {
		re, err := regexp.Compile(expected)
		if err != nil {
			return false
		}
		return re.MatchString(actual)
	}
	return strings.EqualFold(actual, expected)
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
