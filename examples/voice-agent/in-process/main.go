// Example: fully in-process Voice Agent — no SpeechKit server in the path.
//
// This is the in-process counterpart to examples/voice-agent/game-instructor
// (which drives a running speechkit-server over WebSocket). Here the realtime
// provider runs inside this binary: we construct a live.LiveProvider directly
// (Gemini Live), wrap it in a live.Session, and drive a text dialogue. The
// provider connects straight to the model's realtime API — STT, the LLM turn,
// and TTS all happen provider-side, so a host needs no separate STT/TTS wiring.
//
// This is the reference an independent coding agent can adapt with one prompt:
// "Embed a SpeechKit Voice Agent in my Go app." Swap the stdin loop for a mic
// source and feed session.SendAudio(pcm) to go fully voice; the OnAudio
// callback already receives the model's 24 kHz PCM for playback.
//
// Run:
//
//	GOOGLE_AI_API_KEY=... go run ./examples/voice-agent/in-process
//
// Then type a line and press Enter. Empty input or EOF (Ctrl-D / Ctrl-Z) ends
// the session. Audio frames from the model are counted, not played, so the
// example stays portable (no audio device needed).
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	live "github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live/gemini"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	apiKey := strings.TrimSpace(os.Getenv("GOOGLE_AI_API_KEY"))
	if apiKey == "" {
		return errors.New("set GOOGLE_AI_API_KEY (a Google AI Studio key with Gemini Live access)")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// turnDone is signaled whenever the agent finishes a spoken turn so the
	// reader prompt reappears after each answer.
	var (
		mu        sync.Mutex
		audioRx   int
		lastState = live.StateInactive
		turnDone  = make(chan struct{}, 1)
	)
	signalTurnDone := func() {
		select {
		case turnDone <- struct{}{}:
		default:
		}
	}

	callbacks := live.Callbacks{
		OnStateChange: func(s live.State) {
			fmt.Printf("[state=%s]\n", s)
			mu.Lock()
			prev := lastState
			lastState = s
			mu.Unlock()
			// A turn is complete when the agent stops speaking and returns to
			// listening. This covers the common realtime path where a turn ends
			// via the speaking-settle timer without a terminal transcript flag.
			if s == live.StateListening && (prev == live.StateSpeaking || prev == live.StateProcessing) {
				signalTurnDone()
			}
		},
		OnInputTranscript: func(text string, done bool) {
			if done && strings.TrimSpace(text) != "" {
				fmt.Printf("you (heard): %s\n", text)
			}
		},
		OnOutputTranscript: func(text string, done bool) {
			if strings.TrimSpace(text) != "" {
				fmt.Printf("agent: %s\n", text)
			}
			if done {
				signalTurnDone()
			}
		},
		OnAudio: func(pcm []byte) {
			mu.Lock()
			audioRx += len(pcm)
			mu.Unlock()
		},
		OnError:      func(err error) { fmt.Fprintln(os.Stderr, "session error:", err) },
		OnSessionEnd: func() { signalTurnDone() },
	}

	provider := gemini.New()
	session := live.NewSession(provider, callbacks)

	cfg := live.LiveConfig{
		// Model empty → the kernel default Gemini Live model is used.
		APIKey:          apiKey,
		Locale:          "en",
		FrameworkPrompt: "You are a concise in-process Voice Agent demo. Answer in one short spoken sentence.",
	}
	if err := session.Start(ctx, cfg, live.DefaultIdleConfig()); err != nil {
		return fmt.Errorf("start session: %w", err)
	}
	defer session.Stop()

	fmt.Println("connected to Gemini Live in-process. Type a line and press Enter (empty line or Ctrl-Z+Enter to quit):")
	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break // EOF
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			break
		}
		if err := session.SendText(line); err != nil {
			return fmt.Errorf("send text: %w", err)
		}
		// Wait for the agent to finish its turn (or the context to end).
		select {
		case <-turnDone:
		case <-ctx.Done():
			return nil
		case <-time.After(30 * time.Second):
			fmt.Fprintln(os.Stderr, "warn: no turn completion within 30s")
		}
	}

	mu.Lock()
	total := audioRx
	mu.Unlock()
	fmt.Printf("session ended cleanly (received %d bytes of agent audio).\n", total)
	return scanner.Err()
}
