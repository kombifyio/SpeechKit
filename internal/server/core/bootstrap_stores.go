//go:build linux

package core

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/server/persona"
	"github.com/kombifyio/SpeechKit/internal/store"
)

// ensureStore opens the configured durable store. Idempotent — the first
// successful call populates app.Store; subsequent calls are no-ops. If
// the backend is unconfigured or the driver fails, we log and continue:
// Voice Agent mode still works (personas stay in-memory), dictation still
// serves requests, and /readyz surfaces the missing capability.
func ensureStore(cfg *config.Config, app *App) {
	if app.Store != nil {
		return
	}
	backend := strings.TrimSpace(cfg.Store.Backend)
	if backend == "" {
		backend = "sqlite"
	}
	storeCfg := store.StoreConfig{
		Backend:            backend,
		SQLitePath:         cfg.Store.SQLitePath,
		PostgresDSN:        cfg.Store.PostgresDSN,
		SaveAudio:          cfg.Store.SaveAudio,
		AudioRetentionDays: cfg.Store.AudioRetentionDays,
		MaxAudioStorageMB:  cfg.Store.MaxAudioStorageMB,
	}
	s, err := store.New(storeCfg)
	if err != nil {
		slog.Warn("store init failed; durable features disabled", "backend", backend, "err", err)
		app.Health.SetReady("store", StatusDegraded, err.Error())
		return
	}
	app.Store = s
	app.Health.SetReady("store", StatusOK, backend)
	slog.Info("store initialized", "backend", backend)
}

// ensurePersonaRegistry lazily initializes the persona catalog.
//
// Boot order:
//  1. Build an empty registry.
//  2. If the Store exposes a *sql.DB (SQLite backend), hydrate previously
//     persisted entries FIRST so admin-authored overrides are in place,
//     then attach a Persister so subsequent admin writes survive restart.
//  3. Overlay TOML seeds on top — TOML acts as a baseline of defaults
//     that admin writes can replace per ID.
//
// Idempotent — second+ calls are no-ops.
func ensurePersonaRegistry(ctx context.Context, cfg *config.Config, app *App) {
	if app.PersonaRegistry != nil {
		return
	}
	reg := persona.NewRegistry()

	// (2) store-backed persistence, opt-in per concrete store type so
	// bootstrap stays compile-time explicit about durable backends.
	switch concreteStore := app.Store.(type) {
	case *store.SQLiteStore:
		persister := persona.NewSQLitePersister(concreteStore.DB())
		if err := reg.HydrateFrom(ctx, persister); err != nil {
			slog.Warn("persona: hydrate from store failed; falling back to TOML-only", "err", err)
		} else {
			slog.Info("persona: hydrated from SQLite store")
		}
		reg.WithPersister(persister)
	case *store.PostgresStore:
		persister := persona.NewPostgresPersister(concreteStore.DB())
		if err := reg.HydrateFrom(ctx, persister); err != nil {
			slog.Warn("persona: hydrate from Postgres store failed; falling back to TOML-only", "err", err)
		} else {
			slog.Info("persona: hydrated from Postgres store")
		}
		reg.WithPersister(persister)
	default:
		if app.Store != nil {
			slog.Info("persona: store backend does not support durable personas; admin writes are in-memory only")
		}
	}

	// (3) overlay TOML seeds. Seeds are tagged Source="toml" and never
	// round-trip to the persister — they're the baseline, not data.
	notes := persona.LoadSeeds(reg, cfg) //nolint:contextcheck // seed loading is in-memory TOML overlay work with no request or cancellable I/O.
	for _, note := range notes {
		slog.Debug("persona seed", "note", note)
	}
	app.PersonaRegistry = reg

	personaCount := len(reg.ListPersonas())
	roleCount := len(reg.ListRoles())
	sequenceCount := len(reg.ListSequences())
	detail := fmt.Sprintf("%d personas, %d roles, %d sequences", personaCount, roleCount, sequenceCount)
	if personaCount == 0 {
		app.Health.SetReady("persona.registry", StatusDegraded, "no personas seeded; clients must create one via POST /v1/personas")
	} else {
		app.Health.SetReady("persona.registry", StatusOK, detail)
	}
	slog.Info("persona registry ready", "summary", detail)
}
