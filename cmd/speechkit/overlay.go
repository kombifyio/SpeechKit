package main

import (
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/kombifyio/SpeechKit/internal/models"
	"github.com/kombifyio/SpeechKit/internal/tray"
)

// This file holds the shared types, constants, and interfaces for the overlay
// subsystem. The implementation is split across overlay_layout.go (pure
// geometry + window options), overlay_lifecycle.go (appState show/hide
// methods), overlay_prompter.go (prompter + voice-agent stream), and
// overlay_snapshot.go (state observation + snapshots).

const (
	// Legacy single-window overlay size. Kept for compatibility with older tests/helpers.
	overlayWindowSize = 300

	overlayEdgeMargin = 6

	pillBubbleW = 80
	pillBubbleH = 36
	dotBubbleW  = 20
	dotBubbleH  = 20

	pillAnchorWidth  = 88
	pillAnchorHeight = 32
	pillPanelWidth   = 240
	pillPanelHeight  = 32
	dotAnchorSize    = 24
	radialMenuSize   = 120

	assistBubbleWidth  = 450
	assistBubbleHeight = 120

	prompterWidth  = 860
	prompterHeight = 640

	overlaySpeakingThreshold = 0.18
	overlayVisualizerGain    = 14.0
	overlayVisualizerFloor   = 0.06
	defaultDoneResetDelay    = 2200 * time.Millisecond
)

type overlayHostMetrics struct {
	Width  int
	Height int
}

var (
	overlayWindowMetrics = overlayHostMetrics{Width: overlayWindowSize, Height: overlayWindowSize}
	pillAnchorMetrics    = overlayHostMetrics{Width: pillAnchorWidth, Height: pillAnchorHeight}
	pillPanelMetrics     = overlayHostMetrics{Width: pillPanelWidth, Height: pillPanelHeight}
	dotAnchorMetrics     = overlayHostMetrics{Width: dotAnchorSize, Height: dotAnchorSize}
	radialMenuMetrics    = overlayHostMetrics{Width: radialMenuSize, Height: radialMenuSize}
)

type screenBounds struct {
	X, Y, Width, Height int
}

type overlayScreenLocator interface {
	OverlayScreenBounds() (screenBounds, bool)
}

type overlayWindow interface {
	Show() application.Window
	Hide() application.Window
	Minimise() application.Window
	IsVisible() bool
	ExecJS(string)
	SetIgnoreMouseEvents(bool) application.Window
	SetPosition(int, int)
	SetSize(int, int) application.Window
}

type settingsWindow interface {
	ExecJS(string)
	Show() application.Window
	IsVisible() bool
	Restore()
	UnMinimise()
	Focus()
}

type trayStateSetter interface {
	SetState(tray.State)
}

type modeAvailabilitySnapshot struct {
	Dictate    bool `json:"dictate"`
	Assist     bool `json:"assist"`
	VoiceAgent bool `json:"voice_agent"`
}

// bubbleRegion is retained for legacy tests only.
type bubbleRegion struct {
	X, Y, W, H int
}

type overlaySnapshot struct {
	State                    string                   `json:"state"`
	Phase                    string                   `json:"phase"`
	Text                     string                   `json:"text"`
	Level                    float64                  `json:"level"`
	Visible                  bool                     `json:"visible"`
	Visualizer               string                   `json:"visualizer"`
	Design                   string                   `json:"design"`
	Hotkey                   string                   `json:"hotkey"`
	DictateHotkey            string                   `json:"dictateHotkey"`
	AssistHotkey             string                   `json:"assistHotkey"`
	VoiceAgentHotkey         string                   `json:"voiceAgentHotkey"`
	DictateHotkeyBehavior    string                   `json:"dictateHotkeyBehavior"`
	AssistHotkeyBehavior     string                   `json:"assistHotkeyBehavior"`
	VoiceAgentHotkeyBehavior string                   `json:"voiceAgentHotkeyBehavior"`
	ModeEnabled              modeAvailabilitySnapshot `json:"modeEnabled"`
	AvailableModes           modeAvailabilitySnapshot `json:"availableModes"`
	AgentHotkey              string                   `json:"agentHotkey"`
	ActiveMode               string                   `json:"activeMode"`
	Position                 string                   `json:"position"`
	Movable                  bool                     `json:"movable"`
	PositionFreeX            int                      `json:"positionFreeX"`
	PositionFreeY            int                      `json:"positionFreeY"`
	LastTranscription        string                   `json:"lastTranscription"`
	QuickNoteMode            bool                     `json:"quickNoteMode"`
	AudioDeviceID            string                   `json:"audioDeviceId"`
	SelectedAudioDeviceID    string                   `json:"selectedAudioDeviceId"`
	ActiveProfiles           map[string]string        `json:"activeProfiles"`
}

