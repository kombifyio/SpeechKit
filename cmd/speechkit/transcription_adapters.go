package main

import (
	"context"
	"fmt"
	"log"

	"github.com/firebase/genkit/go/core"

	"github.com/kombifyio/SpeechKit/internal/ai/flows"
	"github.com/kombifyio/SpeechKit/internal/assist"
	"github.com/kombifyio/SpeechKit/internal/audio"
	"github.com/kombifyio/SpeechKit/internal/output"
	"github.com/kombifyio/SpeechKit/internal/router"
	"github.com/kombifyio/SpeechKit/internal/store"
	"github.com/kombifyio/SpeechKit/internal/stt"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

type routerTranscriber struct {
	router *router.Router
}

func (t routerTranscriber) Transcribe(ctx context.Context, audio []byte, durationSecs float64, language string) (speechkit.Transcript, error) {
	if t.router == nil {
		return speechkit.Transcript{}, fmt.Errorf("router not configured")
	}

	result, err := t.router.Route(ctx, audio, durationSecs, stt.TranscribeOpts{Language: language})
	if err != nil {
		return speechkit.Transcript{}, err
	}

	return speechkit.Transcript{
		Text:       result.Text,
		Language:   result.Language,
		Duration:   result.Duration,
		Provider:   result.Provider,
		Model:      result.Model,
		Confidence: result.Confidence,
	}, nil
}

type speechkitStoreAdapter struct {
	store store.Store
}

func (a speechkitStoreAdapter) SaveQuickNote(ctx context.Context, text, language, provider string, durationMs, latencyMs int64, audioData []byte) (int64, error) {
	if a.store == nil {
		return 0, nil
	}
	return a.store.SaveQuickNote(ctx, text, language, provider, durationMs, latencyMs, audioData)
}

func (a speechkitStoreAdapter) GetQuickNoteText(ctx context.Context, id int64) (string, error) {
	if a.store == nil {
		return "", nil
	}
	note, err := a.store.GetQuickNote(ctx, id)
	if err != nil {
		return "", err
	}
	return note.Text, nil
}

func (a speechkitStoreAdapter) UpdateQuickNote(ctx context.Context, id int64, text string) error {
	if a.store == nil {
		return nil
	}
	return a.store.UpdateQuickNote(ctx, id, text)
}

func (a speechkitStoreAdapter) UpdateQuickNoteCapture(ctx context.Context, id int64, text, provider string, durationMs, latencyMs int64, audioData []byte) error {
	if a.store == nil {
		return nil
	}
	return a.store.UpdateQuickNoteCapture(ctx, id, text, provider, durationMs, latencyMs, audioData)
}

func (a speechkitStoreAdapter) SaveTranscription(ctx context.Context, text, language, provider, model string, durationMs, latencyMs int64, audioData []byte) error {
	if a.store == nil {
		return nil
	}
	return a.store.SaveTranscription(ctx, text, language, provider, model, durationMs, latencyMs, audioData)
}

type speechkitCommitObserver struct {
	state *appState
}

func (o speechkitCommitObserver) OnCommit(completion speechkit.Completion) {
	if o.state == nil {
		return
	}
	o.state.applyTranscriptionCompletion(completion)
}

type desktopTranscriptOutput struct {
	handler        output.OutputHandler
	interceptor    transcriptInterceptor
	agentFlow      *core.Flow[flows.AgentInput, flows.AgentOutput, struct{}]
	assistPipeline *assist.Pipeline
	audioPlayer    *audio.Player
	activeMode     func() string
	agentMode      func() string // "assist" or "voice_agent"
	onAssistText   func(text string) // Callback for UI (speech bubble)
}

func (o desktopTranscriptOutput) Deliver(ctx context.Context, transcript speechkit.Transcript, target any) error {
	mode := ""
	if o.activeMode != nil {
		mode = o.activeMode()
	}

	// 1. Codeword interception -- works in BOTH dictate and agent mode.
	if o.interceptor != nil {
		handled, err := o.interceptor.Intercept(ctx, transcript, target)
		if err != nil {
			return err
		}
		if handled {
			return nil
		}
	}

	// 2. Agent/Assist mode.
	if mode == "agent" {
		agentMode := "assist"
		if o.agentMode != nil {
			agentMode = o.agentMode()
		}

		switch agentMode {
		case "assist":
			return o.deliverAssist(ctx, transcript, target)
		case "voice_agent":
			// Voice Agent Mode uses real-time WebSocket, not this pipeline.
			// If we reach here, the Voice Agent session isn't active — fall through to legacy agent.
			log.Printf("WARN: voice_agent mode active but no live session — falling back to agent flow")
		}

		// Legacy/fallback agent flow.
		return o.deliverAgentFlow(ctx, transcript, target)
	}

	// 3. Dictate mode -- pass through to clipboard.
	if o.handler == nil {
		return nil
	}

	outputTarget, _ := target.(output.Target)
	return o.handler.Handle(ctx, &stt.Result{
		Text:       transcript.Text,
		Language:   transcript.Language,
		Duration:   transcript.Duration,
		Provider:   transcript.Provider,
		Model:      transcript.Model,
		Confidence: transcript.Confidence,
	}, outputTarget)
}

// deliverAssist uses the Assist Pipeline: Codeword → LLM → TTS → Text+Audio.
func (o desktopTranscriptOutput) deliverAssist(ctx context.Context, transcript speechkit.Transcript, target any) error {
	if o.assistPipeline == nil {
		// No assist pipeline — try legacy agent flow, or warn user.
		if o.agentFlow != nil {
			return o.deliverAgentFlow(ctx, transcript, target)
		}
		// Neither pipeline available — show feedback and fall back to clipboard paste.
		log.Printf("WARN: Assist mode active but no LLM provider configured — falling back to dictation output")
		if o.onAssistText != nil {
			o.onAssistText("No LLM provider configured. Check Settings > Provider.")
		}
		if o.handler == nil {
			return nil
		}
		outputTarget, _ := target.(output.Target)
		return o.handler.Handle(ctx, &stt.Result{
			Text:     transcript.Text,
			Language: transcript.Language,
			Duration: transcript.Duration,
			Provider: transcript.Provider,
		}, outputTarget)
	}

	result, err := o.assistPipeline.Process(ctx, transcript.Text, assist.ProcessOpts{
		Locale: transcript.Language,
	})
	if err != nil {
		log.Printf("assist pipeline error: %v", err)
		return err
	}

	if result.Action == "silent" {
		return nil
	}

	// Always deliver text to UI (speech bubble callback).
	if o.onAssistText != nil && result.Text != "" {
		o.onAssistText(result.Text)
	}

	// Play TTS audio if available.
	if o.audioPlayer != nil && len(result.Audio) > 0 {
		go func() {
			var playErr error
			switch result.Format {
			case "mp3":
				playErr = o.audioPlayer.PlayMP3(context.Background(), result.Audio)
			case "pcm", "wav":
				playErr = o.audioPlayer.PlayPCM(context.Background(), result.Audio, 24000)
			default:
				playErr = o.audioPlayer.PlayMP3(context.Background(), result.Audio)
			}
			if playErr != nil {
				log.Printf("TTS playback error: %v", playErr)
			}
		}()
	}

	// For shortcuts that need clipboard action, still paste the text.
	if result.Action == "shortcut" || result.Action == "execute" {
		// Show shortcut name in assist bubble for feedback.
		if o.onAssistText != nil {
			o.onAssistText(fmt.Sprintf("Shortcut: %s", result.Shortcut))
		}
		return nil
	}

	// Also paste the text to clipboard for "respond" action.
	if o.handler != nil && result.Text != "" {
		outputTarget, _ := target.(output.Target)
		return o.handler.Handle(ctx, &stt.Result{
			Text:     result.Text,
			Language: result.Locale,
			Provider: "assist",
		}, outputTarget)
	}

	return nil
}

// deliverAgentFlow uses the legacy agent Genkit flow (no TTS).
func (o desktopTranscriptOutput) deliverAgentFlow(ctx context.Context, transcript speechkit.Transcript, target any) error {
	if o.agentFlow == nil {
		return nil
	}

	resp, err := o.agentFlow.Run(ctx, flows.AgentInput{
		Utterance: transcript.Text,
		Locale:    transcript.Language,
	})
	if err != nil {
		log.Printf("agent flow error: %v", err)
		return err
	}
	if resp.Text == "" || resp.Action == "silent" {
		return nil
	}
	if o.handler == nil {
		return nil
	}
	outputTarget, _ := target.(output.Target)
	return o.handler.Handle(ctx, &stt.Result{
		Text:     resp.Text,
		Language: transcript.Language,
		Duration: transcript.Duration,
		Provider: "agent",
	}, outputTarget)
}
