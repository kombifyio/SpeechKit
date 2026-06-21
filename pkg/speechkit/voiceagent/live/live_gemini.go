package live

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"google.golang.org/genai"
)

type geminiLiveSession interface {
	SendRealtimeInput(input genai.LiveRealtimeInput) error
	SendToolResponse(input genai.LiveToolResponseInput) error
	Receive() (*genai.LiveServerMessage, error)
	Close() error
}

// GeminiLive implements LiveProvider using the Google GenAI Live API.
type GeminiLive struct {
	mu         sync.RWMutex
	client     *genai.Client
	session    geminiLiveSession
	resume     *resumeHandle // Session resumption handle: DPAPI-at-rest on Windows, TTL-bounded everywhere.
	lastConfig *LiveConfig   // Stored for reconnection
}

// defaultGeminiLiveModel is the kernel-level default for realtime sessions
// when the caller does not specify a Model in LiveConfig. It tracks the
// current Gemini Live family primary model.
//
// As of April 2026 this is gemini-3.1-flash-live-preview. When Google
// promotes a model to GA or publishes a newer preview, bump this constant
// and update both defaults in internal/config/config.go
// (defaultGeminiNativeAudioModel / fallbackGeminiNativeAudioModel) so the
// whole Framework picks up the new default without breaking running
// deployments that pinned an explicit LiveConfig.Model.
const defaultGeminiLiveModel = "gemini-3.1-flash-live-preview"

// NewGeminiLive creates a Gemini Live provider.
func NewGeminiLive() *GeminiLive {
	return &GeminiLive{
		resume: newResumeHandle(),
	}
}

func (g *GeminiLive) Connect(ctx context.Context, cfg LiveConfig) error {
	if err := validateGeminiLiveConfig(cfg); err != nil {
		return err
	}

	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:      cfg.APIKey,
		Backend:     genai.BackendGeminiAPI,
		HTTPOptions: buildGeminiLiveHTTPOptions(cfg),
	})
	if err != nil {
		return fmt.Errorf("gemini live: create client: %w", err)
	}
	g.client = client

	connectCfg := buildGeminiLiveConnectConfig(cfg)

	// Build the candidate list: primary model first, then optional fallback.
	// shouldTryFallback decides whether the fallback is distinct enough from
	// the primary to be worth attempting; the function is tested in isolation
	// because the genai SDK is awkward to mock end-to-end.
	primary := resolvedGeminiLiveModel(cfg)
	candidates := []string{primary}
	if fallback := strings.TrimSpace(cfg.FallbackModel); shouldTryFallback(primary, fallback) {
		candidates = append(candidates, fallback)
	}

	var lastErr error
	for i, model := range candidates {
		session, err := client.Live.Connect(ctx, model, connectCfg)
		if err == nil {
			g.mu.Lock()
			g.session = session
			g.lastConfig = &cfg
			g.mu.Unlock()
			if i == 0 {
				slog.Info("Gemini Live connected",
					"model", model,
					"voice", connectCfg.SpeechConfig.VoiceConfig.PrebuiltVoiceConfig.VoiceName,
					// configured_region is logged for compliance evidence only.
					// Gemini Live (May 2026) uses a single global endpoint; the
					// region field does NOT redirect traffic. Data residency is
					// controlled at the Google Cloud project level.
					// See docs/compliance/byok-gemini-region-pinning.md.
					"configured_region", cfg.Region,
				)
			} else {
				slog.Warn("Gemini Live connected via fallback model",
					"primary", candidates[0],
					"fallback", model,
					"primary_err", lastErr,
					"voice", connectCfg.SpeechConfig.VoiceConfig.PrebuiltVoiceConfig.VoiceName,
					"configured_region", cfg.Region,
				)
			}
			return nil
		}
		lastErr = err
		if i+1 < len(candidates) {
			slog.Warn("Gemini Live primary connect failed; trying fallback",
				"primary", model,
				"err", err,
			)
		}
	}
	return fmt.Errorf("gemini live: connect to %v: %w", candidates, lastErr)
}

// shouldTryFallback reports whether a fallback model is worth attempting
// when the primary fails. The fallback must be non-empty and different
// from the primary; otherwise the second attempt would just re-trigger
// the same failure.
func shouldTryFallback(primary, fallback string) bool {
	fallback = strings.TrimSpace(fallback)
	if fallback == "" {
		return false
	}
	return strings.TrimSpace(primary) != fallback
}

