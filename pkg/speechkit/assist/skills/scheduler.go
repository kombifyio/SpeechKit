package skills

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Alarm describes a Timer or Reminder that has just fired. The host's
// [Options.OnAlarm] callback receives it and rings a sound / shows a
// notification.
type Alarm struct {
	ID     string    // stable per-catalog id, e.g. "timer-3"
	Kind   string    // "timer" or "reminder"
	Label  string    // spoken duration ("5 minutes") or reminder text
	Locale string    // caller locale ("de"/"en")
	FireAt time.Time // wall-clock instant the alarm was scheduled to fire
}

var errSchedulerClosed = errors.New("speechkit skills: scheduler is closed")

// scheduler is the default in-process backend for the Timer and Reminder
// skills. It arms a real timer per request and invokes onAlarm on a background
// goroutine when it elapses, so Timer/Reminder actually fire without the host
// implementing its own scheduler — the host only supplies the OnAlarm sound.
type scheduler struct {
	onAlarm func(Alarm)

	mu     sync.Mutex
	seq    int
	timers map[string]*time.Timer
	closed bool
}

func newScheduler(onAlarm func(Alarm)) *scheduler {
	return &scheduler{onAlarm: onAlarm, timers: make(map[string]*time.Timer)}
}

// arm schedules alarm a to fire after duration d and returns its id.
func (s *scheduler) arm(d time.Duration, a Alarm) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return "", errSchedulerClosed
	}
	if d < 0 {
		d = 0
	}
	s.seq++
	a.ID = fmt.Sprintf("%s-%d", a.Kind, s.seq)
	id := a.ID
	s.timers[id] = time.AfterFunc(d, func() { s.fire(id, a) })
	return id, nil
}

func (s *scheduler) fire(id string, a Alarm) {
	s.mu.Lock()
	delete(s.timers, id)
	onAlarm := s.onAlarm
	closed := s.closed
	s.mu.Unlock()
	if closed || onAlarm == nil {
		return
	}
	onAlarm(a)
}

// Close stops every pending timer and rejects further scheduling. Safe to call
// more than once.
func (s *scheduler) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	for id, t := range s.timers {
		t.Stop()
		delete(s.timers, id)
	}
}

// timerSink adapts the scheduler to voice_companion.TimerSink (duration-based).
type timerSink struct{ s *scheduler }

func (t timerSink) Schedule(_ context.Context, d time.Duration, label, locale string) (string, error) {
	return t.s.arm(d, Alarm{Kind: "timer", Label: label, Locale: locale, FireAt: time.Now().Add(d)})
}

// reminderSink adapts the scheduler to voice_companion.ReminderSink
// (absolute-time-based).
type reminderSink struct{ s *scheduler }

func (r reminderSink) Schedule(_ context.Context, fireAt time.Time, label, locale string) (string, error) {
	return r.s.arm(time.Until(fireAt), Alarm{Kind: "reminder", Label: label, Locale: locale, FireAt: fireAt})
}
