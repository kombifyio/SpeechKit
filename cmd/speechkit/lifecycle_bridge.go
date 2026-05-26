package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/kombifyio/SpeechKit/internal/auditlog"
	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/lifecycle"
)

const (
	desktopDepAudioCapture lifecycle.SharedDepKey = "audio.capture"
	desktopDepAIGenkit     lifecycle.SharedDepKey = "ai.genkit"
	desktopDepTTSRouter    lifecycle.SharedDepKey = "tts.router"
)

// desktopLifecycleBridge wires pkg/speechkit/lifecycle.Registry into
// the Wails reference client.
type desktopLifecycleBridge struct {
	registry  *lifecycle.Registry
	sharedDep *lifecycle.SharedDepRegistry
	runtimes  map[lifecycle.ModeKey]*desktopModeRuntime

	unsub    func()
	auditCtx context.Context //nolint:containedctx // ctx held for audit-emitter goroutine lifetime
	wg       sync.WaitGroup
}

type desktopLifecycleBridgeOptions struct {
	Config *config.Config
	State  *appState
}

// newDesktopLifecycleBridge constructs the bridge with the three
// canonical mode runtimes registered. The audit-emitter goroutine
// starts immediately and runs until ctx is cancelled or Shutdown
// is called.
func newDesktopLifecycleBridge(ctx context.Context, opts ...desktopLifecycleBridgeOptions) *desktopLifecycleBridge {
	sd := lifecycle.NewSharedDepRegistry()
	reg := lifecycle.NewRegistry(sd)

	bridge := &desktopLifecycleBridge{
		registry:  reg,
		sharedDep: sd,
		runtimes:  map[lifecycle.ModeKey]*desktopModeRuntime{},
		auditCtx:  ctx,
	}

	var wiring desktopLifecycleBridgeOptions
	if len(opts) > 0 {
		wiring = opts[0]
	}
	bridge.registerSharedDeps(wiring)

	for _, spec := range []struct {
		mode     lifecycle.ModeKey
		requires []lifecycle.SharedDepKey
	}{
		{mode: lifecycle.ModeDictation, requires: []lifecycle.SharedDepKey{desktopDepAudioCapture}},
		{mode: lifecycle.ModeAssist, requires: []lifecycle.SharedDepKey{desktopDepAudioCapture, desktopDepAIGenkit, desktopDepTTSRouter}},
		{mode: lifecycle.ModeVoiceAgent, requires: []lifecycle.SharedDepKey{desktopDepAudioCapture, desktopDepAIGenkit, desktopDepTTSRouter}},
	} {
		rt := &desktopModeRuntime{name: spec.mode, requires: spec.requires}
		if err := reg.Register(rt); err != nil {
			slog.Warn("lifecycle bridge: register failed", "mode", spec.mode, "err", err)
			continue
		}
		bridge.runtimes[spec.mode] = rt
	}

	events, unsub := reg.Subscribe(32)
	bridge.unsub = unsub

	bridge.wg.Add(1)
	go bridge.runAuditEmitter(ctx, events)

	return bridge
}

func (b *desktopLifecycleBridge) registerSharedDeps(opts desktopLifecycleBridgeOptions) {
	if b == nil || b.sharedDep == nil {
		return
	}
	b.sharedDep.Register(desktopDepAudioCapture, func(_ context.Context) (any, func() error, error) {
		if opts.State == nil {
			return nil, nil, nil
		}
		opts.State.mu.Lock()
		audioSession := opts.State.audioSession
		opts.State.mu.Unlock()
		return audioSession, nil, nil
	})
	b.sharedDep.Register(desktopDepAIGenkit, func(ctx context.Context) (any, func() error, error) {
		if opts.Config == nil || opts.State == nil {
			return nil, nil, nil
		}
		ensureSharedDepsForCfg(ctx, opts.Config, opts.State, nil)
		opts.State.mu.Lock()
		rt := opts.State.genkitRT
		opts.State.mu.Unlock()
		// Genkit exposes no public Close. The runtime stays process-scoped;
		// the lifecycle claim still captures cold-start and refcount state.
		return rt, nil, nil
	})
	b.sharedDep.Register(desktopDepTTSRouter, func(ctx context.Context) (any, func() error, error) {
		if opts.Config == nil || opts.State == nil {
			return nil, nil, nil
		}
		ensureSharedDepsForCfg(ctx, opts.Config, opts.State, nil)
		opts.State.mu.Lock()
		router := opts.State.ttsRouter
		opts.State.mu.Unlock()
		return router, func() error {
			teardownDesktopTTSRuntime(opts.State)
			return nil
		}, nil
	})
}

// Apply diffs the bridge's current mode state against the cfg-derived
// target and starts / stops runtimes accordingly. Triggers audit
// events via the subscriber goroutine.
//
// Safe to call before app.Run starts the Wails event loop — the
// underlying Registry uses non-blocking subscriber sends.
func (b *desktopLifecycleBridge) Apply(ctx context.Context, cfg *config.Config) error {
	if b == nil || cfg == nil {
		return nil
	}
	return b.registry.Apply(ctx, b.targetFromConfig(cfg))
}

