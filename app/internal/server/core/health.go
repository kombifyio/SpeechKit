//go:build linux

package core

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ComponentStatus describes a single component's health.
type ComponentStatus string

const (
	// StatusOK means the component is fully available.
	StatusOK ComponentStatus = "ok"
	// StatusDegraded means the component is reachable but reporting errors.
	StatusDegraded ComponentStatus = "degraded"
	// StatusUnavailable means the component cannot be used.
	StatusUnavailable ComponentStatus = "unavailable"
	// StatusDisabled means the component is intentionally turned off by
	// configuration. Unlike StatusUnavailable this is a deliberate
	// operator choice, not a failure, so it does not degrade strict
	// readiness.
	StatusDisabled ComponentStatus = "disabled"
	// StatusStarting means the component is initializing and not yet ready.
	StatusStarting ComponentStatus = "starting"
)

// HealthRegistry tracks the readiness of named components. The server binary
// starts in "starting" state and mode packages flip their components to "ok"
// (or "unavailable") once their bootstrap completes.
type HealthRegistry struct {
	mu         sync.RWMutex
	components map[string]ComponentEntry
	startedAt  time.Time
}

type ComponentEntry struct {
	Status    ComponentStatus `json:"status"`
	Detail    string          `json:"detail,omitempty"`
	UpdatedAt time.Time       `json:"updated_at"`
	Blocking  bool            `json:"blocking"`
	Kind      string          `json:"kind,omitempty"`
	Modes     []string        `json:"modes,omitempty"`
	Provider  string          `json:"provider,omitempty"`
}

type ComponentOptions struct {
	Blocking bool
	Kind     string
	Modes    []string
	Provider string
}

// NewHealthRegistry returns an empty registry.
func NewHealthRegistry() *HealthRegistry {
	return &HealthRegistry{
		components: map[string]ComponentEntry{},
		startedAt:  time.Now().UTC(),
	}
}

// SetReady records or updates a component's status.
func (r *HealthRegistry) SetReady(name string, status ComponentStatus, detail string) {
	r.SetReadyWithOptions(name, status, detail, ComponentOptions{Blocking: true})
}

func (r *HealthRegistry) SetReadyWithOptions(name string, status ComponentStatus, detail string, opts ComponentOptions) {
	if r == nil {
		return
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.components[name] = ComponentEntry{
		Status:    status,
		Detail:    detail,
		UpdatedAt: time.Now().UTC(),
		Blocking:  opts.Blocking,
		Kind:      strings.TrimSpace(opts.Kind),
		Modes:     cleanStrings(opts.Modes),
		Provider:  strings.TrimSpace(opts.Provider),
	}
}

// Snapshot returns a copy of the current component map plus strict readiness.
// Strict readiness is "ok" iff every component is "ok"; otherwise the worst
// individual status wins (starting < degraded < unavailable).
func (r *HealthRegistry) Snapshot() (overall ComponentStatus, components map[string]ComponentEntry, uptimeSeconds int64) {
	return r.snapshot(false)
}

func (r *HealthRegistry) ReadinessSnapshot() (overall ComponentStatus, components map[string]ComponentEntry, uptimeSeconds int64) {
	return r.snapshot(true)
}

func (r *HealthRegistry) snapshot(blockingOnly bool) (overall ComponentStatus, components map[string]ComponentEntry, uptimeSeconds int64) {
	if r == nil {
		return StatusUnavailable, nil, 0
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	components = make(map[string]ComponentEntry, len(r.components))
	overall = StatusOK
	considered := 0
	for name, entry := range r.components {
		components[name] = entry
		if blockingOnly && !entry.Blocking {
			continue
		}
		considered++
		if statusRank(entry.Status) > statusRank(overall) {
			overall = entry.Status
		}
	}
	if considered == 0 {
		overall = StatusStarting
	}
	uptimeSeconds = int64(time.Since(r.startedAt).Seconds())
	return overall, components, uptimeSeconds
}

func cleanStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func statusRank(s ComponentStatus) int {
	switch s {
	case StatusOK, StatusDisabled:
		return 0
	case StatusStarting:
		return 1
	case StatusDegraded:
		return 2
	case StatusUnavailable:
		return 3
	default:
		return 3
	}
}

// registerHealth wires /healthz and /readyz handlers onto the app mux.
//
// /healthz is a pure liveness probe: returns 200 as long as the process is up.
// /readyz reflects mode readiness: 200 when every blocking component reports
// StatusOK. /readyz/strict requires every registered component to report OK.
func registerHealth(app *App) {
	app.Mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":  "ok",
			"version": app.Version,
		})
	})

	readyHandler := func(w http.ResponseWriter, r *http.Request) {
		strict := r.URL.Query().Get("strict") == "true"
		overall, components, uptime := app.Health.ReadinessSnapshot()
		strictOverall, _, _ := app.Health.Snapshot()
		if strict {
			overall = strictOverall
		}
		w.Header().Set("Content-Type", "application/json")
		if overall != StatusOK {
			w.WriteHeader(http.StatusServiceUnavailable)
		} else {
			w.WriteHeader(http.StatusOK)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":         overall,
			"strict_status":  strictOverall,
			"components":     components,
			"uptime_seconds": uptime,
			"version":        app.Version,
		})
	}

	app.Mux.HandleFunc("/readyz", readyHandler)
	app.Mux.HandleFunc("/readyz/strict", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		q.Set("strict", "true")
		r.URL.RawQuery = q.Encode()
		readyHandler(w, r)
	})
}
