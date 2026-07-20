//go:build linux

package core

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/firebase/genkit/go/core"
	"github.com/kombifyio/SpeechKit/internal/ai"
	"github.com/kombifyio/SpeechKit/internal/ai/flows"
	"github.com/kombifyio/SpeechKit/internal/assist"
	"github.com/kombifyio/SpeechKit/internal/assist/skills/voice_companion"
	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/shortcuts"
	"github.com/kombifyio/SpeechKit/internal/tts"
	"github.com/kombifyio/SpeechKit/internal/ttswiring"
)

// ensureSharedAIDeps lazily builds Genkit runtime + TTS router + assist/agent
// flows and stores them on the App. Subsequent calls are no-ops. Used by
// both the Assist mode handler and the Cascaded Voice Agent provider so
// they share a single Genkit instance instead of paying init cost twice.
//
// Never fatal: provider absence degrades /readyz components but the server
// still starts.
func ensureSharedAIDeps(ctx context.Context, app *App) []string {
	if app.aiDepsOnce {
		return nil
	}
	// Without a config there is nothing to build; leave the AI deps nil so
	// callers that need them (e.g. the cascaded Voice Agent provider) degrade
	// to a clean "unavailable" error instead of dereferencing a nil config.
	// app.Cfg is always set in production (newServerApp); this guards tests and
	// any embedder that builds an App without one.
	if app.Cfg == nil {
		return nil
	}
	app.aiDepsOnce = true

	var notes []string

	genkitRT, genkitNotes, err := buildGenkitRuntime(ctx, app.Cfg)
	notes = append(notes, genkitNotes...)
	if err != nil {
		slog.Warn("Genkit init failed; LLM-dependent modes degrade", "err", err)
		app.Health.SetReady("genkit", StatusUnavailable, err.Error())
	} else {
		app.GenkitRuntime = genkitRT
		app.AssistFlow = assistFlowFromRuntime(genkitRT)
		app.AgentFlow = agentFlowFromRuntime(genkitRT)
		app.Health.SetReady("genkit", StatusOK, "ready")
	}
	registerLocalLLMHealth(ctx, app)

	ttsRouter, ttsEnabled, ttsNotes := buildTTSRouter(app.Cfg)
	notes = append(notes, ttsNotes...)
	app.TTSRouter = ttsRouter
	app.TTSEnabled = ttsEnabled
	switch {
	case ttsEnabled:
		app.Health.SetReady("tts", StatusOK, "enabled")
	case !app.Cfg.TTS.Enabled:
		app.Health.SetReady("tts", StatusOK, "disabled")
	default:
		app.Health.SetReady("tts", StatusDegraded, "enabled but no providers configured")
	}

	return notes
}

func registerLocalLLMHealth(ctx context.Context, app *App) {
	if app == nil || app.Cfg == nil || !app.Cfg.LocalLLM.Enabled || strings.TrimSpace(app.Cfg.LocalLLM.BaseURL) == "" {
		return
	}
	healthURL := localLLMHealthURL(app.Cfg.LocalLLM.BaseURL)
	app.Health.SetReady("llm.local", StatusStarting, "probing")
	go func() {
		client := &http.Client{Timeout: 5 * time.Second}
		deadline := time.Now().Add(15 * time.Minute)
		var lastErr error
		for {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, http.NoBody)
			if err == nil {
				resp, err := client.Do(req)
				if err == nil {
					_ = resp.Body.Close()
					if resp.StatusCode == http.StatusOK {
						app.Health.SetReady("llm.local", StatusOK, "ready")
						return
					}
					lastErr = errStatus(resp.StatusCode)
				} else {
					lastErr = err
				}
			} else {
				lastErr = err
			}

			if time.Now().After(deadline) {
				detail := "local LLM did not become ready"
				if lastErr != nil {
					detail = lastErr.Error()
				}
				app.Health.SetReady("llm.local", StatusDegraded, detail)
				return
			}
			time.Sleep(10 * time.Second)
		}
	}()
}

func localLLMHealthURL(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	baseURL = strings.TrimSuffix(baseURL, "/v1")
	return baseURL + "/health"
}

