package main

import (
	"github.com/kombifyio/SpeechKit/cmd/speechkit/internal/profiles"
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
	// Keep aligned with frontend circle shell (`h-[18px] w-[18px]`) so
	// edge anchoring uses the true visible dot footprint.
	dotBubbleW = 18
	dotBubbleH = 18

	pillAnchorWidth    = pillBubbleW
	pillAnchorHeight   = pillBubbleH
	pillPanelWidth     = 240
	pillPanelHeight    = 40
	pillFeedbackWidth  = 380
	pillFeedbackHeight = 128
	dotAnchorSize      = 24
	radialMenuSize     = 120

	assistBubbleWidth  = 450
	assistBubbleHeight = 120
	assistPanelWidth   = 570
	assistPanelHeight  = 360

	prompterWidth           = 390
	prompterHeight          = 500
	prompterCollapsedWidth  = 340
	prompterCollapsedHeight = 132

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
	pillFeedbackMetrics  = overlayHostMetrics{Width: pillFeedbackWidth, Height: pillFeedbackHeight}
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
	SetWakeListening(bool)
}

type modeAvailabilitySnapshot struct {
	Dictate    bool `json:"dictate"`
	Assist     bool `json:"assist"`
	VoiceAgent bool `json:"voice_agent"`
}

type voiceAgentProfileSnapshot struct {
	ID                string `json:"id"`
	DisplayName       string `json:"displayName"`
	Description       string `json:"description,omitempty"`
	Voice             string `json:"voice,omitempty"`
	DefaultSequenceID string `json:"defaultSequenceId,omitempty"`
	BuiltIn           bool   `json:"builtIn,omitempty"`
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
	AssistOverlayMode        string                   `json:"assistOverlayMode"`
	VoiceAgentOverlayMode    string                   `json:"voiceAgentOverlayMode"`
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
	AudioOutputDeviceID      string                   `json:"audioOutputDeviceId"`
	SelectedOutputDeviceID   string                   `json:"selectedOutputDeviceId"`
	ActiveProfiles           map[string]string        `json:"activeProfiles"`
}

