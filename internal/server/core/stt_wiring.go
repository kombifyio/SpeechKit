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

// buildSTTRouter constructs the STT router from config. It mirrors the
// essential parts of cmd/speechkit/app_init.go:buildRouter but is leaner:
// - no device-target model-profile-selection logic
// - no Ollama (clients that need it can re-enable in v1.1)
// - no Genkit wiring here (that belongs in the Assist milestone)
//
// The returned healthChecks map lets bootstrap register each provider as a
// health-component after the router is built; keeping the check logic here
// keeps the bootstrap file focused on lifecycle.
func buildSTTRouter(cfg *config.Config) (*router.Router, []namedProvider, []string) {
	r := &router.Router{
		Strategy:             router.Strategy(strings.TrimSpace(cfg.Routing.Strategy)),
		PreferLocalUnderSecs: cfg.Routing.PreferLocalUnderSeconds,
		ParallelCloud:        cfg.Routing.ParallelCloud,
		ReplaceOnBetter:      cfg.Routing.ReplaceOnBetter,
	}
	// Default to cloud-only when config leaves the field blank — the server
	// deployment target typically relies on managed cloud APIs even when a
	// local whisper.cpp is present.
	if r.Strategy == "" {
		r.Strategy = router.StrategyCloudOnly
	}

	var providers []namedProvider
	var notes []string

	// HuggingFace (cloud). Resolves token via standard precedence.
	if cfg.HuggingFace.Enabled {
		token, status, err := config.ResolveHuggingFaceToken(cfg)
		if err == nil && token != "" {
			p := stt.NewHuggingFaceProvider(cfg.HuggingFace.Model, token)
			r.AddCloud(p)
			providers = append(providers, namedProvider{name: "stt.huggingface", provider: p})
			notes = append(notes, "HuggingFace STT registered (model="+cfg.HuggingFace.Model+", source="+string(status.ActiveSource)+")")
		} else {
			notes = append(notes, "HuggingFace STT disabled (token missing)")
		}
	}

	// OpenRouter speech-to-text (cloud gateway).
	if cfg.Providers.OpenRouter.Enabled {
		if key := strings.TrimSpace(config.ResolveSecret(cfg.Providers.OpenRouter.APIKeyEnv)); key != "" {
			p := stt.NewOpenRouterSTTProvider(key, cfg.Providers.OpenRouter.STTModel)
			r.AddCloud(p)
			providers = append(providers, namedProvider{name: "stt.openrouter", provider: p})
			notes = append(notes, "OpenRouter STT registered (model="+p.Model+")")
		} else {
			notes = append(notes, "OpenRouter STT disabled ("+cfg.Providers.OpenRouter.APIKeyEnv+" not set)")
		}
	}

	// OpenAI Whisper (cloud).
	if cfg.Providers.OpenAI.Enabled {
		if key := strings.TrimSpace(config.ResolveSecret(cfg.Providers.OpenAI.APIKeyEnv)); key != "" {
			p := stt.NewOpenAISTTProvider(key)
			r.AddCloud(p)
			providers = append(providers, namedProvider{name: "stt.openai", provider: p})
			notes = append(notes, "OpenAI STT registered")
		} else {
			notes = append(notes, "OpenAI STT disabled ("+cfg.Providers.OpenAI.APIKeyEnv+" not set)")
		}
	}

	// Groq (cloud).
	if cfg.Providers.Groq.Enabled {
		if key := strings.TrimSpace(config.ResolveSecret(cfg.Providers.Groq.APIKeyEnv)); key != "" {
			p := stt.NewGroqSTTProvider(key)
			r.AddCloud(p)
			providers = append(providers, namedProvider{name: "stt.groq", provider: p})
			notes = append(notes, "Groq STT registered")
		} else {
			notes = append(notes, "Groq STT disabled ("+cfg.Providers.Groq.APIKeyEnv+" not set)")
		}
	}

	// Google Cloud STT.
	if cfg.Providers.Google.Enabled {
		if key, source := config.ResolveGoogleSTTKey(cfg); key != "" {
			p := stt.NewGoogleSTTProvider(key, cfg.Providers.Google.STTModel)
			r.AddCloud(p)
			providers = append(providers, namedProvider{name: "stt.google", provider: p})
			notes = append(notes, "Google STT registered (model="+cfg.Providers.Google.STTModel+", source="+source+")")
		} else {
			notes = append(notes, "Google STT disabled (set "+config.GoogleSTTAPIKeyEnvName(cfg)+" or "+config.GoogleCloudSTTAPIKeyEnv+")")
		}
	}

	// VPS (self-hosted OpenAI-compatible).
	if cfg.VPS.Enabled && strings.TrimSpace(cfg.VPS.URL) != "" {
		key := config.ResolveSecret(cfg.VPS.APIKeyEnv)
		p := stt.NewVPSProviderWithModel(cfg.VPS.URL, key, cfg.VPS.Model)
		r.AddCloud(p)
		providers = append(providers, namedProvider{name: "stt.vps", provider: p})
		notes = append(notes, "VPS STT registered (url="+cfg.VPS.URL+", model="+p.Model+")")
	}

	// Local whisper.cpp. We DO register the provider so the router has a
	// local target, but lifecycle (StartServer) is managed separately — see
	// MaybeStartLocalSTT. This keeps the server usable with cloud-only
	// providers while still surfacing a local option.
	if cfg.Local.Enabled && strings.TrimSpace(cfg.Local.ModelPath) != "" {
		p := stt.NewLocalProvider(cfg.Local.Port, cfg.Local.ModelPath, cfg.Local.GPU)
		r.SetLocal(p)
		providers = append(providers, namedProvider{name: "stt.local", provider: p})
		notes = append(notes, "Local whisper.cpp configured (not yet started; see readiness probe)")
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
			app.Health.SetReadyWithOptions(sttAggregateComponent, StatusUnavailable, "no STT providers configured", sttAggregateOptions(true))
			return
		}
		app.Health.SetReadyWithOptions(sttAggregateComponent, StatusStarting, "probing STT providers", sttAggregateOptions(true))
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
			app.Health.SetReadyWithOptions(sttAggregateComponent, StatusOK, "at least one STT provider ready", sttAggregateOptions(true))
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
		app.Health.SetReadyWithOptions(sttAggregateComponent, StatusUnavailable, "no STT providers configured", sttAggregateOptions(true))
		return
	}
	if starting > 0 {
		app.Health.SetReadyWithOptions(sttAggregateComponent, StatusStarting, "probing STT providers", sttAggregateOptions(true))
		return
	}
	if degraded > 0 {
		app.Health.SetReadyWithOptions(sttAggregateComponent, StatusDegraded, "no STT provider is currently ready", sttAggregateOptions(true))
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

func sttAggregateOptions(blocking bool) ComponentOptions {
	return ComponentOptions{
		Blocking: blocking,
		Kind:     "dependency",
		Modes:    []string{string(ModeDictation)},
	}
}
