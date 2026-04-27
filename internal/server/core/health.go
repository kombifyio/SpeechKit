//go:build linux

package core

import (
	"encoding/json"
	"net/http"
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
	// StatusStarting means the component is initializing and not yet ready.
	StatusStarting ComponentStatus = "starting"
)

// HealthRegistry tracks the readiness of named components. The server binary
// starts in "starting" state and mode packages flip their components to "ok"
// (or "unavailable") once their bootstrap completes.
type HealthRegistry struct {
	mu         sync.RWMutex
	components map[string]componentEntry
	startedAt  time.Time
}

type componentEntry struct {
	Status    ComponentStatus `json:"status"`
	Detail    string          `json:"detail,omitempty"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// NewHealthRegistry returns an empty registry.
func NewHealthRegistry() *HealthRegistry {
	return &HealthRegistry{
		components: map[string]componentEntry{},
		startedAt:  time.Now().UTC(),
	}
}

// SetReady records or updates a component's status.
func (r *HealthRegistry) SetReady(name string, status ComponentStatus, detail string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.components[name] = componentEntry{
		Status:    status,
		Detail:    detail,
		UpdatedAt: time.Now().UTC(),
	}
}

// Snapshot returns a copy of the current component map plus the derived overall
// status. Overall is "ok" iff every component is "ok"; otherwise the worst
// individual status wins (starting < degraded < unavailable).
func (r *HealthRegistry) Snapshot() (overall ComponentStatus, components map[string]componentEntry, uptimeSeconds int64) {
	if r == nil {
		return StatusUnavailable, nil, 0
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	components = make(map[string]componentEntry, len(r.components))
	overall = StatusOK
	for name, entry := range r.components {
		components[name] = entry
		if statusRank(entry.Status) > statusRank(overall) {
			overall = entry.Status
		}
	}
	if len(components) == 0 {
		overall = StatusStarting
	}
	uptimeSeconds = int64(time.Since(r.startedAt).Seconds())
	return overall, components, uptimeSeconds
}

func statusRank(s ComponentStatus) int {
	switch s {
	case StatusOK:
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
// /readyz reflects component readiness: 200 only when every registered
// component reports StatusOK.
func registerHealth(app *App) {
	app.Mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":  "ok",
			"version": app.Version,
		})
	})

	app.Mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		overall, components, uptime := app.Health.Snapshot()
		w.Header().Set("Content-Type", "application/json")
		if overall != StatusOK {
			w.WriteHeader(http.StatusServiceUnavailable)
		} else {
			w.WriteHeader(http.StatusOK)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":         overall,
			"components":     components,
			"uptime_seconds": uptime,
			"version":        app.Version,
		})
	})
}
