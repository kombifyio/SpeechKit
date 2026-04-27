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

// probeTimeout caps each provider's startup Health() probe. Cloud-API probes
// are usually sub-second; we allow a generous window so a sluggish DNS lookup
// or TLS handshake doesn't mark a perfectly fine provider as degraded.
const probeTimeout = 8 * time.Second

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
	// Default to cloud-only when config leaves the field blank â€” the server
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
		if key := strings.TrimSpace(config.ResolveSecret(cfg.Providers.Google.APIKeyEnv)); key != "" {
			p := stt.NewGoogleSTTProvider(key, cfg.Providers.Google.STTModel)
			r.AddCloud(p)
			providers = append(providers, namedProvider{name: "stt.google", provider: p})
			notes = append(notes, "Google STT registered (model="+cfg.Providers.Google.STTModel+")")
		} else {
			notes = append(notes, "Google STT disabled ("+cfg.Providers.Google.APIKeyEnv+" not set)")
		}
	}

	// VPS (self-hosted OpenAI-compatible).
	if cfg.VPS.Enabled && strings.TrimSpace(cfg.VPS.URL) != "" {
		key := config.ResolveSecret(cfg.VPS.APIKeyEnv)
		p := stt.NewVPSProvider(cfg.VPS.URL, key)
		r.AddCloud(p)
		providers = append(providers, namedProvider{name: "stt.vps", provider: p})
		notes = append(notes, "VPS STT registered (url="+cfg.VPS.URL+")")
	}

	// Local whisper.cpp. We DO register the provider so the router has a
	// local target, but lifecycle (StartServer) is managed separately â€” see
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
func registerProviderHealth(app *App, providers []namedProvider) {
	for _, np := range providers {
		app.Health.SetReady(np.name, StatusStarting, "probing")
	}

	go func() {
		for _, np := range providers {
			ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
			err := np.provider.Health(ctx)
			cancel()
			if err != nil {
				slog.Warn("STT provider health probe failed",
					"component", np.name,
					"provider", np.provider.Name(),
					"err", err,
				)
				app.Health.SetReady(np.name, StatusDegraded, err.Error())
				continue
			}
			slog.Info("STT provider ready", "component", np.name, "provider", np.provider.Name())
			app.Health.SetReady(np.name, StatusOK, "ready")
		}
	}()
}
