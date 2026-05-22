package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/firebase/genkit/go/core"

	"github.com/kombifyio/SpeechKit/cmd/speechkit/internal/transcription"
	"github.com/kombifyio/SpeechKit/internal/ai/flows"
	"github.com/kombifyio/SpeechKit/internal/assist"
	"github.com/kombifyio/SpeechKit/internal/audio"
	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/output"
	"github.com/kombifyio/SpeechKit/internal/store"
	"github.com/kombifyio/SpeechKit/internal/stt"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

// routerTranscriber adapts the kernel's STT router (or a server-delegated
// equivalent — see server_delegates.go) to the speechkit.Transcript
// interface the orchestration code expects. The `router` field is typed
// as the small Transcriber interface so a *router.Router (in-process,
// pre-0.26 behaviour) and a *compositeTranscriber (server-first with
// optional local fallback) are both accepted.
type routerTranscriber struct {
	router          Transcriber
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
	entries := transcription.ParseVocabularyDictionary(rawDictionary)
	if t.dictionaryStore != nil {
		storedEntries, err := transcription.VocabularyEntriesFromStore(ctx, t.dictionaryStore, language)
		if err != nil {
			slog.Debug("load vocabulary dictionary from store", "err", err)
		} else if len(storedEntries) > 0 {
			entries = storedEntries
		}
	}

	result, err := t.router.Route(ctx, audioData, durationSecs, stt.TranscribeOpts{
		Language: language,
		Prompt:   transcription.BuildVocabularyPrompt(entries),
	})
	if err != nil {
		return speechkit.Transcript{}, err
	}
	correctedText, correctedTerms := transcription.ApplyVocabularyCorrectionsWithMatches(result.Text, entries)
	result.Text = correctedText
	if t.dictionaryStore != nil && len(correctedTerms) > 0 {
		languageForUsage := result.Language
		if languageForUsage == "" {
			languageForUsage = language
		}
		recordDictionaryUsageAsync(t.dictionaryStore, correctedTerms, languageForUsage) //nolint:contextcheck // fire-and-forget usage write; uses its own 2s timeout context
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

func recordDictionaryUsageAsync(dictionaryStore store.UserDictionaryStore, correctedTerms []string, language string) {
	if dictionaryStore == nil || len(correctedTerms) == 0 {
		return
	}

	terms := append([]string(nil), correctedTerms...)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		for _, term := range terms {
			if err := dictionaryStore.RecordUserDictionaryUsage(ctx, term, language); err != nil {
				slog.Debug("record dictionary usage", "term", term, "err", err)
			}
		}
	}()
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
	cfg              *config.Config
	state            *appState
	handler          output.OutputHandler
	interceptor      transcriptInterceptor
	activeMode       func() string
	agentMode        func() string     // "assist" or "voice_agent"
	onAssistText     func(text string) // Callback for UI (speech bubble)
	selectionCapture func(context.Context) (string, error)
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
			"No ready Voice Agent model is available. Open Settings > Voice Agent Mode and download a local model or choose a cloud provider.",
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

	processOpts := o.assistProcessOptions(ctx, transcript, target)
	if serverAssist := o.currentServerAssistDelegate(); serverAssist != nil {
		return o.deliverAssistViaServer(ctx, transcript, mode, processOpts, serverAssist)
	}
	return o.deliverAssistViaLocalPipeline(ctx, transcript, mode, processOpts)
}

func (o desktopTranscriptOutput) assistProcessOptions(ctx context.Context, transcript speechkit.Transcript, target any) assist.ProcessOpts {
	return assist.ProcessOpts{
		Locale:    transcript.Language,
		Selection: o.captureAssistSelection(ctx),
		Target:    target,
		// v0.38 multi-turn: single-user Device-Target uses a stable session
		// key so consecutive utterances within the SkillContextStore TTL
		// reach the same follow-up slot. The server-target derives this
		// from Identity.UserID; the device only has one user.
		SessionKey: "device",
	}
}

func (o desktopTranscriptOutput) deliverAssistViaLocalPipeline(ctx context.Context, transcript speechkit.Transcript, mode string, processOpts assist.ProcessOpts) error { //nolint:contextcheck // playbackCtx for TTS goroutine is app-scoped, not request ctx (goroutine outlives Deliver)
	assistPipeline := o.currentAssistPipeline()
	if assistPipeline == nil {
		return o.deliverAssistWithoutPipeline(ctx, transcript, mode)
	}

	if !assistPipeline.HasDirectReplyModel() && !assistPipeline.CanHandleWithoutDirectReplyModel(transcript.Text, processOpts) {
		o.presentAssistModelMissingHint()
		return nil
	}

	result, err := assistPipeline.Process(ctx, transcript.Text, processOpts)
	if err != nil {
		slog.Error("assist pipeline error", "err", err)
		o.failConversation(mode, "", friendlyConversationError(mode, err))
		return err
	}
	return o.deliverAssistResult(ctx, transcript, mode, result, "TTS playback error")
}

func (o desktopTranscriptOutput) deliverAssistWithoutPipeline(ctx context.Context, transcript speechkit.Transcript, mode string) error {
	if o.currentAgentFlow() != nil {
		return o.deliverAgentFlow(ctx, transcript, mode)
	}
	slog.Warn("assist mode active but no LLM provider configured")
	o.failConversation(mode, transcript.Text, "No LLM provider configured. Check Settings > Provider.")
	return nil
}

func (o desktopTranscriptOutput) deliverAssistResult(ctx context.Context, transcript speechkit.Transcript, mode string, result *assist.Result, playbackErrorLog string) error {
	if result == nil {
		return nil
	}
	if result.Action == "silent" {
		return nil
	}

	prompterPanelSurface := o.presentAssistResult(transcript, mode, result)
	o.playAssistAudio(ctx, result, prompterPanelSurface, playbackErrorLog)
	return nil
}

func (o desktopTranscriptOutput) presentAssistResult(transcript speechkit.Transcript, mode string, result *assist.Result) bool {
	assistantText := assistResultText(result)
	assistPanelSurface, prompterPanelSurface := assistResultPanelSurfaces(result, mode)
	if assistPanelSurface {
		if o.state != nil && assistantText != "" {
			o.state.showAssistPanel(transcript.Text, assistantText)
		}
	} else if prompterPanelSurface {
		o.startConversation(mode, transcript.Text)
		nextState := "ready"
		if len(result.Audio) > 0 {
			nextState = "speaking"
		}
		o.finishConversation(assistantText, nextState)
	} else if result.Kind != assist.ResultKindUtilityAction && assistantText != "" && o.onAssistText != nil {
		o.onAssistText(assistantText)
	}
	return prompterPanelSurface
}

func assistResultText(result *assist.Result) string {
	if result.Text != "" {
		return result.Text
	}
	if result.Shortcut != "" {
		return fmt.Sprintf("Shortcut: %s", result.Shortcut)
	}
	return ""
}

func assistResultPanelSurfaces(result *assist.Result, mode string) (bool, bool) {
	panelSurface := result.Surface == "" || result.Surface == assist.ResultSurfacePanel
	assistPanelSurface := panelSurface && mode == modeAssist
	prompterPanelSurface := panelSurface && !assistPanelSurface
	return assistPanelSurface, prompterPanelSurface
}

func (o desktopTranscriptOutput) playAssistAudio(ctx context.Context, result *assist.Result, prompterPanelSurface bool, playbackErrorLog string) {
	if audioPlayer := o.currentAudioPlayer(); audioPlayer != nil && len(result.Audio) > 0 {
		playCtx := o.assistPlaybackContext(ctx)
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
				slog.Error(playbackErrorLog, "err", playErr)
				if prompterPanelSurface && o.state != nil {
					o.state.updatePrompterState("error")
				}
				return
			}
			if prompterPanelSurface && o.state != nil {
				o.state.updatePrompterState("ready")
			}
		}()
	}
}

