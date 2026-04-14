package voiceagent

import (
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// IdleConfig configures the idle timer behavior.
type IdleConfig struct {
	ReminderAfter   time.Duration // Default: 5 minutes
	DeactivateAfter time.Duration // Default: 15 minutes
}

// DefaultIdleConfig returns sensible defaults.
func DefaultIdleConfig() IdleConfig {
	return IdleConfig{
		ReminderAfter:   5 * time.Minute,
		DeactivateAfter: 15 * time.Minute,
	}
}

// IdleTimer manages reminder and auto-deactivation for Voice Agent.
type IdleTimer struct {
	mu              sync.Mutex
	cfg             IdleConfig
	session         *Session
	reminderTimer   *time.Timer
	deactivateTimer *time.Timer
	goodbyeTimer    *time.Timer
	reminded        bool
	stopped         bool
}

// NewIdleTimer creates an idle timer bound to a session.
func NewIdleTimer(cfg IdleConfig, session *Session) *IdleTimer {
	return &IdleTimer{
		cfg:     cfg,
		session: session,
	}
}

// Reset restarts the idle countdown. Call after each user interaction.
func (t *IdleTimer) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.stopped {
		return
	}

	t.reminded = false

	// Reset reminder timer.
	if t.reminderTimer != nil {
		t.reminderTimer.Stop()
	}
	if t.cfg.ReminderAfter > 0 {
		t.reminderTimer = time.AfterFunc(t.cfg.ReminderAfter, t.onReminder)
	}

	// Reset deactivation timer.
	if t.deactivateTimer != nil {
		t.deactivateTimer.Stop()
	}
	if t.cfg.DeactivateAfter > 0 {
		t.deactivateTimer = time.AfterFunc(t.cfg.DeactivateAfter, t.onDeactivate)
	}
}

// Stop cancels all timers.
func (t *IdleTimer) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.stopped = true
	if t.reminderTimer != nil {
		t.reminderTimer.Stop()
	}
	if t.deactivateTimer != nil {
		t.deactivateTimer.Stop()
	}
	if t.goodbyeTimer != nil {
		t.goodbyeTimer.Stop()
	}
}

func (t *IdleTimer) onReminder() {
	t.mu.Lock()
	if t.stopped || t.reminded {
		t.mu.Unlock()
		return
	}
	t.reminded = true
	locale := t.session.locale
	reminderAfter := t.cfg.ReminderAfter
	t.mu.Unlock()

	// Send a text prompt to the model asking it to remind the user.
	prompt := reminderPrompt(locale, reminderAfter)
	slog.Info("voice agent idle reminder triggered")

	if err := t.session.provider.SendText(prompt); err != nil {
		slog.Warn("voice agent failed to send idle reminder", "err", err)
	}
}

func (t *IdleTimer) onDeactivate() {
	t.mu.Lock()
	if t.stopped {
		t.mu.Unlock()
		return
	}
	locale := t.session.locale
	t.mu.Unlock()

	slog.Info("voice agent idle deactivation triggered")

	// Ask the model to say goodbye before closing.
	prompt := deactivatePrompt(locale)
	if err := t.session.provider.SendText(prompt); err != nil {
		slog.Warn("voice agent failed to send deactivation message", "err", err)
	}

	// Give the model a moment to respond, then stop.
	t.mu.Lock()
	t.goodbyeTimer = time.AfterFunc(5*time.Second, func() {
		t.session.Stop()
	})
	t.mu.Unlock()
}

func reminderPrompt(locale string, reminderAfter time.Duration) string {
	minutes := int(reminderAfter.Minutes())
	if minutes <= 0 {
		minutes = int(DefaultIdleConfig().ReminderAfter.Minutes())
	}
	switch locale {
	case "de", "de-DE":
		return fmt.Sprintf("Der Nutzer ist seit %d Minuten still. Frage freundlich und kurz, ob er noch Aufgaben fuer dich hat.",
			minutes)
	default:
		return fmt.Sprintf("The user has been silent for %d minutes. Briefly and friendly ask if they need anything.",
			minutes)
	}
}

func deactivatePrompt(locale string) string {
	switch locale {
	case "de", "de-DE":
		return "Die Session wird beendet wegen Inaktivitaet. Verabschiede dich kurz und freundlich."
	default:
		return "The session is ending due to inactivity. Say a brief, friendly goodbye."
	}
}