func (g *GeminiLive) SendAudio(chunk []byte) error {
	g.mu.RLock()
	session := g.session
	g.mu.RUnlock()
	if session == nil {
		return fmt.Errorf("gemini live: not connected")
	}

	return session.SendRealtimeInput(genai.LiveRealtimeInput{
		Audio: &genai.Blob{
			MIMEType: "audio/pcm;rate=16000",
			Data:     chunk,
		},
	})
}

func (g *GeminiLive) SendAudioStreamEnd() error {
	g.mu.RLock()
	session := g.session
	g.mu.RUnlock()
	if session == nil {
		return fmt.Errorf("gemini live: not connected")
	}

	return session.SendRealtimeInput(genai.LiveRealtimeInput{
		AudioStreamEnd: true,
	})
}

func (g *GeminiLive) Receive(ctx context.Context) (*LiveMessage, error) {
	g.mu.RLock()
	session := g.session
	g.mu.RUnlock()
	if session == nil {
		return nil, fmt.Errorf("gemini live: not connected")
	}

	// genai.Session.Receive() blocks until a message arrives.
	// Context cancellation is handled by the caller closing the session/WebSocket.
	resp, err := session.Receive()
	if err != nil {
		return nil, fmt.Errorf("gemini live: receive: %w", err)
	}

	msg := &LiveMessage{}

	if resp.ServerContent != nil {
		if resp.ServerContent.ModelTurn != nil {
			for _, part := range resp.ServerContent.ModelTurn.Parts {
				if part.InlineData != nil && len(part.InlineData.Data) > 0 {
					msg.Audio = append(msg.Audio, part.InlineData.Data...)
				}
				if part.Text != "" {
					msg.Text += part.Text
				}
			}
		}
		msg.Done = resp.ServerContent.TurnComplete

		// Transcription fields.
		if resp.ServerContent.InputTranscription != nil {
			msg.InputTranscript = resp.ServerContent.InputTranscription.Text
			msg.InputTranscriptDone = resp.ServerContent.InputTranscription.Finished
		}
		if resp.ServerContent.OutputTranscription != nil {
			msg.OutputTranscript = resp.ServerContent.OutputTranscription.Text
			msg.OutputTranscriptDone = resp.ServerContent.OutputTranscription.Finished
		}

		// Barge-in: user interrupted model.
		if resp.ServerContent.Interrupted {
			msg.Interrupted = true
		}
	}

	if resp.ToolCall != nil {
		for _, call := range resp.ToolCall.FunctionCalls {
			if call == nil {
				continue
			}
			msg.ToolCalls = append(msg.ToolCalls, ToolCall{
				ID:   call.ID,
				Name: call.Name,
				Args: call.Args,
			})
		}
	}

	if resp.ToolCallCancellation != nil {
		msg.ToolCallCancellationIDs = append(msg.ToolCallCancellationIDs, resp.ToolCallCancellation.IDs...)
	}

	// GoAway: server signals imminent session end.
	if resp.GoAway != nil {
		msg.GoAway = true
		slog.Warn("Gemini Live GoAway received — session will end soon")
	}

	// Session resumption: store handle for reconnection.
	// The handle is kept in a DPAPI-protected container with a TTL so a stale
	// memory dump cannot replay old sessions indefinitely.
	if resp.SessionResumptionUpdate != nil && resp.SessionResumptionUpdate.NewHandle != "" {
		g.resume.Set(resp.SessionResumptionUpdate.NewHandle)
	}

	return msg, nil
}

func (g *GeminiLive) SendText(text string) error {
	g.mu.RLock()
	session := g.session
	g.mu.RUnlock()
	if session == nil {
		return fmt.Errorf("gemini live: not connected")
	}

	return session.SendRealtimeInput(genai.LiveRealtimeInput{
		Text: text,
	})
}

func (g *GeminiLive) SendToolResponse(response ToolResponse) error {
	g.mu.RLock()
	session := g.session
	g.mu.RUnlock()
	if session == nil {
		return fmt.Errorf("gemini live: not connected")
	}

	return session.SendToolResponse(genai.LiveToolResponseInput{
		FunctionResponses: []*genai.FunctionResponse{
			{
				ID:           response.ID,
				Name:         response.Name,
				Response:     response.Response,
				Scheduling:   mapToolResponseScheduling(response.Scheduling),
				WillContinue: response.WillContinue,
			},
		},
	})
}

