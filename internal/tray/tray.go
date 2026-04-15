package tray

import (
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
	systray *application.SystemTray
	state   State
	mu      sync.Mutex

	OnQuit     func()
	OnSettings func()
	OnFeedback func()
}

// New creates a system tray with menu and icons.
func New(app *application.App, onQuit func(), onSettings func()) *Tray {
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
		if t.OnSettings != nil {
			t.OnSettings()
		}
	})
	menu.Add("Send Feedback").OnClick(func(ctx *application.Context) {
		if t.OnFeedback != nil {
			t.OnFeedback()
		}
	})
	menu.AddSeparator()
	menu.Add("Quit SpeechKit").OnClick(func(ctx *application.Context) {
		if t.OnQuit != nil {
			t.OnQuit()
		}
	})
	t.systray.SetMenu(menu)
	t.systray.OnDoubleClick(func() {
		if t.OnSettings != nil {
			t.OnSettings()
		}
	})
	t.systray.OnRightClick(func() {
		t.systray.OpenMenu()
	})

	return t
}

// SetState updates the tray tooltip to reflect the current state.
func (t *Tray) SetState(state State) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.state == state {
		return
	}
	t.state = state

	tooltip := stateTooltips[state]
	if tooltip == "" {
		tooltip = "SpeechKit"
	}
	t.systray.SetTooltip(tooltip)
}
