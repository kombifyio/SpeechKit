//go:build linux

package core

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/router"
	"github.com/kombifyio/SpeechKit/internal/stt"
)

// probeTimeout caps each provider Health() attempt. Cloud-API probes are
// usually sub-second; we allow a generous window so a sluggish DNS lookup or
// TLS handshake doesn't mark a perfectly fine provider as degraded.
const (
	probeTimeout  = 8 * time.Second
	probeDeadline = 2 * time.Minute
	probeInterval = 5 * time.Second

	sttAggregateComponent = "stt"
)

// buildSTTRouter constructs the STT router from config. It resolves the
// server's config (credentials via standard precedence, server defaults such
// as blank-strategy → cloud-only, intentionally no Ollama) into per-provider
// options and delegates router assembly to the shared stt.BuildRouter SSOT —
// the same path the Device-Target uses in cmd/speechkit/app_init.go.
//
// The returned namedProvider list lets bootstrap register each configured
// provider as a health-component after the router is built; keeping the check
// logic here keeps the bootstrap file focused on lifecycle.
func buildSTTRouter(cfg *config.Config) (*router.Router, []namedProvider, []string) {
	// STT adapters are config-free (importable from pkg/speechkit/**); inject
	// the host secret resolver so Google streaming credentials still resolve
	// through env + Doppler + token store, not just os.Getenv.
	stt.SetSecretResolver(config.ResolveSecret)
	strategy := router.Strategy(strings.TrimSpace(cfg.Routing.Strategy))
	// Default to cloud-only when config leaves the field blank — the server
	// deployment target typically relies on managed cloud APIs even when a
	// local whisper.cpp is present.
	if strategy == "" {
		strategy = router.StrategyCloudOnly
	}

	var enabled stt.EnabledProviders
	var notes []string
	credential := func(target string) (string, string) {
		key, source, err := config.ResolveProviderCredentialValue(cfg, target)
		if err != nil {
			source = config.ProviderCredentialEnvName(cfg, target)
		}
		if strings.TrimSpace(source) == "" {
			source = config.ProviderCredentialEnvName(cfg, target)
		}
		return strings.TrimSpace(key), strings.TrimSpace(source)
	}

	// HuggingFace (cloud). Resolves token via standard precedence.
	if cfg.HuggingFace.Enabled {
		token, source := credential("huggingface")
		if token != "" {
			enabled.HuggingFace = &stt.HuggingFaceOpts{Model: cfg.HuggingFace.Model, Token: token}
			notes = append(notes, "HuggingFace STT registered (model="+cfg.HuggingFace.Model+", source="+source+")")
		} else {
			notes = append(notes, "HuggingFace STT disabled (token missing)")
		}
	}

	// OpenRouter speech-to-text (cloud gateway).
	if cfg.Providers.OpenRouter.Enabled {
		if key, source := credential("openrouter"); key != "" {
			enabled.OpenRouter = &stt.OpenRouterOpts{APIKey: key, Model: cfg.Providers.OpenRouter.STTModel}
			notes = append(notes, "OpenRouter STT registered (model="+cfg.Providers.OpenRouter.STTModel+")")
		} else {
			notes = append(notes, "OpenRouter STT disabled ("+source+" not set)")
		}
	}

	// OpenAI Whisper (cloud). Model defaults to whisper-1 when unset.
	if cfg.Providers.OpenAI.Enabled {
		if key, source := credential("openai"); key != "" {
			model := firstNonEmpty(strings.TrimSpace(cfg.Providers.OpenAI.STTModel), "whisper-1")
			enabled.OpenAI = &stt.OpenAIOpts{APIKey: key, Model: model}
			notes = append(notes, "OpenAI STT registered (model="+model+", source="+source+")")
		} else {
			notes = append(notes, "OpenAI STT disabled ("+source+" not set)")
		}
	}

	// Groq (cloud). Model defaults to whisper-large-v3-turbo when unset.
	if cfg.Providers.Groq.Enabled {
		if key, source := credential("groq"); key != "" {
			model := firstNonEmpty(cfg.Providers.Groq.STTModel, "whisper-large-v3-turbo")
			enabled.Groq = &stt.GroqOpts{APIKey: key, Model: model}
			notes = append(notes, "Groq STT registered (model="+model+", source="+source+")")
		} else {
			notes = append(notes, "Groq STT disabled ("+source+" not set)")
		}
	}

	// Deepgram (direct cloud STT + diarization).
	if cfg.Providers.Deepgram.Enabled {
		if key, source := credential("deepgram"); key != "" {
			enabled.Deepgram = &stt.DeepgramOpts{
				APIKey:           key,
				Model:            cfg.Providers.Deepgram.STTModel,
				DiarizationModel: firstNonEmpty(cfg.Providers.Deepgram.DiarizationModel, "latest"),
				Listen:           deepgramOptionsFromConfig(cfg),
			}
			notes = append(notes, "Deepgram STT registered (model="+cfg.Providers.Deepgram.STTModel+", source="+source+")")
		} else {
			notes = append(notes, "Deepgram STT disabled ("+source+" not set)")
		}
	}

	// AssemblyAI (direct cloud STT + batch diarization / speaker identification).
	if cfg.Providers.AssemblyAI.Enabled {
		if key, source := credential("assemblyai"); key != "" {
			aai := cfg.Providers.AssemblyAI
			enabled.AssemblyAI = &stt.AssemblyAIOpts{
				APIKey:            key,
				Models:            aai.STTModels,
				StreamingModel:    aai.StreamingModel,
				StreamingBaseURL:  aai.StreamingBaseURL,
				SyncBaseURL:       aai.SyncBaseURL,
				DisableSync:       aai.DisableSync,
				StreamingLLM:      aai.StreamingLLM,
				StreamingLLMModel: aai.LLMGatewayUtilityModel,
			}
			notes = append(notes, "AssemblyAI STT registered (models="+aai.STTModels+", source="+source+")")
		} else {
			notes = append(notes, "AssemblyAI STT disabled ("+source+" not set)")
		}
	}

	// Google Cloud STT.
	if cfg.Providers.Google.Enabled {
		if key, source := credential("google_stt"); key != "" {
			enabled.Google = &stt.GoogleOpts{
				APIKey:                    key,
				Model:                     cfg.Providers.Google.STTModel,
				CredentialsJSONEnv:        config.GoogleSTTCredentialsJSONEnvName(cfg),
				ApplicationCredentialsEnv: config.GoogleApplicationCredentialsEnvName(cfg),
			}
			notes = append(notes, "Google STT registered (model="+cfg.Providers.Google.STTModel+", source="+source+")")
		} else {
			notes = append(notes, "Google STT disabled (set "+config.GoogleSTTAPIKeyEnvName(cfg)+" or "+config.GoogleCloudSTTAPIKeyEnv+")")
		}
	}

	// VPS (self-hosted OpenAI-compatible). Model defaults to whisper-1.
	if cfg.VPS.Enabled && strings.TrimSpace(cfg.VPS.URL) != "" {
		key := config.ResolveSecret(cfg.VPS.APIKeyEnv)
		enabled.VPS = &stt.VPSOpts{URL: cfg.VPS.URL, APIKey: key, Model: cfg.VPS.Model}
		notes = append(notes, "VPS STT registered (url="+cfg.VPS.URL+", model="+firstNonEmpty(strings.TrimSpace(cfg.VPS.Model), "whisper-1")+")")
	}

	// Local whisper.cpp. We DO register the provider so the router has a
	// local target, but lifecycle (StartServer) is managed separately — see
	// MaybeStartLocalSTT. This keeps the server usable with cloud-only
	// providers while still surfacing a local option.
	if cfg.Local.Enabled && strings.TrimSpace(cfg.Local.ModelPath) != "" {
		enabled.Local = &stt.LocalOpts{Port: cfg.Local.Port, ModelPath: cfg.Local.ModelPath, GPU: cfg.Local.GPU}
		notes = append(notes, "Local whisper.cpp configured (not yet started; see readiness probe)")
	}

	routerCfg := stt.RouterConfig{
		Strategy:             strategy,
		PreferLocalUnderSecs: cfg.Routing.PreferLocalUnderSeconds,
		ParallelCloud:        cfg.Routing.ParallelCloud,
		ReplaceOnBetter:      cfg.Routing.ReplaceOnBetter,
	}
	r, ok, _ := stt.BuildRouter(routerCfg, enabled)
	if !ok {
		// Keep returning a configured (empty) router so bootstrap and
		// handlers always have a routing target.
		r = &router.Router{
			Strategy:             routerCfg.Strategy,
			PreferLocalUnderSecs: routerCfg.PreferLocalUnderSecs,
			ParallelCloud:        routerCfg.ParallelCloud,
			ReplaceOnBetter:      routerCfg.ReplaceOnBetter,
		}
	}

	var providers []namedProvider
	for _, p := range r.Providers() {
		providers = append(providers, namedProvider{name: "stt." + p.Name(), provider: p})
	}

	return r, providers, notes
}