// buildAssistPipeline assembles the Assist mode pipeline from the shared AI
// deps held on App. Any degraded component (no Genkit, no TTS, no
// shortcuts) still yields a viable pipeline — the Framework's Assist code
// gracefully degrades from LLM → shortcuts → action-only, and Assist
// responses stay functional for every valid caller.
func buildAssistPipeline(ctx context.Context, cfg *config.Config, app *App) (*assist.Pipeline, []string) {
	notes := ensureSharedAIDeps(ctx, app)

	shortcutResolver := buildShortcutResolver(cfg)

	// Voice-Companion skill catalog wraps the host-side legacy executor.
	// CompositeExecutor first dispatches to a matching pure-Go skill
	// (Time / Date / Math / Weather / Timer / Reminder / Wikipedia /
	// HomeAssistant); unmatched intents fall through to the legacy
	// serverAssistToolExecutor so existing Copy/Insert/Summarize clients
	// keep working unchanged.
	haURL := strings.TrimSpace(cfg.Assist.HomeAssistant.URL)
	haToken := strings.TrimSpace(config.ResolveSecret(cfg.Assist.HomeAssistant.TokenEnv))
	haConfigured := haURL != "" && haToken != ""
	skills := voice_companion.AllSkills(haURL, haToken)
	executor := voice_companion.NewCompositeExecutor(skills, serverAssistToolExecutor{})

	switch {
	case haConfigured:
		notes = append(notes, "Assist: HomeAssistant bridge wired ("+haURL+")")
	case haURL != "":
		notes = append(notes, "Assist: HomeAssistant URL configured but token env unresolved; smart-home intents fail closed")
	}
	notes = append(notes, "Assist: Voice-Companion skill catalog active (Time/Date/Math/Weather/Timer/Reminder/Wikipedia)")

	utilityRegistry := buildAssistUtilityRegistry(cfg)
	if haConfigured {
		// Flip the HomeAssistant UtilityDefinition to Enabled=true so the
		// router routes HA-prefix matches ("schalte ...", "turn on ...")
		// to the CompositeExecutor. The Pipeline's secondary guard keeps
		// the same intent terminal when the integration is unavailable.
		enableHomeAssistantUtility(utilityRegistry)
	}

	// v0.38.0 multi-turn: each Pipeline gets its own in-memory
	// SkillContextStore. The store is process-scoped — multi-server
	// deployments needing cross-replica state would swap in a
	// Redis-backed implementation here.
	skillContexts := assist.NewInMemorySkillContextStore(60*time.Second, nil)

	pipeline := assist.NewPipeline(
		app.AssistFlow,
		executor,
		app.TTSRouter,
		app.TTSEnabled,
		assist.WithRouter(assist.NewRouter(
			assist.WithResolver(shortcutResolver),
			assist.WithUtilityRegistry(utilityRegistry),
		)),
		assist.WithSkillContextStore(skillContexts),
		assist.WithSpeechDefaults(config.SpeechDefaultsValues(app.Cfg)),
	)
	return pipeline, notes
}

// enableHomeAssistantUtility flips the HomeAssistant UtilityDefinition
// to Enabled=true so the router accepts the intent. Idempotent — when
// the registry already has the entry enabled the call is a no-op.
func enableHomeAssistantUtility(registry *assist.UtilityRegistry) {
	if registry == nil {
		return
	}
	registry.Register(assist.UtilityDefinition{
		ID:             assist.UtilityHomeAssistant,
		Intent:         shortcuts.IntentHomeAssistant,
		Label:          "Send command to Home Assistant",
		Input:          assist.UtilityInputUtterance,
		DefaultSurface: assist.ResultSurfaceActionAck,
		DefaultKind:    assist.ResultKindUtilityAction,
		Enabled:        true,
	})
}

type serverAssistToolExecutor struct{}

func (serverAssistToolExecutor) Execute(_ context.Context, call assist.ToolCall) (assist.ToolResult, error) {
	text := serverAssistToolText(call)
	return assist.ToolResult{
		Text:      text,
		SpeakText: text,
		Action:    "execute",
		Locale:    call.Locale,
	}, nil
}

func serverAssistToolText(call assist.ToolCall) string {
	switch call.Intent {
	case shortcuts.IntentCopyLast:
		return "Copy last transcription."
	case shortcuts.IntentInsertLast:
		return "Insert last transcription."
	case shortcuts.IntentSummarize:
		return "Summarize selection."
	case shortcuts.IntentQuickNote:
		return "Create quick note."
	default:
		return "Execute Assist tool."
	}
}