type settingsSnapshot struct {
	OverlayEnabled             bool                                  `json:"overlayEnabled"`
	OverlayPosition            string                                `json:"overlayPosition"`
	OverlayMovable             bool                                  `json:"overlayMovable"`
	OverlayFreeX               int                                   `json:"overlayFreeX"`
	OverlayFreeY               int                                   `json:"overlayFreeY"`
	StoreBackend               string                                `json:"storeBackend"`
	SQLitePath                 string                                `json:"sqlitePath"`
	PostgresConfigured         bool                                  `json:"postgresConfigured"`
	PostgresDSN                string                                `json:"postgresDSN,omitempty"`
	MaxAudioStorageMB          int                                   `json:"maxAudioStorageMB"`
	HFAvailable                bool                                  `json:"hfAvailable"`
	HFEnabled                  bool                                  `json:"hfEnabled"`
	HFHasUserToken             bool                                  `json:"hfHasUserToken"`
	HFHasInstallToken          bool                                  `json:"hfHasInstallToken"`
	HFTokenSource              string                                `json:"hfTokenSource"`
	Hotkey                     string                                `json:"hotkey"`
	DictateHotkey              string                                `json:"dictateHotkey"`
	AssistHotkey               string                                `json:"assistHotkey"`
	VoiceAgentHotkey           string                                `json:"voiceAgentHotkey"`
	DictateHotkeyBehavior      string                                `json:"dictateHotkeyBehavior"`
	AssistHotkeyBehavior       string                                `json:"assistHotkeyBehavior"`
	VoiceAgentHotkeyBehavior   string                                `json:"voiceAgentHotkeyBehavior"`
	VoiceAgentCloseBehavior    string                                `json:"voiceAgentCloseBehavior"`
	VoiceAgentRefinementPrompt string                                `json:"voiceAgentRefinementPrompt"`
	AutoStartOnLaunch          bool                                  `json:"autoStartOnLaunch"`
	VoiceAgentAutoStart        bool                                  `json:"voiceAgentAutoStart,omitempty"`
	ModeEnabled                modeAvailabilitySnapshot              `json:"modeEnabled"`
	AvailableModes             modeAvailabilitySnapshot              `json:"availableModes"`
	AgentHotkey                string                                `json:"agentHotkey"`
	AgentMode                  string                                `json:"agentMode"`
	ActiveMode                 string                                `json:"activeMode"`
	HFModel                    string                                `json:"hfModel"`
	Visualizer                 string                                `json:"visualizer"`
	Design                     string                                `json:"design"`
	Language                   string                                `json:"language"`
	VocabularyDictionary       string                                `json:"vocabularyDictionary"`
	SaveAudio                  bool                                  `json:"saveAudio"`
	AudioRetentionDays         int                                   `json:"audioRetentionDays"`
	AudioDeviceID              string                                `json:"audioDeviceId"`
	SelectedAudioDeviceID      string                                `json:"selectedAudioDeviceId"`
	Profiles                   []models.Profile                      `json:"profiles"`
	ActiveProfiles             map[string]string                     `json:"activeProfiles"`
	ModelSelections            map[string]modeModelSelectionSnapshot `json:"modelSelections"`
	ProviderCredentials        map[string]providerCredentialState    `json:"providerCredentials"`
}