// Reconnect re-establishes the session using the stored resumption handle.
// If the handle has expired (TTL) or been cleared, a fresh session is opened.
func (g *GeminiLive) Reconnect(ctx context.Context) error {
	g.mu.RLock()
	lastCfg := g.lastConfig
	g.mu.RUnlock()
	resumeHandleValue := g.resume.Get()

	if lastCfg == nil {
		return fmt.Errorf("gemini live: no stored config for reconnect")
	}
	if err := validateGeminiLiveConfig(*lastCfg); err != nil {
		return err
	}

	// Close existing session. Log, but do not abort the reconnect — the old
	// session may already be half-dead on the wire, and aborting here would
	// leave the caller without a working session at all.
	if err := g.Close(); err != nil {
		slog.Warn("gemini live: close prior session during reconnect", "err", err)
	}

	// Re-create client.
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:      lastCfg.APIKey,
		Backend:     genai.BackendGeminiAPI,
		HTTPOptions: buildGeminiLiveHTTPOptions(*lastCfg),
	})
	if err != nil {
		return fmt.Errorf("gemini live: reconnect create client: %w", err)
	}

	model := resolvedGeminiLiveModel(*lastCfg)

	connectCfg := buildGeminiLiveConnectConfig(*lastCfg)
	connectCfg.SessionResumption = buildGeminiLiveSessionResumptionConfig(resumeHandleValue)

	session, err := client.Live.Connect(ctx, model, connectCfg)
	if err != nil {
		return fmt.Errorf("gemini live: reconnect to %s: %w", model, err)
	}

	g.mu.Lock()
	g.client = client
	g.session = session
	g.mu.Unlock()

	slog.Info("Gemini Live reconnected", "model", model, "had_resume_handle", resumeHandleValue != "")
	return nil
}

func (g *GeminiLive) Close() error {
	g.mu.Lock()
	session := g.session
	g.session = nil
	g.client = nil
	g.mu.Unlock()

	// Wipe the resumption handle on close. Any caller that intends to
	// reconnect must have already captured it via Reconnect() before calling
	// Close(); keeping it around after a completed session only widens the
	// window for misuse if memory is later inspected.
	if g.resume != nil {
		g.resume.Clear()
	}

	if session != nil {
		if err := session.Close(); err != nil {
			return fmt.Errorf("gemini live: close session: %w", err)
		}
	}
	return nil
}

func (g *GeminiLive) Name() string { return "gemini-live" }

