package tray

import (
	"log/slog"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"

	appassets "github.com/kombifyio/SpeechKit/assets"
)

// State represents the current tray status.
type State string

const (
	StateIdle       State = "idle"
	StateRecording  State = "recording"
	StateProcessing State = "processing"
	StateDone       State = "done"
)

var stateTooltips = map[State]string{
	StateIdle:       "SpeechKit",
	StateRecording:  "SpeechKit - Recording",
	StateProcessing: "SpeechKit - Processing",
	StateDone:       "SpeechKit - Done",
}

// Tray manages the Wails v3 native system tray.
type Tray struct {
	systray       *application.SystemTray
	state         State
	wakeListening bool
	mu            sync.Mutex

	OnQuit     func()
	OnSettings func()
	OnFeedback func()
}

// New creates a system tray with menu and icons.
func New(app *application.App, onQuit, onSettings func()) *Tray {
	t := &Tray{
		state:      StateIdle,
		OnQuit:     onQuit,
		OnSettings: onSettings,
	}

	icon := appassets.BubbleIcon()
	t.systray = app.SystemTray.New()
	t.systray.SetIcon(icon)
	t.systray.SetDarkModeIcon(icon)
	t.systray.SetTooltip(stateTooltips[StateIdle])

	menu := app.NewMenu()
	menu.Add("Dashboard").OnClick(func(ctx *application.Context) {
		slog.Info("tray.action", "action", "dashboard", "source", "menu")
		if t.OnSettings != nil {
			t.OnSettings()
		}
	})
	menu.Add("Send Feedback").OnClick(func(ctx *application.Context) {
		slog.Info("tray.action", "action", "feedback", "source", "menu")
		if t.OnFeedback != nil {
			t.OnFeedback()
		}
	})
	menu.AddSeparator()
	menu.Add("Quit SpeechKit").OnClick(func(ctx *application.Context) {
		slog.Info("tray.action", "action", "quit", "source", "menu")
		if t.OnQuit != nil {
			t.OnQuit()
		}
	})
	t.systray.SetMenu(menu)
	t.systray.OnDoubleClick(func() {
		slog.Info("tray.action", "action", "dashboard", "source", "double_click")
		if t.OnSettings != nil {
			t.OnSettings()
		}
	})
	t.systray.OnRightClick(func() {
		slog.Info("tray.action", "action", "open_menu", "source", "right_click")
		t.systray.OpenMenu()
	})

	return t
}

// tooltipFor resolves the display tooltip for a tray state, with a stable
// fallback when the state is unrecognized. When wakeListening is true,
// the privacy-indicator suffix " - Wake on" is appended so users always
// know the mic is open for wake detection.
func tooltipFor(state State, wakeListening bool) string {
	base, ok := stateTooltips[state]
	if !ok || base == "" {
		base = "SpeechKit"
	}
	if wakeListening {
		return base + " - Wake on"
	}
	return base
}

// SetState updates the tray tooltip to reflect the current state.
func (t *Tray) SetState(state State) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.state == state {
		return
	}
	t.state = state

	t.systray.SetTooltip(tooltipFor(state, t.wakeListening))
}

// SetWakeListening flips the always-on wake-word indicator. Called by the
// Device-Target whenever the wake runtime starts or stops so the user
// always knows when the mic is open for wake detection (privacy contract).
func (t *Tray) SetWakeListening(listening bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.wakeListening == listening {
		return
	}
	t.wakeListening = listening
	t.systray.SetTooltip(tooltipFor(t.state, listening))
}