// targetFromConfig maps the Wails-Desktop cfg fields to the
// lifecycle.Target shape. A mode is "on" only when its Enabled flag
// is true AND it has a non-empty hotkey binding (matching the
// existing applyModeEnabled semantics in settings_routes_modes.go).
func (b *desktopLifecycleBridge) targetFromConfig(cfg *config.Config) lifecycle.Target {
	hotkeyBound := func(s string) bool { return strings.TrimSpace(s) != "" }
	return lifecycle.Target{
		lifecycle.ModeDictation:  cfg.General.DictateEnabled && hotkeyBound(cfg.General.DictateHotkey),
		lifecycle.ModeAssist:     cfg.General.AssistEnabled && hotkeyBound(cfg.General.AssistHotkey),
		lifecycle.ModeVoiceAgent: cfg.General.VoiceAgentEnabled && hotkeyBound(cfg.General.VoiceAgentHotkey),
	}
}

// Snapshot returns the current registry state — used by /readyz-like
// endpoints and by integration tests asserting transitions.
func (b *desktopLifecycleBridge) Snapshot() lifecycle.SnapshotView {
	if b == nil {
		return lifecycle.SnapshotView{}
	}
	return b.registry.Snapshot()
}

// Shutdown stops every running runtime, releases shared deps, and
// closes the audit-emitter goroutine. Idempotent. Safe to call
// from desktopCleanupStack.
func (b *desktopLifecycleBridge) Shutdown(ctx context.Context) error {
	if b == nil {
		return nil
	}
	err := b.registry.Shutdown(ctx)
	if b.unsub != nil {
		b.unsub()
		b.unsub = nil
	}
	b.wg.Wait()
	return err
}

func (b *desktopLifecycleBridge) runAuditEmitter(ctx context.Context, events <-chan lifecycle.TransitionEvent) {
	defer b.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			b.recordTransitionObservability(ctx, ev)
			b.emitAudit(ev)
		}
	}
}

// emitAudit translates a TransitionEvent into the canonical
// audit-log event. Only terminal transitions (Running, Stopped,
// Failed from Starting/Stopping) emit; intermediate Starting /
// Stopping transitions are not audited to keep SIEM volume low.
func (b *desktopLifecycleBridge) emitAudit(ev lifecycle.TransitionEvent) {
	var event auditlog.Event
	var outcome auditlog.Outcome
	releasedOrRequired := "requires"

	switch {
	case ev.To == lifecycle.StatusRunning && ev.From == lifecycle.StatusStarting:
		event = auditlog.EventModeStart
		outcome = auditlog.OutcomeSuccess
	case ev.To == lifecycle.StatusFailed && ev.From == lifecycle.StatusStarting:
		event = auditlog.EventModeStart
		outcome = auditlog.OutcomeFailure
	case ev.To == lifecycle.StatusStopped && ev.From == lifecycle.StatusStopping:
		event = auditlog.EventModeStop
		outcome = auditlog.OutcomeSuccess
		releasedOrRequired = "released"
	case ev.To == lifecycle.StatusFailed && ev.From == lifecycle.StatusStopping:
		event = auditlog.EventModeStop
		outcome = auditlog.OutcomeFailure
		releasedOrRequired = "released"
	default:
		return
	}

	resource := map[string]any{"mode": string(ev.Mode)}
	if rt, ok := b.runtimes[ev.Mode]; ok {
		if claims := rt.Requires(); len(claims) > 0 {
			vals := make([]string, len(claims))
			for i, c := range claims {
				vals[i] = string(c)
			}
			resource[releasedOrRequired] = vals
		}
	}
	if ev.Err != nil {
		resource["error"] = ev.Err.Error()
	}

	if err := auditlog.AppendEvent(b.auditCtx, auditlog.Record{
		Event:    event,
		Resource: resource,
		Outcome:  outcome,
	}); err != nil {
		slog.Warn("lifecycle bridge: audit event emit failed",
			"event", event,
			"mode", ev.Mode,
			"err", err)
	}
}

// desktopModeRuntime is the Wails-Desktop adapter for one of the
// three SpeechKit modes. Phase 1 tracks Status only; Phase 2 will
// add Requires() with real shared-dep keys.
type desktopModeRuntime struct {
	name     lifecycle.ModeKey
	requires []lifecycle.SharedDepKey

	mu     sync.Mutex
	status lifecycle.Status
}

func (r *desktopModeRuntime) Name() lifecycle.ModeKey { return r.name }

func (r *desktopModeRuntime) Status() lifecycle.Status {
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

// Requires returns the SharedDepKeys this mode needs. Phase 1 returns
// nil — the Registry then acquires no shared deps for this mode.
// Phase 2 fills this with the per-mode dep keys (audio.capture for
// all three; ai.genkit + tts.router for Assist and VoiceAgent).
func (r *desktopModeRuntime) Requires() []lifecycle.SharedDepKey {
	if r == nil || len(r.requires) == 0 {
		return nil
	}
	return append([]lifecycle.SharedDepKey(nil), r.requires...)
}

func (r *desktopModeRuntime) Start(_ context.Context, deps lifecycle.Deps) error {
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

func (r *desktopModeRuntime) Stop(_ context.Context) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	r.status = lifecycle.StatusStopped
	r.mu.Unlock()
	return nil
}

// Compile-time interface satisfaction check.
var _ lifecycle.ModeRuntime = (*desktopModeRuntime)(nil)
