//go:build linux

package voiceagent

import (
	"sync"
	"time"

	"github.com/kombifyio/SpeechKit/app/internal/server/wssession"
)

// voiceBudgetLease owns a live session's reserved-media deadline. A renewal
// replaces the Cloud reservation and moves the deadline forward; the time
// spent on the previous reservation is settled as its own segment, because a
// Cloud reservation settles exactly once and never beyond its grant.
type voiceBudgetLease struct {
	mu          sync.Mutex
	budget      wssession.VoiceBudget
	segment     int
	since       time.Time // start of the current segment; zero for the first
	connectedAt time.Time
	provider    string
	timer       *time.Timer
	expired     bool
	stopped     bool
	onExpire    func()
}

// voiceBudgetSegment is the settled share of one reservation.
type voiceBudgetSegment struct {
	budget   wssession.VoiceBudget
	segment  int
	provider string
	duration time.Duration
}

func newVoiceBudgetLease(budget wssession.VoiceBudget, onExpire func()) *voiceBudgetLease {
	l := &voiceBudgetLease{budget: budget, onExpire: onExpire}
	l.timer = time.AfterFunc(time.Until(time.Unix(budget.ExpiresAt, 0)), l.fire)
	return l
}

func (l *voiceBudgetLease) fire() {
	l.mu.Lock()
	if l.stopped {
		l.mu.Unlock()
		return
	}
	l.expired = true
	l.mu.Unlock()
	l.onExpire()
}

// ended reports whether the current reservation can no longer admit media.
func (l *voiceBudgetLease) ended() bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.expired || !time.Now().Before(time.Unix(l.budget.ExpiresAt, 0))
}

func (l *voiceBudgetLease) markConnected(at time.Time, provider string) {
	l.mu.Lock()
	l.connectedAt, l.provider = at, provider
	l.mu.Unlock()
}

// renew adopts a strictly later reservation while the current one is still
// live and returns the previous reservation's settled segment.
func (l *voiceBudgetLease) renew(next wssession.VoiceBudget, now time.Time) (voiceBudgetSegment, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.stopped || l.expired || !now.Before(time.Unix(l.budget.ExpiresAt, 0)) ||
		next.ExpiresAt <= l.budget.ExpiresAt || !l.timer.Stop() {
		return voiceBudgetSegment{}, wssession.ErrVoiceBudgetNotRenewable
	}
	previous := l.segmentLocked(now)
	l.budget, l.segment, l.since = next, l.segment+1, now
	l.timer = time.AfterFunc(time.Until(time.Unix(next.ExpiresAt, 0)), l.fire)
	return previous, nil
}

// finish stops the deadline and returns the current reservation's segment.
func (l *voiceBudgetLease) finish(endedAt time.Time) voiceBudgetSegment {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.stopped = true
	l.timer.Stop()
	return l.segmentLocked(endedAt)
}

func (l *voiceBudgetLease) segmentLocked(end time.Time) voiceBudgetSegment {
	if expiry := time.Unix(l.budget.ExpiresAt, 0); end.After(expiry) {
		end = expiry
	}
	start := l.connectedAt
	if l.since.After(start) {
		start = l.since
	}
	duration := time.Duration(0)
	if !l.connectedAt.IsZero() && end.After(start) {
		duration = end.Sub(start)
	}
	return voiceBudgetSegment{budget: l.budget, segment: l.segment, provider: l.provider, duration: duration}
}