// namedProvider pairs a provider with the health-component name the bootstrap
// registers it under.
type namedProvider struct {
	name     string
	provider stt.STTProvider
}

// registerProviderHealth schedules a one-shot readiness check per provider
// that flips its health status once Health(ctx) succeeds. For cloud providers
// this is typically a cheap HTTP HEAD/GET; we don't want to block the server
// start on it, so we run the probes in background with a short per-attempt
// timeout.

func registerProviderHealth(app *App, providers []namedProvider, blockSTT bool) {
	if app == nil || app.Health == nil {
		return
	}
	if blockSTT {
		if len(providers) == 0 {
			app.Health.SetReadyWithOptions(sttAggregateComponent, StatusUnavailable, "no STT providers configured", sttAggregateOptions())
			return
		}
		app.Health.SetReadyWithOptions(sttAggregateComponent, StatusStarting, "probing STT providers", sttAggregateOptions())
	}
	for _, np := range providers {
		app.Health.SetReadyWithOptions(np.name, StatusStarting, "probing", sttProviderOptions(np))
		go probeProviderHealth(app, np, blockSTT)
	}
}

func probeProviderHealth(app *App, np namedProvider, blockSTT bool) {
	deadline := time.Now().Add(probeDeadline)
	var lastErr error
	for {
		ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
		err := np.provider.Health(ctx)
		cancel()
		if err == nil {
			slog.Info("STT provider ready", "component", np.name, "provider", np.provider.Name())
			app.Health.SetReadyWithOptions(np.name, StatusOK, "ready", sttProviderOptions(np))
			if blockSTT {
				updateSTTAggregate(app)
			}
			return
		}
		lastErr = err
		slog.Warn("STT provider health probe failed",
			"component", np.name,
			"provider", np.provider.Name(),
			"err", err,
		)
		if time.Now().After(deadline) {
			app.Health.SetReadyWithOptions(np.name, StatusDegraded, lastErr.Error(), sttProviderOptions(np))
			if blockSTT {
				updateSTTAggregate(app)
			}
			return
		}
		time.Sleep(probeInterval)
	}
}

