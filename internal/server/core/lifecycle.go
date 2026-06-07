//go:build linux

package core

import (
	"context"
	"fmt"
	"log/slog"
	"os/signal"
	"sync"
	"syscall"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/lifecycle"
)

const (
	serverDepSTTRouter lifecycle.SharedDepKey = "stt.router"
	serverDepAIGenkit  lifecycle.SharedDepKey = "ai.genkit"
	serverDepTTSRouter lifecycle.SharedDepKey = "tts.router"
)

// NotifySignals wraps signal.NotifyContext for SIGINT + SIGTERM. Calling the
// returned stop func is idempotent and safe from defer.
func NotifySignals(parent context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(parent, syscall.SIGINT, syscall.SIGTERM)
}

func initServerLifecycle(app *App) {
	if app == nil {
		return
	}
	shared := lifecycle.NewSharedDepRegistry()
	shared.Register(serverDepSTTRouter, func(context.Context) (any, func() error, error) {
		return app.STTRouter, nil, nil
	})
	shared.Register(serverDepAIGenkit, func(ctx context.Context) (any, func() error, error) {
		if app.GenkitRuntime == nil {
			if err := ensureSharedAIDeps(ctx, app); err != nil {
				slog.Warn("server lifecycle: shared AI deps unavailable for Genkit runtime", "err", err)
			}
		}
		return app.GenkitRuntime, nil, nil
	})
	shared.Register(serverDepTTSRouter, func(ctx context.Context) (any, func() error, error) {
		if app.TTSRouter == nil {
			if err := ensureSharedAIDeps(ctx, app); err != nil {
				slog.Warn("server lifecycle: shared AI deps unavailable for TTS router", "err", err)
			}
		}
		return app.TTSRouter, nil, nil
	})

	reg := lifecycle.NewRegistry(shared)
	for _, spec := range []struct {
		mode     lifecycle.ModeKey
		requires []lifecycle.SharedDepKey
	}{
		{mode: lifecycle.ModeDictation, requires: []lifecycle.SharedDepKey{serverDepSTTRouter}},
		{mode: lifecycle.ModeAssist, requires: []lifecycle.SharedDepKey{serverDepSTTRouter, serverDepAIGenkit, serverDepTTSRouter}},
		{mode: lifecycle.ModeVoiceAgent, requires: []lifecycle.SharedDepKey{serverDepSTTRouter, serverDepAIGenkit, serverDepTTSRouter}},
	} {
		if err := reg.Register(&serverModeRuntime{name: spec.mode, requires: spec.requires}); err != nil {
			slog.Warn("server lifecycle: mode runtime registration failed", "mode", spec.mode, "err", err)
		}
	}

	app.SharedDeps = shared
	app.Lifecycle = reg
	if err := reg.ApplyWithOptions(context.Background(), lifecycle.Target{
		lifecycle.ModeDictation:  app.ModeEnabled(ModeDictation),
		lifecycle.ModeAssist:     app.ModeEnabled(ModeAssist),
		lifecycle.ModeVoiceAgent: app.ModeEnabled(ModeVoiceAgent),
	}, lifecycle.ApplyOptions{
		EagerWarmup: app.Cfg != nil && app.Cfg.General.EagerWarmup,
	}); err != nil {
		slog.Warn("server lifecycle: applying initial mode target failed", "err", err)
	}
}

type serverModeRuntime struct {
	name     lifecycle.ModeKey
	requires []lifecycle.SharedDepKey

	mu     sync.Mutex
	status lifecycle.Status
}

func (r *serverModeRuntime) Name() lifecycle.ModeKey { return r.name }

func (r *serverModeRuntime) Requires() []lifecycle.SharedDepKey {
	if r == nil || len(r.requires) == 0 {
		return nil
	}
	return append([]lifecycle.SharedDepKey(nil), r.requires...)
}

func (r *serverModeRuntime) Start(_ context.Context, deps lifecycle.Deps) error {
	if r == nil {
		return nil
	}
	for _, key := range r.Requires() {
		if _, ok := deps.Get(key); !ok {
			return fmt.Errorf("missing required dependency %s", key)
		}
	}
	r.mu.Lock()
	r.status = lifecycle.StatusRunning
	r.mu.Unlock()
	return nil
}

func (r *serverModeRuntime) Stop(context.Context) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	r.status = lifecycle.StatusStopped
	r.mu.Unlock()
	return nil
}

func (r *serverModeRuntime) Status() lifecycle.Status {
	if r == nil {
		return lifecycle.StatusStopped
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.status == "" {
		return lifecycle.StatusStopped
	}
	return r.status
}

var _ lifecycle.ModeRuntime = (*serverModeRuntime)(nil)