func (o desktopTranscriptOutput) assistPlaybackContext(_ context.Context) context.Context {
	if o.playbackCtx != nil {
		return o.playbackCtx
	}
	return context.Background()
}

func (o desktopTranscriptOutput) presentAssistModelMissingHint() {
	const message = "Assist can't answer because no ready Assist model is available. Open Settings > Assist Mode and download a local model or choose a cloud provider."
	slog.Warn("assist direct reply requested but no ready Assist model is available")
	if o.state != nil {
		o.state.addLog(message, "warn")
		o.state.showAssistBubble(message)
	}
}

func (o desktopTranscriptOutput) captureAssistSelection(ctx context.Context) string {
	capture := o.selectionCapture
	if capture == nil {
		capture = output.CaptureSelectedText
	}
	selection, err := capture(ctx)
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
		o.failConversation(mode, "", friendlyConversationError(mode, err))
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

// currentServerAssistDelegate returns the server-delegated Assist
// processor when one is configured (mode_source = "server"), nil
// otherwise. The orchestration code branches on this to skip the
// in-process Pipeline entirely.
func (o desktopTranscriptOutput) currentServerAssistDelegate() assistServerDelegate {
	if o.state == nil {
		return nil
	}
	o.state.mu.Lock()
	defer o.state.mu.Unlock()
	if o.state.serverDelegates == nil || !o.state.serverDelegates.hasAssist() {
		return nil
	}
	return o.state.serverDelegates.assist
}

// assistServerDelegate is the small interface every server-delegated
// Assist processor satisfies. Re-declared here so this file does not
// depend on internal/serverclient's concrete type — easier to mock in
// tests and prevents accidental import cycles.
type assistServerDelegate interface {
	Process(ctx context.Context, transcript string, opts assist.ProcessOpts) (*assist.Result, error)
}

// deliverAssistViaServer executes the Assist call against a remote
// SpeechKit server. The Result shape is identical to the local pipeline
// so the post-Process surface logic (panel display, TTS playback, etc.)
// is shared via the same downstream helpers. Errors are surfaced through
// the same conversation-error UI path the local pipeline uses.
func (o desktopTranscriptOutput) deliverAssistViaServer(ctx context.Context, transcript speechkit.Transcript, mode string, processOpts assist.ProcessOpts, server assistServerDelegate) error { //nolint:contextcheck // playbackCtx is app-scoped, intentionally not request ctx
	result, err := server.Process(ctx, transcript.Text, processOpts)
	if err != nil {
		slog.Error("server-delegated assist error", "err", err)
		o.failConversation(mode, "", friendlyConversationError(mode, err))
		return err
	}
	return o.deliverAssistResult(ctx, transcript, mode, result, "TTS playback error (server delegate)")
}