func buildGeminiLiveConnectConfig(cfg LiveConfig) *genai.LiveConnectConfig {
	resolved := ResolveLiveOptions("google", "realtime.google.gemini-native-audio", cfg, nil, nil)
	policies := normalizeLivePolicies(cfg.Policies)
	if resolved.HasTurnDetectionOverride() && !resolved.TurnDetection {
		policies.ActivityDetection.Automatic = false
	}
	voiceName := resolved.Voice
	if voiceName == "" {
		voiceName = "Kore"
	}

	connectCfg := &genai.LiveConnectConfig{
		ResponseModalities: []genai.Modality{genai.ModalityAudio},
		SpeechConfig: &genai.SpeechConfig{
			VoiceConfig: &genai.VoiceConfig{
				PrebuiltVoiceConfig: &genai.PrebuiltVoiceConfig{
					VoiceName: voiceName,
				},
			},
		},
		SystemInstruction: &genai.Content{
			Parts: []*genai.Part{genai.NewPartFromText(buildInstructionText(cfg))},
		},
		SessionResumption: buildGeminiLiveSessionResumptionConfig(""),
		RealtimeInputConfig: &genai.RealtimeInputConfig{
			AutomaticActivityDetection: &genai.AutomaticActivityDetection{
				Disabled:                 !policies.ActivityDetection.Automatic,
				StartOfSpeechSensitivity: mapStartSensitivity(policies.ActivityDetection.StartSensitivity),
				EndOfSpeechSensitivity:   mapEndSensitivity(policies.ActivityDetection.EndSensitivity),
			},
			ActivityHandling: mapActivityHandling(policies.ActivityDetection.ActivityHandling),
			TurnCoverage:     mapTurnCoverage(policies.ActivityDetection.TurnCoverage),
		},
		Tools: toolDefinitionsToGenAI(cfg.Tools),
	}

	if policies.EnableInputAudioTranscription {
		connectCfg.InputAudioTranscription = &genai.AudioTranscriptionConfig{}
	}
	if policies.EnableOutputAudioTranscription {
		connectCfg.OutputAudioTranscription = &genai.AudioTranscriptionConfig{}
	}
	if policies.EnableAffectiveDialog {
		enable := true
		connectCfg.EnableAffectiveDialog = &enable
	}
	if policies.Thinking.Enabled {
		connectCfg.ThinkingConfig = &genai.ThinkingConfig{
			IncludeThoughts: policies.Thinking.IncludeThoughts,
			ThinkingLevel:   mapThinkingLevel(policies.Thinking.ThinkingLevel),
		}
		if policies.Thinking.ThinkingBudget > 0 {
			budget := policies.Thinking.ThinkingBudget
			connectCfg.ThinkingConfig.ThinkingBudget = &budget
		}
	}
	if policies.ContextCompression.Enabled {
		trigger := policies.ContextCompression.TriggerTokens
		target := policies.ContextCompression.TargetTokens
		connectCfg.ContextWindowCompression = &genai.ContextWindowCompressionConfig{
			TriggerTokens: &trigger,
			SlidingWindow: &genai.SlidingWindow{
				TargetTokens: &target,
			},
		}
	}

	if aad := connectCfg.RealtimeInputConfig.AutomaticActivityDetection; aad != nil {
		if policies.ActivityDetection.PrefixPaddingMs > 0 {
			v := policies.ActivityDetection.PrefixPaddingMs
			aad.PrefixPaddingMs = &v
		}
		if policies.ActivityDetection.SilenceDurationMs > 0 {
			v := policies.ActivityDetection.SilenceDurationMs
			aad.SilenceDurationMs = &v
		}
	}

	return connectCfg
}

func buildGeminiLiveSessionResumptionConfig(handle string) *genai.SessionResumptionConfig {
	return &genai.SessionResumptionConfig{
		Handle: strings.TrimSpace(handle),
	}
}

func buildInstructionText(cfg LiveConfig) string {
	instruction := resolvedFrameworkPrompt(cfg)
	if refinement := strings.TrimSpace(cfg.RefinementPrompt); refinement != "" {
		instruction += "\n\nPersonal refinement:\n" + refinement +
			"\n\nApply this personal refinement when it does not conflict with higher-priority framework or host instructions."
	}
	if hint := strings.TrimSpace(cfg.VocabularyHint); hint != "" {
		instruction += "\n\nVocabulary and proper nouns:\n" + hint
	}
	if localeGuide := preferredLocaleInstruction(cfg.Locale); localeGuide != "" {
		instruction += "\n\n" + localeGuide
	}
	return instruction
}

func resolvedFrameworkPrompt(cfg LiveConfig) string {
	instruction := strings.TrimSpace(cfg.FrameworkPrompt)
	if instruction == "" {
		instruction = defaultVoiceAgentInstruction(cfg.Locale)
	}
	return instruction
}

func defaultVoiceAgentInstruction(locale string) string {
	switch locale {
	case "de", "de-DE":
		return strings.TrimSpace(`Du bist der Voice Agent von SpeechKit.
Du hilfst allgemein, freundlich, klar und zuegig.
Fuehre natuerliche Gespraeche, beantworte Fragen knapp und verstaendlich und stelle kurze Rueckfragen, wenn wichtige Informationen fehlen.
Arbeite ergebnisorientiert: Wenn der Nutzer diskutiert, plant oder ein Problem analysiert, fasse Zwischenergebnisse sauber zusammen und extrahiere auf Wunsch das konkrete Ergebnis, die Entscheidung oder die naechsten Schritte.
Halte Antworten standardmaessig gut sprechbar und nicht laenger als noetig.
Wenn der Host dir weitere Anweisungen oder Werkzeuge mitgibt, befolge diese vorrangig.`)
	default:
		return strings.TrimSpace(`You are the SpeechKit Voice Agent.
You provide general-purpose help in a natural, concise, and supportive way.
Hold fluid spoken conversations, answer clearly, and ask short follow-up questions when important information is missing.
Stay outcome-oriented: when the user is discussing, planning, or analyzing something, help structure the conversation and extract clear conclusions, decisions, or next steps when useful.
Keep responses easy to speak and usually no longer than necessary.
If the host supplies additional instructions or tools, follow them as the higher-priority guide.`)
	}
}