func buildAssistUtilityRegistry(cfg *config.Config) *assist.UtilityRegistry {
	base := assist.DefaultUtilityRegistry()
	if cfg == nil || cfg.Assist.EnabledTools == nil {
		// No explicit allow-list — surface the default registry as-is
		// so freshly-shipped Voice-Companion skills (Time/Date/...) are
		// usable out of the box. Hosts that want to lock the catalog
		// down still get the existing allow-list semantics via the
		// branch below.
		return base
	}

	enabled := map[assist.UtilityID]bool{}
	for _, id := range cfg.Assist.EnabledTools {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		enabled[assist.UtilityID(id)] = true
	}

	filtered := assist.NewUtilityRegistry()
	for _, def := range base.List() {
		if enabled[def.ID] {
			def.Enabled = true
			filtered.Register(def)
		}
	}
	return filtered
}

// agentFlowFromRuntime mirrors assistFlowFromRuntime for the agent flow that
// the Cascaded Voice Agent provider uses. Returns nil when no agent models
// are available — the cascaded provider rejects Connect in that state.
func agentFlowFromRuntime(rt *ai.Runtime) *core.Flow[flows.AgentInput, flows.AgentOutput, struct{}] {
	if rt == nil {
		return nil
	}
	models := rt.AgentModels()
	if len(models) == 0 {
		return nil
	}
	return flows.DefineAgentFlow(rt.G, models)
}

// buildGenkitRuntime maps config.Config into ai.Config and initializes Genkit.
// Only the fields the Server-Target actually uses are mapped — Local-LLM
// runtime detection and model-profile-selection logic are Device-Target
// concerns that the server deliberately does not replicate.
func buildGenkitRuntime(ctx context.Context, cfg *config.Config) (*ai.Runtime, []string, error) {
	var notes []string
	aiCfg := ai.Config{}
	credential := func(target string) string {
		key, _, err := config.ResolveProviderCredentialValue(cfg, target)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(key)
	}

	if cfg.Providers.Google.Enabled {
		if key := credential("google"); key != "" {
			aiCfg.GoogleAPIKey = key
			aiCfg.GoogleUtilityModel = cfg.Providers.Google.UtilityModel
			aiCfg.GoogleAssistModel = cfg.Providers.Google.AssistModel
			aiCfg.GoogleAgentModel = cfg.Providers.Google.AgentModel
			notes = append(notes, "Genkit: Google provider registered")
		}
	}
	if cfg.Providers.OpenAI.Enabled {
		if key := credential("openai"); key != "" {
			aiCfg.OpenAIAPIKey = key
			aiCfg.OpenAIUtilityModel = cfg.Providers.OpenAI.UtilityModel
			aiCfg.OpenAIAssistModel = cfg.Providers.OpenAI.AssistModel
			aiCfg.OpenAIAgentModel = cfg.Providers.OpenAI.AgentModel
			notes = append(notes, "Genkit: OpenAI provider registered")
		}
	}
	if cfg.Providers.Groq.Enabled {
		if key := credential("groq"); key != "" {
			aiCfg.GroqAPIKey = key
			aiCfg.GroqUtilityModel = cfg.Providers.Groq.UtilityModel
			aiCfg.GroqAssistModel = cfg.Providers.Groq.AssistModel
			aiCfg.GroqAgentModel = cfg.Providers.Groq.AgentModel
			notes = append(notes, "Genkit: Groq provider registered")
		}
	}
	if cfg.HuggingFace.Enabled {
		if token := credential("huggingface"); token != "" {
			aiCfg.HuggingFaceToken = token
			aiCfg.HFUtilityModel = cfg.HuggingFace.UtilityModel
			aiCfg.HFAssistModel = cfg.HuggingFace.AssistModel
			aiCfg.HFAgentModel = cfg.HuggingFace.AgentModel
			notes = append(notes, "Genkit: HuggingFace provider registered")
		}
	}
	if cfg.Providers.OpenRouter.Enabled {
		if key := credential("openrouter"); key != "" {
			aiCfg.OpenRouterAPIKey = key
			aiCfg.OpenRouterUtilityModel = cfg.Providers.OpenRouter.UtilityModel
			aiCfg.OpenRouterAssistModel = cfg.Providers.OpenRouter.AssistModel
			aiCfg.OpenRouterAgentModel = cfg.Providers.OpenRouter.AgentModel
			notes = append(notes, "Genkit: OpenRouter provider registered")
		}
	}
	if cfg.LocalLLM.Enabled && strings.TrimSpace(cfg.LocalLLM.BaseURL) != "" {
		aiCfg.LocalLLMBaseURL = cfg.LocalLLM.BaseURL
		aiCfg.LocalLLMUtilityModel = cfg.LocalLLM.UtilityModel
		aiCfg.LocalLLMAssistModel = cfg.LocalLLM.AssistModel
		aiCfg.LocalLLMAgentModel = cfg.LocalLLM.AgentModel
		notes = append(notes, "Genkit: Local LLM registered ("+cfg.LocalLLM.BaseURL+")")
	}

	rt, err := ai.Init(ctx, aiCfg)
	if err != nil {
		return nil, notes, err
	}
	return rt, notes, nil
}

