package lifecycle

import "strings"

// Status is a mode runtime's observable lifecycle state.
type Status string

const (
	// StatusStopped means the mode has never started, or has been cleanly
	// torn down. No goroutines, no live shared-dep references.
	StatusStopped Status = "stopped"

	// StatusStarting means a Start call is in flight but has not yet
	// returned. Shared deps are being acquired or the runtime is wiring up.
	StatusStarting Status = "starting"

	// StatusRunning means the mode is live and serving requests.
	StatusRunning Status = "running"

	// StatusStopping means a Stop call is in flight. The runtime is
	// draining and will release its shared deps once Stop returns.
	StatusStopping Status = "stopping"

	// StatusFailed means the last transition aborted with an error. The
	// runtime is back at rest from the registry's point of view; a fresh
	// Start may be attempted.
	StatusFailed Status = "failed"

	// StatusDisabled means an operator has explicitly turned the mode off
	// (e.g. config flag), distinct from Stopped (transient) — disabled
	// modes never auto-start and do not block readiness checks.
	// See internal/server/core/health.go for the analogous distinction
	// at the HTTP /readyz surface.
	StatusDisabled Status = "disabled"
)

// IsActive reports whether the runtime is participating in serving
// traffic. Starting and Running are active; everything else is not.
func (s Status) IsActive() bool {
	switch s {
	case StatusStarting, StatusRunning:
		return true
	default:
		return false
	}
}

// IsTerminal reports whether the runtime is at rest — neither serving
// nor in the middle of a transition. Hosts can take fresh action
// (Start, retry, surface error) only on terminal statuses.
func (s Status) IsTerminal() bool {
	switch s {
	case StatusStopped, StatusFailed, StatusDisabled:
		return true
	default:
		return false
	}
}

// Rank orders statuses for "worst-status-wins" aggregation across
// multiple modes. Higher rank = more attention required. The ordering
// mirrors core/health.go statusRank semantics so dashboards display
// consistent overall state.
//
//	Stopped  / Disabled → 0
//	Starting / Stopping → 1
//	Running             → 2 (not "worst" — Running is the goal)
//	Failed              → 3
//
// Note that Running ranks above Starting because callers that compare
// statuses across modes want to detect transitions (Starting) as a
// distinct "interesting" state without elevating it above a steady
// Running mode. Failed always wins.
func (s Status) Rank() int {
	switch s {
	case StatusStopped, StatusDisabled:
		return 0
	case StatusStarting, StatusStopping:
		return 1
	case StatusRunning:
		return 2
	case StatusFailed:
		return 3
	default:
		return 3
	}
}

// String implements fmt.Stringer.
func (s Status) String() string {
	if strings.TrimSpace(string(s)) == "" {
		return string(StatusStopped)
	}
	return string(s)
}