func preferredLocaleInstruction(locale string) string {
	locale = strings.TrimSpace(locale)
	if locale == "" {
		return ""
	}
	switch strings.ToLower(locale) {
	case "de", "de-de":
		return "Bevorzuge Deutsch fuer die Unterhaltung. Wenn der Nutzer bewusst die Sprache wechselt, folge dem Nutzer."
	case "en", "en-us", "en-gb":
		return "Prefer English for the conversation. If the user clearly switches languages, follow the user."
	default:
		return fmt.Sprintf("Prefer %s for the conversation unless the user clearly switches languages.", locale)
	}
}

func normalizeLivePolicies(policies LivePolicies) LivePolicies {
	if !policies.EnableInputAudioTranscription && !policies.EnableOutputAudioTranscription &&
		!policies.EnableAffectiveDialog && !policies.Thinking.Enabled &&
		!policies.ContextCompression.Enabled && !policies.ActivityDetection.Automatic &&
		policies.ActivityDetection.StartSensitivity == "" && policies.ActivityDetection.EndSensitivity == "" &&
		policies.ActivityDetection.PrefixPaddingMs == 0 && policies.ActivityDetection.SilenceDurationMs == 0 &&
		policies.ActivityDetection.ActivityHandling == "" && policies.ActivityDetection.TurnCoverage == "" {
		return defaultLivePolicies()
	}

	if policies.ActivityDetection.StartSensitivity == "" {
		policies.ActivityDetection.StartSensitivity = StartSensitivityLow
	}
	if policies.ActivityDetection.EndSensitivity == "" {
		policies.ActivityDetection.EndSensitivity = EndSensitivityLow
	}
	if policies.ActivityDetection.PrefixPaddingMs == 0 {
		policies.ActivityDetection.PrefixPaddingMs = 100
	}
	if policies.ActivityDetection.SilenceDurationMs == 0 {
		policies.ActivityDetection.SilenceDurationMs = 700
	}
	if policies.ActivityDetection.ActivityHandling == "" {
		policies.ActivityDetection.ActivityHandling = ActivityHandlingStartOfActivityInterrupts
	}
	if policies.ActivityDetection.TurnCoverage == "" {
		policies.ActivityDetection.TurnCoverage = TurnCoverageTurnIncludesOnlyActivity
	}
	if !policies.ContextCompression.Enabled {
		policies.ContextCompression.Enabled = true
	}
	if policies.ContextCompression.TriggerTokens == 0 {
		policies.ContextCompression.TriggerTokens = 12000
	}
	if policies.ContextCompression.TargetTokens == 0 {
		policies.ContextCompression.TargetTokens = 6000
	}
	return policies
}

func defaultLivePolicies() LivePolicies {
	return LivePolicies{
		EnableInputAudioTranscription:  true,
		EnableOutputAudioTranscription: true,
		ContextCompression: ContextCompressionPolicy{
			Enabled:       true,
			TriggerTokens: 12000,
			TargetTokens:  6000,
		},
		ActivityDetection: ActivityDetectionPolicy{
			Automatic:         true,
			StartSensitivity:  StartSensitivityLow,
			EndSensitivity:    EndSensitivityLow,
			PrefixPaddingMs:   100,
			SilenceDurationMs: 700,
			ActivityHandling:  ActivityHandlingStartOfActivityInterrupts,
			TurnCoverage:      TurnCoverageTurnIncludesOnlyActivity,
		},
	}
}

func toolDefinitionsToGenAI(defs []ToolDefinition) []*genai.Tool {
	if len(defs) == 0 {
		return nil
	}
	functions := make([]*genai.FunctionDeclaration, 0, len(defs))
	for _, def := range defs {
		if strings.TrimSpace(def.Name) == "" {
			continue
		}
		functions = append(functions, &genai.FunctionDeclaration{
			Name:                 def.Name,
			Description:          def.Description,
			ParametersJsonSchema: def.ParametersJSONSchema,
			ResponseJsonSchema:   def.ResponseJSONSchema,
			Behavior:             mapToolBehavior(def.Behavior),
		})
	}
	if len(functions) == 0 {
		return nil
	}
	return []*genai.Tool{{FunctionDeclarations: functions}}
}