type settingsSnapshot struct {
	OverlayEnabled             bool                                           `json:"overlayEnabled"`
	OverlayPosition            string                                         `json:"overlayPosition"`
	OverlayMovable             bool                                           `json:"overlayMovable"`
	OverlayFreeX               int                                            `json:"overlayFreeX"`
	OverlayFreeY               int                                            `json:"overlayFreeY"`
	StoreBackend               string                                         `json:"storeBackend"`
	SQLitePath                 string                                         `json:"sqlitePath"`
	PostgresConfigured         bool                                           `json:"postgresConfigured"`
	PostgresDSN                string                                         `json:"postgresDSN,omitempty"`
	MaxAudioStorageMB          int                                            `json:"maxAudioStorageMB"`
	ModelDownloadDir           string                                         `json:"modelDownloadDir"`
	HFAvailable                bool                                           `json:"hfAvailable"`
	HFEnabled                  bool                                           `json:"hfEnabled"`
	HFHasUserToken             bool                                           `json:"hfHasUserToken"`
	HFHasInstallToken          bool                                           `json:"hfHasInstallToken"`
	HFTokenSource              string                                         `json:"hfTokenSource"`
	Hotkey                     string                                         `json:"hotkey"`
	DictateHotkey              string                                         `json:"dictateHotkey"`
	AssistHotkey               string                                         `json:"assistHotkey"`
	VoiceAgentHotkey           string                                         `json:"voiceAgentHotkey"`
	DictateHotkeyBehavior      string                                         `json:"dictateHotkeyBehavior"`
	AssistHotkeyBehavior       string                                         `json:"assistHotkeyBehavior"`
	VoiceAgentHotkeyBehavior   string                                         `json:"voiceAgentHotkeyBehavior"`
	VoiceAgentCloseBehavior    string                                         `json:"voiceAgentCloseBehavior"`
	VoiceAgentProfileID        string                                         `json:"voiceAgentProfileId"`
	VoiceAgentSequenceID       string                                         `json:"voiceAgentSequenceId,omitempty"`
	VoiceAgentProfiles         []voiceAgentProfileSnapshot                    `json:"voiceAgentProfiles"`
	VoiceAgentRefinementPrompt string                                         `json:"voiceAgentRefinementPrompt"`
	VoiceAgentSessionSummary   bool                                           `json:"voiceAgentSessionSummary"`
	AutoStartOnLaunch          bool                                           `json:"autoStartOnLaunch"`
	VoiceAgentAutoStart        bool                                           `json:"voiceAgentAutoStart,omitempty"`
	ModeEnabled                modeAvailabilitySnapshot                       `json:"modeEnabled"`
	AvailableModes             modeAvailabilitySnapshot                       `json:"availableModes"`
	AgentHotkey                string                                         `json:"agentHotkey"`
	AgentMode                  string                                         `json:"agentMode"`
	ActiveMode                 string                                         `json:"activeMode"`
	HFModel                    string                                         `json:"hfModel"`
	Visualizer                 string                                         `json:"visualizer"`
	Design                     string                                         `json:"design"`
	AssistOverlayMode          string                                         `json:"assistOverlayMode"`
	VoiceAgentOverlayMode      string                                         `json:"voiceAgentOverlayMode"`
	Language                   string                                         `json:"language"`
	VocabularyDictionary       string                                         `json:"vocabularyDictionary"`
	SaveAudio                  bool                                           `json:"saveAudio"`
	AudioRetentionDays         int                                            `json:"audioRetentionDays"`
	AudioDeviceID              string                                         `json:"audioDeviceId"`
	SelectedAudioDeviceID      string                                         `json:"selectedAudioDeviceId"`
	AudioOutputDeviceID        string                                         `json:"audioOutputDeviceId"`
	SelectedOutputDeviceID     string                                         `json:"selectedOutputDeviceId"`
	Profiles                   []models.Profile                               `json:"profiles"`
	ActiveProfiles             map[string]string                              `json:"activeProfiles"`
	ModelSelections            map[string]profiles.ModeModelSelectionSnapshot `json:"modelSelections"`
	ProviderCredentials        map[string]providerCredentialState             `json:"providerCredentials"`
	ProviderIntegrations       map[string]providerIntegrationState            `json:"providerIntegrations"`

	// Wakeword surfaces the always-on activation-word listener configuration
	// alongside the catalog of selectable phrases so the React Settings panel
	// can render the toggle + dropdown without a second round-trip.
	Wakeword wakewordSettingsSnapshot `json:"wakeword"`
}

// wakewordSettingsSnapshot is the JSON-friendly shape of WakewordConfig
// plus runtime info (active status, catalog entries) for the Settings UI.
type wakewordSettingsSnapshot struct {
	// Enabled mirrors cfg.Wakeword.Enabled.
	Enabled bool `json:"enabled"`

	// PhraseID is the currently selected catalog entry. Empty means
	// "custom" mode driven by ModelPath.
	PhraseID string `json:"phraseId"`

	// DefaultMode is the runtime mode to trigger ("dictate" | "assist"
	// | "voice_agent").
	DefaultMode string `json:"defaultMode"`

	// Threshold is the user-overridden detection threshold. 0 means
	// "use the catalog's recommended value".
	Threshold float64 `json:"threshold"`

	// MinConsecutiveFrames and CooldownMs are the rolling debounce knobs.
	MinConsecutiveFrames int `json:"minConsecutiveFrames"`
	CooldownMs           int `json:"cooldownMs"`

	// Active reflects whether the wake runtime is currently listening
	// (Enabled AND model files present AND audio device available).
	Active bool `json:"active"`

	// StatusMessage is a short human-readable status line for the panel
	// — e.g. "Listening for Hey Quby" or "Disabled: model file missing".
	StatusMessage string `json:"statusMessage"`

	// PhraseCatalog lists every curated entry the user can pick from in
	// the dropdown.
	PhraseCatalog []wakewordCatalogEntry `json:"phraseCatalog"`
}

// wakewordCatalogEntry mirrors wakeword.PhraseCatalogEntry for JSON.
type wakewordCatalogEntry struct {
	ID           string `json:"id"`
	DisplayName  string `json:"displayName"`
	KeywordLabel string `json:"keywordLabel"`
	Notes        string `json:"notes"`
}
