// Example: fully in-process Assist — text (or speech) in, one useful result
// out, no SpeechKit server.
//
// It wires a real LLM Generator (Gemini via the genai SDK) into
// pkg/speechkit/assist.Service, and — when an OpenAI key is present — the
// public pkg/speechkit/tts router for spoken output. This demonstrates that
// Assist and TTS are embeddable in-process: the host owns the LLM call and
// SpeechKit owns the pipeline (codeword/utility/LLM -> one-shot result ->
// optional TTS).
//
// Audio-in: this demo takes text on stdin to stay portable. To go from speech,
// transcribe a WAV with a public STT provider first, e.g.
//
//	p := openaicompat.NewOpenAISTTProvider(os.Getenv("OPENAI_API_KEY")) // pkg/speechkit/stt
//	res, _ := p.Transcribe(ctx, wav, stt.TranscribeOpts{})
//	req := speechkit.AssistRequest{Text: res.Text}
//
// Run:
//
//	GOOGLE_AI_API_KEY=... go run ./examples/assist/in-process
//	# optional spoken output (public TTS router): also set OPENAI_API_KEY
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/assist"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/tts"
	"google.golang.org/genai"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	model := flag.String("model", "gemini-2.5-flash", "Gemini generateContent model")
	flag.Parse()

	apiKey := strings.TrimSpace(os.Getenv("GOOGLE_AI_API_KEY"))
	if apiKey == "" {
		return errors.New("set GOOGLE_AI_API_KEY (a Google AI Studio key)")
	}

	ctx := context.Background()
	client, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: apiKey, Backend: genai.BackendGeminiAPI})
	if err != nil {
		return fmt.Errorf("genai client: %w", err)
	}

	// The Generator is the host's LLM call. SpeechKit's assist.Service owns the
	// surrounding one-shot pipeline; the host owns the model.
	generator := assist.GenerateFunc(func(ctx context.Context, req speechkit.AssistRequest) (speechkit.AssistResult, error) {
		prompt := "You are a concise voice assistant. Answer in one or two short sentences.\n\nUser: " + req.Text
		resp, err := client.Models.GenerateContent(ctx, *model, genai.Text(prompt), nil)
		if err != nil {
			return speechkit.AssistResult{}, err
		}
		return speechkit.AssistResult{Text: strings.TrimSpace(resp.Text()), Locale: req.Locale}, nil
	})

	opts := assist.Options{Generator: generator}

	// Optional spoken output through the public TTS router (pkg/speechkit/tts).
	if oaKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY")); oaKey != "" {
		opts.TTSRouter = tts.NewRouter(tts.StrategyCloudFirst, tts.NewOpenAI(tts.OpenAIOpts{APIKey: oaKey}))
		opts.TTSEnabled = true
		fmt.Println("(spoken output enabled via OpenAI TTS)")
	}

	svc, err := assist.NewService(opts)
	if err != nil {
		return fmt.Errorf("assist service: %w", err)
	}

	fmt.Println("in-process Assist ready. Type a request and press Enter (empty line quits):")
	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			break
		}
		res, err := svc.Process(ctx, speechkit.AssistRequest{Text: line, Locale: "en"})
		if err != nil {
			fmt.Fprintln(os.Stderr, "assist error:", err)
			continue
		}
		fmt.Printf("assist: %s\n", res.Text)
		if audio := res.Audio.Bytes(); len(audio) > 0 {
			fmt.Printf("       (+%d bytes of %s audio)\n", len(audio), res.Format)
		}
	}
	return scanner.Err()
}