func resolvedGeminiLiveModel(cfg LiveConfig) string {
	model := strings.TrimSpace(cfg.Model)
	if model != "" {
		return model
	}
	return defaultGeminiLiveModel
}

func buildGeminiLiveHTTPOptions(cfg LiveConfig) genai.HTTPOptions {
	if requiresGeminiLiveV1Alpha(cfg) {
		return genai.HTTPOptions{APIVersion: "v1alpha"}
	}
	return genai.HTTPOptions{APIVersion: "v1beta"}
}

func requiresGeminiLiveV1Alpha(cfg LiveConfig) bool {
	if strings.HasPrefix(strings.TrimSpace(cfg.APIKey), "auth_tokens/") {
		return true
	}
	return normalizeLivePolicies(cfg.Policies).EnableAffectiveDialog
}

func validateGeminiLiveConfig(cfg LiveConfig) error {
	model := strings.ToLower(resolvedGeminiLiveModel(cfg))
	policies := normalizeLivePolicies(cfg.Policies)

	if policies.EnableAffectiveDialog && isGemini31FlashLiveModel(model) {
		return fmt.Errorf("gemini live: affective dialog is not supported by %s", resolvedGeminiLiveModel(cfg))
	}
	if isGemini31FlashLiveModel(model) {
		for _, tool := range cfg.Tools {
			if tool.Behavior == ToolBehaviorNonBlocking {
				return fmt.Errorf("gemini live: non-blocking tool behavior is not supported by %s", resolvedGeminiLiveModel(cfg))
			}
		}
	}
	return nil
}

func isGemini31FlashLiveModel(model string) bool {
	return strings.Contains(model, "3.1-flash-live")
}

func mapThinkingLevel(level ThinkingLevel) genai.ThinkingLevel {
	switch level {
	case ThinkingLevelLow:
		return genai.ThinkingLevelLow
	case ThinkingLevelMedium:
		return genai.ThinkingLevelMedium
	case ThinkingLevelHigh:
		return genai.ThinkingLevelHigh
	default:
		return genai.ThinkingLevelMinimal
	}
}

func mapToolBehavior(behavior ToolBehavior) genai.Behavior {
	switch behavior {
	case ToolBehaviorBlocking:
		return genai.BehaviorBlocking
	case ToolBehaviorNonBlocking:
		return genai.BehaviorNonBlocking
	default:
		return genai.BehaviorUnspecified
	}
}

func mapToolResponseScheduling(scheduling ToolResponseScheduling) genai.FunctionResponseScheduling {
	switch scheduling {
	case ToolResponseSchedulingSilent:
		return genai.FunctionResponseSchedulingSilent
	case ToolResponseSchedulingWhenIdle:
		return genai.FunctionResponseSchedulingWhenIdle
	case ToolResponseSchedulingInterrupt:
		return genai.FunctionResponseSchedulingInterrupt
	default:
		return genai.FunctionResponseSchedulingUnspecified
	}
}

func mapStartSensitivity(level StartSensitivity) genai.StartSensitivity {
	switch level {
	case StartSensitivityHigh:
		return genai.StartSensitivityHigh
	default:
		return genai.StartSensitivityLow
	}
}

func mapEndSensitivity(level EndSensitivity) genai.EndSensitivity {
	switch level {
	case EndSensitivityHigh:
		return genai.EndSensitivityHigh
	default:
		return genai.EndSensitivityLow
	}
}

func mapActivityHandling(mode ActivityHandling) genai.ActivityHandling {
	switch mode {
	case ActivityHandlingNoInterrupt:
		return genai.ActivityHandlingNoInterruption
	case ActivityHandlingStartOfActivityInterrupts:
		return genai.ActivityHandlingStartOfActivityInterrupts
	default:
		return genai.ActivityHandlingStartOfActivityInterrupts
	}
}

func mapTurnCoverage(mode TurnCoverage) genai.TurnCoverage {
	switch mode {
	case TurnCoverageTurnIncludesAllInput:
		return genai.TurnCoverageTurnIncludesAllInput
	case TurnCoverageTurnIncludesAudioActivity:
		return genai.TurnCoverageTurnIncludesAudioActivityAndAllVideo
	default:
		return genai.TurnCoverageTurnIncludesOnlyActivity
	}
}