// assistFlowFromRuntime defines the Assist flow only when at least one assist
// model is available. Returning nil is a valid state — the pipeline still
// handles codeword shortcuts without an LLM.
func assistFlowFromRuntime(rt *ai.Runtime) *core.Flow[flows.AssistInput, flows.AssistOutput, struct{}] {
	if rt == nil {
		return nil
	}
	models := rt.AssistModels()
	if len(models) == 0 {
		return nil
	}
	return flows.DefineAssistFlow(rt.G, models)
}

// buildTTSRouter constructs the TTS router from configured providers. Returns
// the router, whether TTS is effectively enabled (any provider available),
// and human-readable status notes for the startup log.
func buildTTSRouter(cfg *config.Config) (*tts.Router, bool, []string) {
	if !cfg.TTS.Enabled {
		return nil, false, []string{"TTS: disabled in config"}
	}

	// Resolve config → enabled-provider opts (shared with the Device-Target via
	// ttswiring), then delegate the actual router assembly + model_selection
	// pinning to the shared tts.BuildRouter SSOT.
	enabled, preNotes := ttswiring.ResolveEnabledProviders(cfg)

	router, ok, notes := tts.BuildRouter(tts.Strategy(strings.TrimSpace(cfg.TTS.Strategy)), enabled)
	notes = append(notes, preNotes...)
	if !ok {
		return nil, false, append(notes, "TTS: no providers configured; Assist responses will be text-only")
	}
	return router, true, notes
}

// buildShortcutResolver translates config.Shortcuts.Locale into a runtime
// resolver. If no locale config is present the built-in default resolver is
// used (covers the most common English phrases).
func buildShortcutResolver(cfg *config.Config) *shortcuts.Resolver {
	if len(cfg.Shortcuts.Locale) == 0 {
		return shortcuts.DefaultResolver()
	}
	registry := shortcuts.DefaultRegistry()
	for locale, localeCfg := range cfg.Shortcuts.Locale {
		if len(localeCfg.LeadingFillers) > 0 {
			registry.RegisterLeadingFillers(locale, localeCfg.LeadingFillers...)
		}
		registerShortcutAliases(registry, locale, shortcuts.IntentCopyLast, localeCfg.CopyLast)
		registerShortcutAliases(registry, locale, shortcuts.IntentInsertLast, localeCfg.InsertLast)
		registerShortcutAliases(registry, locale, shortcuts.IntentSummarize, localeCfg.Summarize)
		registerShortcutAliases(registry, locale, shortcuts.IntentQuickNote, localeCfg.QuickNote)
	}
	return shortcuts.NewResolver(registry)
}

// registerShortcutAliases mirrors the device-target's alias-registration
// helper. Each alias becomes a phrase with prefix-matching and priority 100,
// bundled into a single IntentLexicon for the given locale.
func registerShortcutAliases(registry *shortcuts.Registry, locale string, intent shortcuts.Intent, aliases []string) {
	if registry == nil || len(aliases) == 0 {
		return
	}
	phrases := make([]shortcuts.Phrase, 0, len(aliases))
	for _, alias := range aliases {
		if trimmed := strings.TrimSpace(alias); trimmed != "" {
			phrases = append(phrases, shortcuts.Phrase{
				Value:    trimmed,
				Prefix:   true,
				Priority: 100,
			})
		}
	}
	if len(phrases) == 0 {
		return
	}
	registry.RegisterLexicon(shortcuts.IntentLexicon{
		Intent:  intent,
		Locale:  locale,
		Phrases: phrases,
	})
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
