package voiceagent

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/internal/config"
)

func TestGeminiLiveConnectIntegration(t *testing.T) {
	if os.Getenv("SPEECHKIT_RUN_LIVE_GEMINI_TEST") != "1" {
		t.Skip("set SPEECHKIT_RUN_LIVE_GEMINI_TEST=1 to run live Gemini integration")
	}

	apiKey := config.ResolveSecret("GOOGLE_AI_API_KEY")
	if apiKey == "" {
		t.Skip("GOOGLE_AI_API_KEY is not available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	provider := NewGeminiLive()
	err := provider.Connect(ctx, LiveConfig{
		Model:    defaultGeminiLiveModel,
		APIKey:   apiKey,
		Voice:    "Kore",
		Locale:   "de-DE",
		Policies: defaultLivePolicies(),
	})
	if err != nil {
		t.Fatalf("connect live Gemini session: %v", err)
	}
	defer func() { //nolint:gocritic // unnecessaryDefer: t.Fatalf inside closure cannot be in a plain defer
		if err := provider.Close(); err != nil {
			t.Fatalf("close live Gemini session: %v", err)
		}
	}()
}

func TestGeminiLiveDialogIntegration(t *testing.T) {
	if os.Getenv("SPEECHKIT_RUN_LIVE_GEMINI_TEST") != "1" {
		t.Skip("set SPEECHKIT_RUN_LIVE_GEMINI_TEST=1 to run live Gemini integration")
	}

	apiKey := config.ResolveSecret("GOOGLE_AI_API_KEY")
	if apiKey == "" {
		t.Skip("GOOGLE_AI_API_KEY is not available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	provider := NewGeminiLive()
	var (
		mu              sync.Mutex
		audioBytes      int
		outputTexts     []string
		stateChanges    []State
		receivedError   error
		sessionEnded    bool
	)

	session := NewSession(provider, Callbacks{
		OnStateChange: func(state State) {
			mu.Lock()
			stateChanges = append(stateChanges, state)
			mu.Unlock()
		},
		OnAudio: func(audio []byte) {
			mu.Lock()
			audioBytes += len(audio)
			mu.Unlock()
		},
		OnText: func(text string) {
			trimmed := strings.TrimSpace(text)
			if trimmed == "" {
				return
			}
			mu.Lock()
			outputTexts = append(outputTexts, trimmed)
			mu.Unlock()
		},
		OnOutputTranscript: func(text string, done bool) {
			trimmed := strings.TrimSpace(text)
			if trimmed == "" {
				return
			}
			mu.Lock()
			if done {
				outputTexts = append(outputTexts, trimmed)
			}
			mu.Unlock()
		},
		OnError: func(err error) {
			mu.Lock()
			receivedError = err
			mu.Unlock()
		},
		OnSessionEnd: func() {
			mu.Lock()
			sessionEnded = true
			mu.Unlock()
		},
	})

	if err := session.Start(ctx, LiveConfig{
		Model:    defaultGeminiLiveModel,
		APIKey:   apiKey,
		Voice:    "Kore",
		Locale:   "de-DE",
		Policies: defaultLivePolicies(),
	}, IdleConfig{
		ReminderAfter:   1 * time.Hour,
		DeactivateAfter: 2 * time.Hour,
	}); err != nil {
		t.Fatalf("start live Gemini session: %v", err)
	}
	defer session.Stop()

	if err := session.SendText("Antworte auf Deutsch mit einem sehr kurzen Satz und bestaetige, dass der SpeechKit-Dialogtest angekommen ist."); err != nil {
		t.Fatalf("send dialog turn: %v", err)
	}

	waitForCondition(t, 35*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return audioBytes > 0 && session.CurrentState() == StateListening
	})

	mu.Lock()
	defer mu.Unlock()
	if receivedError != nil {
		t.Fatalf("received unexpected session error: %v", receivedError)
	}
	if sessionEnded {
		t.Fatal("session ended unexpectedly during dialog test")
	}
	if audioBytes == 0 {
		t.Fatal("expected audio output from live Gemini dialog")
	}
	if !containsState(stateChanges, StateConnecting) || !containsState(stateChanges, StateSpeaking) {
		t.Fatalf("state changes = %#v, want connecting and speaking", stateChanges)
	}
	if len(outputTexts) > 0 {
		t.Logf("live dialog output: %s", outputTexts[len(outputTexts)-1])
	}
}
