package cascaded

import (
	"sync"
	"time"
)

// TimingReporter is an optional [Agent] extension: after a turn it reports the
// phases it measured itself (for a remote agent, request to response headers and
// to the first answer text), in milliseconds. The provider attaches them to the
// turn's closing message next to its own phases.
type TimingReporter interface {
	LastTurnTiming() map[string]int64
}

// turnTiming collects the milliseconds a turn spends in each phase, measured
// from the start of the turn's agent leg. It carries durations only.
type turnTiming struct {
	mu    sync.Mutex
	start time.Time
	marks map[string]int64
}

func newTurnTiming() *turnTiming {
	return &turnTiming{start: time.Now(), marks: make(map[string]int64)}
}

// mark records the elapsed time of a phase once; later marks of the same name are ignored.
func (t *turnTiming) mark(name string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, seen := t.marks[name]; !seen {
		t.marks[name] = time.Since(t.start).Milliseconds()
	}
}

func (t *turnTiming) set(name string, ms int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.marks[name] = ms
}

// snapshot returns the recorded phases, the elapsed total and the agent's own report.
func (t *turnTiming) snapshot(agent Agent) map[string]any {
	t.mu.Lock()
	out := make(map[string]any, len(t.marks)+4)
	for name, ms := range t.marks {
		out[name] = ms
	}
	out["total_ms"] = time.Since(t.start).Milliseconds()
	t.mu.Unlock()
	if reporter, ok := agent.(TimingReporter); ok {
		for name, ms := range reporter.LastTurnTiming() {
			out["agent_"+name] = ms
		}
	}
	return out
}

// noteSTT keeps the last speech-recognition duration for the turn that follows it.
func (p *Provider) noteSTT(d time.Duration) {
	p.mu.Lock()
	p.pendingSTT = d.Milliseconds()
	p.sttNoted = true
	p.mu.Unlock()
}

func (p *Provider) takeSTT() (int64, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	ms, ok := p.pendingSTT, p.sttNoted
	p.pendingSTT, p.sttNoted = 0, false
	return ms, ok
}
