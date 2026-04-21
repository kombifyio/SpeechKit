package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/firebase/genkit/go/core"

	"github.com/kombifyio/SpeechKit/internal/ai/flows"
	"github.com/kombifyio/SpeechKit/internal/assist"
	"github.com/kombifyio/SpeechKit/internal/audio"
	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/output"
	"github.com/kombifyio/SpeechKit/internal/router"
	"github.com/kombifyio/SpeechKit/internal/store"
	"github.com/kombifyio/SpeechKit/internal/stt"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

type routerTranscriber struct {
	router          *router.Router
	state           *appState
	dictionaryStore store.UserDictionaryStore
}

func (t routerTranscriber) Transcribe(ctx context.Context, audioData []byte, durationSecs float64, language string) (speechkit.Transcript, error) {
	if t.router == nil {
		return speechkit.Transcript{}, fmt.Errorf("router not configured")
	}

	rawDictionary := ""
	if t.state != nil {
		t.state.mu.Lock()
		rawDictionary = t.state.vocabularyDictionary
		t.state.mu.Unlock()
	}
	entries := parseVocabularyDictionary(rawDictionary)

	result, err := t.router.Route(ctx, audioData, durationSecs, stt.TranscribeOpts{
		Language: language,
		Prompt:   buildVocabularyPrompt(entries),
	})
	if err != nil {
		return speechkit.Transcript{}, err
	}
	correctedText, correctedTerms := applyVocabularyCorrectionsWithMatches(result.Text, entries)
	result.Text = correctedText
	if t.dictionaryStore != nil {
		languageForUsage := result.Language
		if languageForUsage == "" {
			languageForUsage = language
		}
		for _, term := range correctedTerms {
			if err := t.dictionaryStore.RecordUserDictionaryUsage(ctx, term, languageForUsage); err != nil {
				slog.Debug("record dictionary usage", "term", term, "err", err)
			}
		}
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
	cfg          *config.Config
	state        *appState
	handler      output.OutputHandler
	interceptor  transcriptInterceptor
	activeMode   func() string
	agentMode    func() string     // "assist" or "voice_agent"
	onAssistText func(text string) // Callback for UI (speech bubble)
	// playbackCtx scopes long-running TTS playback goroutines to the app's
	// lifecycle. Cancelled on shutdown so in-flight audio stops promptly
	// instead of holding the process open. Callers may leave this nil (e.g.
	// tests) in which case playback falls back to context.Background().
	playbackCtx context.Context
}

func (o desktopTranscriptOutput) Deliver(ctx context.Context, transcript speechkit.Transcript, target any) error {
	mode := o.currentMode()

	switch mode {
	case modeAssist:
		return o.deliverAssist(ctx, transcript, target)
	case modeVoiceAgent:
		return o.deliverVoiceAgentFallback(ctx, transcript, target)
	case modeDictate, modeNone:
		return o.deliverPassthrough(ctx, transcript, target)
	default:
		return o.deliverAgentFlow(ctx, transcript, modeAssist)
	}
}

func (o desktopTranscriptOutput) currentMode() string {
	legacyAgentMode := modeAssist
	if o.agentMode != nil {
		legacyAgentMode = normalizeAgentMode(o.agentMode())
	}
	if o.activeMode == nil {
		return modeNone
	}
	return normalizeRuntimeMode(o.activeMode(), legacyAgentMode)
}

func (o desktopTranscriptOutput) startConversation(mode, userText string) {
	if o.state == nil {
		return
	}
	o.state.showPrompterWindowForMode(mode, false)
	if userText != "" {
		o.state.sendPrompterMessage("user", userText, true)
	}
	o.state.updatePrompterState("processing")
}

func (o desktopTranscriptOutput) finishConversation(text, state string) {
	if o.state == nil {
		return
	}
	if text != "" {
		o.state.sendPrompterMessage("assistant", text, true)
	}
	o.state.updatePrompterState(state)
}

func (o desktopTranscriptOutput) failConversation(mode, userText, errText string) {
	if o.state == nil {
		return
	}
	o.state.showPrompterWindowForMode(mode, false)
	if userText != "" {
		o.state.sendPrompterMessage("user", userText, true)
	}
	if errText != "" {
		o.state.sendPrompterMessage("assistant", errText, true)
	}
	o.state.updatePrompterState("error")
}

func (o desktopTranscriptOutput) deliverVoiceAgentFallback(ctx context.Context, transcript speechkit.Transcript, target any) error {
	_ = ctx
	_ = target

	if o.currentAgentFlow() == nil {
		slog.Warn("voice agent pipeline fallback active but no agent flow configured")
		o.failConversation(
			modeVoiceAgent,
			transcript.Text,
			"No Voice Agent model configured. Check Settings > Models and select a Voice Agent model.",
		)
		return nil
	}
	return o.deliverAgentFlow(ctx, transcript, modeVoiceAgent)
}

// deliverAssist uses the Assist Pipeline: Codeword → LLM → TTS → Text+Audio.
func (o desktopTranscriptOutput) deliverAssist(ctx context.Context, transcript speechkit.Transcript, target any) error { //nolint:contextcheck // playbackCtx for TTS goroutine is app-scoped, not request ctx (goroutine outlives Deliver)
	return o.deliverAssistForMode(ctx, transcript, modeAssist, target)
}

func (o desktopTranscriptOutput) deliverAssistForMode(ctx context.Context, transcript speechkit.Transcript, mode string, target any) error { //nolint:contextcheck // playbackCtx for TTS goroutine is app-scoped, not request ctx (goroutine outlives Deliver)
	if strings.TrimSpace(transcript.Text) == "" {
		slog.Debug("assist mode ignored empty transcript")
		return nil
	}

	assistPipeline := o.currentAssistPipeline()
	if assistPipeline == nil {
		// No assist pipeline — try legacy agent flow, or warn user.
		if o.currentAgentFlow() != nil {
			return o.deliverAgentFlow(ctx, transcript, mode)
		}
		slog.Warn("assist mode active but no LLM provider configured")
		o.failConversation(mode, transcript.Text, "No LLM provider configured. Check Settings > Provider.")
		return nil
	}

	selection := o.captureAssistSelection(ctx)
	processOpts := assist.ProcessOpts{
		Locale:    transcript.Language,
		Selection: selection,
		Target:    target,
	}
	if !assistPipeline.HasDirectReplyModel() && !assistPipeline.CanHandleWithoutDirectReplyModel(transcript.Text, processOpts) {
		o.presentAssistModelMissingHint()
		return nil
	}

	result, err := assistPipeline.Process(ctx, transcript.Text, processOpts)
	if err != nil {
		slog.Error("assist pipeline error", "err", err)
		o.failConversation(mode, "", friendlyConversationError(o.cfg, mode, err))
		return err
	}

	if result.Action == "silent" {
		return nil
	}

	assistantText := result.Text
	if assistantText == "" && result.Shortcut != "" {
		assistantText = fmt.Sprintf("Shortcut: %s", result.Shortcut)
	}

	panelSurface := result.Surface == "" || result.Surface == assist.ResultSurfacePanel
	if panelSurface {
		o.startConversation(mode, transcript.Text)
		nextState := "ready"
		if len(result.Audio) > 0 {
			nextState = "speaking"
		}
		o.finishConversation(assistantText, nextState)
	} else if result.Kind != assist.ResultKindUtilityAction && assistantText != "" && o.onAssistText != nil {
		o.onAssistText(assistantText)
	}

	// Play TTS audio if available. The goroutine outlives Deliver(), so it
	// must not take the caller's ctx (which will be cancelled when Deliver
	// returns). Use the app-scoped playbackCtx so shutdown still interrupts
	// playback; tests that leave playbackCtx nil fall back to Background.
	if audioPlayer := o.currentAudioPlayer(); audioPlayer != nil && len(result.Audio) > 0 {
		playCtx := o.playbackCtx
		if playCtx == nil {
			playCtx = context.Background()
		}
		audioData := result.Audio
		audioFormat := result.Format
		go func() { //nolint:contextcheck // playbackCtx is app-scoped and intentionally not the request ctx, which would cancel when Deliver() returns
			var playErr error
			switch audioFormat {
			case "pcm", "wav":
				playErr = audioPlayer.PlayPCM(playCtx, audioData, 24000)
			default:
				playErr = audioPlayer.PlayMP3(playCtx, audioData)
			}
			if playErr != nil && playCtx.Err() == nil {
				slog.Error("TTS playback error", "err", playErr)
				if panelSurface && o.state != nil {
					o.state.updatePrompterState("error")
				}
				return
			}
			if panelSurface && o.state != nil {
				o.state.updatePrompterState("ready")
			}
		}()
	}

	return nil
}

func (o desktopTranscriptOutput) presentAssistModelMissingHint() {
	const message = "Assist can't answer because no Assist model is configured. Open Settings > Models and select an Assist model."
	slog.Warn("assist direct reply requested but no Assist model is configured")
	if o.state != nil {
		o.state.addLog(message, "warn")
		o.state.showAssistBubble(message)
	}
}

func (o desktopTranscriptOutput) captureAssistSelection(ctx context.Context) string {
	selection, err := output.CaptureSelectedText(ctx)
	if err != nil {
		slog.Debug("assist selection capture failed", "err", err)
		return ""
	}
	return selection
}

func (o desktopTranscriptOutput) deliverPassthrough(ctx context.Context, transcript speechkit.Transcript, target any) error {
	if o.handler == nil {
		return nil
	}

	return o.handler.Handle(ctx, &stt.Result{
		Text:       transcript.Text,
		Language:   transcript.Language,
		Duration:   transcript.Duration,
		Provider:   transcript.Provider,
		Model:      transcript.Model,
		Confidence: transcript.Confidence,
	}, outputTarget(target))
}

// deliverAgentFlow uses the legacy agent Genkit flow (no TTS).
func (o desktopTranscriptOutput) deliverAgentFlow(ctx context.Context, transcript speechkit.Transcript, mode string) error {
	agentFlow := o.currentAgentFlow()
	if agentFlow == nil {
		return nil
	}

	if mode == modeVoiceAgent && o.state != nil {
		o.state.recordVoiceAgentDialogTurn("user", transcript.Text, true)
	}
	o.startConversation(mode, transcript.Text)
	resp, err := agentFlow.Run(ctx, flows.AgentInput{
		Utterance: transcript.Text,
		Locale:    transcript.Language,
	})
	if err != nil {
		slog.Error("agent flow error", "err", err)
		o.failConversation(mode, "", friendlyConversationError(o.cfg, mode, err))
		return err
	}
	if resp.Text == "" || resp.Action == "silent" {
		o.finishConversation("", "ready")
		return nil
	}
	if mode == modeVoiceAgent && o.state != nil {
		o.state.recordVoiceAgentDialogTurn("assistant", resp.Text, true)
	}
	o.finishConversation(resp.Text, "ready")
	return nil
}

func (o desktopTranscriptOutput) currentAssistPipeline() *assist.Pipeline {
	if o.state == nil {
		return nil
	}
	o.state.mu.Lock()
	defer o.state.mu.Unlock()
	return o.state.assistPipeline
}

func (o desktopTranscriptOutput) currentAgentFlow() *core.Flow[flows.AgentInput, flows.AgentOutput, struct{}] {
	if o.state == nil {
		return nil
	}
	o.state.mu.Lock()
	defer o.state.mu.Unlock()
	return o.state.agentFlow
}

func (o desktopTranscriptOutput) currentAudioPlayer() *audio.Player {
	if o.state == nil {
		return nil
	}
	o.state.mu.Lock()
	defer o.state.mu.Unlock()
	return o.state.audioPlayer
}
