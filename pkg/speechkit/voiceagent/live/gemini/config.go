package gemini

import (
	"fmt"
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
	"google.golang.org/genai"
)

func buildGeminiLiveConnectConfig(cfg live.LiveConfig) *genai.LiveConnectConfig {
	resolved := live.ResolveLiveOptions("google", "realtime.google.gemini-native-audio", cfg, nil, nil)
	policies := normalizeLivePolicies(cfg.Policies)
	if resolved.HasTurnDetectionOverride() && !resolved.TurnDetection {
		policies.ActivityDetection.Automatic = false
	}
	if !policies.Thinking.Enabled {
		if level := thinkingLevelFromReasoningEffort(resolved.ReasoningEffort); level != "" {
			policies.Thinking.Enabled = true
			policies.Thinking.ThinkingLevel = level
		}
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
			Parts: []*genai.Part{genai.NewPartFromText(live.AppendContextPrompt(buildInstructionText(cfg), resolved.ContextPrompt))},
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

func buildInstructionText(cfg live.LiveConfig) string {
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

func resolvedFrameworkPrompt(cfg live.LiveConfig) string {
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

func normalizeLivePolicies(policies live.LivePolicies) live.LivePolicies {
	if !policies.EnableInputAudioTranscription && !policies.EnableOutputAudioTranscription &&
		!policies.EnableAffectiveDialog && !policies.Thinking.Enabled &&
		!policies.ContextCompression.Enabled && !policies.ActivityDetection.Automatic &&
		policies.ActivityDetection.StartSensitivity == "" && policies.ActivityDetection.EndSensitivity == "" &&
		policies.ActivityDetection.PrefixPaddingMs == 0 && policies.ActivityDetection.SilenceDurationMs == 0 &&
		policies.ActivityDetection.ActivityHandling == "" && policies.ActivityDetection.TurnCoverage == "" {
		return defaultLivePolicies()
	}

	if policies.ActivityDetection.StartSensitivity == "" {
		policies.ActivityDetection.StartSensitivity = live.StartSensitivityLow
	}
	if policies.ActivityDetection.EndSensitivity == "" {
		policies.ActivityDetection.EndSensitivity = live.EndSensitivityLow
	}
	if policies.ActivityDetection.PrefixPaddingMs == 0 {
		policies.ActivityDetection.PrefixPaddingMs = 100
	}
	if policies.ActivityDetection.SilenceDurationMs == 0 {
		policies.ActivityDetection.SilenceDurationMs = 700
	}
	if policies.ActivityDetection.ActivityHandling == "" {
		policies.ActivityDetection.ActivityHandling = live.ActivityHandlingStartOfActivityInterrupts
	}
	if policies.ActivityDetection.TurnCoverage == "" {
		policies.ActivityDetection.TurnCoverage = live.TurnCoverageTurnIncludesOnlyActivity
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

func defaultLivePolicies() live.LivePolicies {
	return live.LivePolicies{
		EnableInputAudioTranscription:  true,
		EnableOutputAudioTranscription: true,
		ContextCompression: live.ContextCompressionPolicy{
			Enabled:       true,
			TriggerTokens: 12000,
			TargetTokens:  6000,
		},
		ActivityDetection: live.ActivityDetectionPolicy{
			Automatic:         true,
			StartSensitivity:  live.StartSensitivityLow,
			EndSensitivity:    live.EndSensitivityLow,
			PrefixPaddingMs:   100,
			SilenceDurationMs: 700,
			ActivityHandling:  live.ActivityHandlingStartOfActivityInterrupts,
			TurnCoverage:      live.TurnCoverageTurnIncludesOnlyActivity,
		},
	}
}

func toolDefinitionsToGenAI(defs []live.ToolDefinition) []*genai.Tool {
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

func resolvedGeminiLiveModel(cfg live.LiveConfig) string {
	model := strings.TrimSpace(cfg.Model)
	if model != "" {
		return model
	}
	return defaultGeminiLiveModel
}

func buildGeminiLiveHTTPOptions(cfg live.LiveConfig) genai.HTTPOptions {
	if requiresGeminiLiveV1Alpha(cfg) {
		return genai.HTTPOptions{APIVersion: "v1alpha"}
	}
	return genai.HTTPOptions{APIVersion: "v1beta"}
}

func requiresGeminiLiveV1Alpha(cfg live.LiveConfig) bool {
	if strings.HasPrefix(strings.TrimSpace(cfg.APIKey), "auth_tokens/") {
		return true
	}
	return normalizeLivePolicies(cfg.Policies).EnableAffectiveDialog
}

func validateGeminiLiveConfig(cfg live.LiveConfig) error {
	model := strings.ToLower(resolvedGeminiLiveModel(cfg))
	policies := normalizeLivePolicies(cfg.Policies)

	if policies.EnableAffectiveDialog && isGemini31FlashLiveModel(model) {
		return fmt.Errorf("gemini live: affective dialog is not supported by %s", resolvedGeminiLiveModel(cfg))
	}
	if isGemini31FlashLiveModel(model) {
		for _, tool := range cfg.Tools {
			if tool.Behavior == live.ToolBehaviorNonBlocking {
				return fmt.Errorf("gemini live: non-blocking tool behavior is not supported by %s", resolvedGeminiLiveModel(cfg))
			}
		}
	}
	return nil
}

func isGemini31FlashLiveModel(model string) bool {
	return strings.Contains(model, "3.1-flash-live")
}

func mapThinkingLevel(level live.ThinkingLevel) genai.ThinkingLevel {
	switch level {
	case live.ThinkingLevelLow:
		return genai.ThinkingLevelLow
	case live.ThinkingLevelMedium:
		return genai.ThinkingLevelMedium
	case live.ThinkingLevelHigh:
		return genai.ThinkingLevelHigh
	default:
		return genai.ThinkingLevelMinimal
	}
}

func thinkingLevelFromReasoningEffort(effort string) live.ThinkingLevel {
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "off", "none", "minimal":
		return live.ThinkingLevelOff
	case "low":
		return live.ThinkingLevelLow
	case "medium", "normal", "balanced":
		return live.ThinkingLevelMedium
	case "high":
		return live.ThinkingLevelHigh
	default:
		return ""
	}
}

func mapToolBehavior(behavior live.ToolBehavior) genai.Behavior {
	switch behavior {
	case live.ToolBehaviorBlocking:
		return genai.BehaviorBlocking
	case live.ToolBehaviorNonBlocking:
		return genai.BehaviorNonBlocking
	default:
		return genai.BehaviorUnspecified
	}
}

func mapToolResponseScheduling(scheduling live.ToolResponseScheduling) genai.FunctionResponseScheduling {
	switch scheduling {
	case live.ToolResponseSchedulingSilent:
		return genai.FunctionResponseSchedulingSilent
	case live.ToolResponseSchedulingWhenIdle:
		return genai.FunctionResponseSchedulingWhenIdle
	case live.ToolResponseSchedulingInterrupt:
		return genai.FunctionResponseSchedulingInterrupt
	default:
		return genai.FunctionResponseSchedulingUnspecified
	}
}

func mapStartSensitivity(level live.StartSensitivity) genai.StartSensitivity {
	switch level {
	case live.StartSensitivityHigh:
		return genai.StartSensitivityHigh
	default:
		return genai.StartSensitivityLow
	}
}

func mapEndSensitivity(level live.EndSensitivity) genai.EndSensitivity {
	switch level {
	case live.EndSensitivityHigh:
		return genai.EndSensitivityHigh
	default:
		return genai.EndSensitivityLow
	}
}

func mapActivityHandling(mode live.ActivityHandling) genai.ActivityHandling {
	switch mode {
	case live.ActivityHandlingNoInterrupt:
		return genai.ActivityHandlingNoInterruption
	case live.ActivityHandlingStartOfActivityInterrupts:
		return genai.ActivityHandlingStartOfActivityInterrupts
	default:
		return genai.ActivityHandlingStartOfActivityInterrupts
	}
}

func mapTurnCoverage(mode live.TurnCoverage) genai.TurnCoverage {
	switch mode {
	case live.TurnCoverageTurnIncludesAllInput:
		return genai.TurnCoverageTurnIncludesAllInput
	case live.TurnCoverageTurnIncludesAudioActivity:
		return genai.TurnCoverageTurnIncludesAudioActivityAndAllVideo
	default:
		return genai.TurnCoverageTurnIncludesOnlyActivity
	}
}