func updateSTTAggregate(app *App) {
	if app == nil || app.Health == nil {
		return
	}
	_, components, _ := app.Health.Snapshot()
	providers := 0
	starting := 0
	degraded := 0
	for name, entry := range components {
		if !strings.HasPrefix(name, "stt.") {
			continue
		}
		switch entry.Status {
		case StatusOK:
			app.Health.SetReadyWithOptions(sttAggregateComponent, StatusOK, "at least one STT provider ready", sttAggregateOptions())
			return
		case StatusStarting:
			providers++
			starting++
		case StatusDegraded, StatusUnavailable:
			providers++
			degraded++
		case StatusDisabled:
			// Disabled STT components are intentional config choices,
			// not candidate providers for the aggregate readiness gate.
			continue
		}
	}
	if providers == 0 {
		app.Health.SetReadyWithOptions(sttAggregateComponent, StatusUnavailable, "no STT providers configured", sttAggregateOptions())
		return
	}
	if starting > 0 {
		app.Health.SetReadyWithOptions(sttAggregateComponent, StatusStarting, "probing STT providers", sttAggregateOptions())
		return
	}
	if degraded > 0 {
		app.Health.SetReadyWithOptions(sttAggregateComponent, StatusDegraded, "no STT provider is currently ready", sttAggregateOptions())
		return
	}
}

func sttProviderOptions(np namedProvider) ComponentOptions {
	provider := strings.TrimPrefix(np.name, "stt.")
	return ComponentOptions{
		Blocking: false,
		Kind:     "provider",
		Modes:    []string{string(ModeDictation), string(ModeAssist)},
		Provider: provider,
	}
}

func sttAggregateOptions() ComponentOptions {
	return ComponentOptions{
		Blocking: true,
		Kind:     "dependency",
		Modes:    []string{string(ModeDictation)},
	}
}
