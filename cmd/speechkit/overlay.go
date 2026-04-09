package main

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/secrets"
	"github.com/kombifyio/SpeechKit/internal/tray"
	"github.com/wailsapp/wails/v3/pkg/application"
)

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
	pillPanelWidth   = 178
	pillPanelHeight  = 32
	dotAnchorSize    = 24
	radialMenuSize   = 120

	assistBubbleWidth  = 450
	assistBubbleHeight = 120

	overlaySpeakingThreshold = 0.18
	overlayVisualizerGain    = 14.0
	overlayVisualizerFloor   = 0.06
	defaultDoneResetDelay    = 2200 * time.Millisecond
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

// bubbleRegion is retained for legacy tests only.
type bubbleRegion struct {
	X, Y, W, H int
}

type overlaySnapshot struct {
	State                 string            `json:"state"`
	Phase                 string            `json:"phase"`
	Text                  string            `json:"text"`
	Level                 float64           `json:"level"`
	Visible               bool              `json:"visible"`
	Visualizer            string            `json:"visualizer"`
	Design                string            `json:"design"`
	Hotkey                string            `json:"hotkey"`
	DictateHotkey         string            `json:"dictateHotkey"`
	AgentHotkey           string            `json:"agentHotkey"`
	ActiveMode            string            `json:"activeMode"`
	Position              string            `json:"position"`
	Movable               bool              `json:"movable"`
	PositionFreeX         int               `json:"positionFreeX"`
	PositionFreeY         int               `json:"positionFreeY"`
	LastTranscription     string            `json:"lastTranscription"`
	QuickNoteMode         bool              `json:"quickNoteMode"`
	AudioDeviceID         string            `json:"audioDeviceId"`
	SelectedAudioDeviceID string            `json:"selectedAudioDeviceId"`
	ActiveProfiles        map[string]string `json:"activeProfiles"`
}

type settingsSnapshot struct {
	OverlayEnabled        bool                               `json:"overlayEnabled"`
	OverlayPosition       string                             `json:"overlayPosition"`
	OverlayMovable        bool                               `json:"overlayMovable"`
	OverlayFreeX          int                                `json:"overlayFreeX"`
	OverlayFreeY          int                                `json:"overlayFreeY"`
	StoreBackend          string                             `json:"storeBackend"`
	SQLitePath            string                             `json:"sqlitePath"`
	PostgresConfigured    bool                               `json:"postgresConfigured"`
	PostgresDSN           string                             `json:"postgresDSN,omitempty"`
	MaxAudioStorageMB     int                                `json:"maxAudioStorageMB"`
	HFAvailable           bool                               `json:"hfAvailable"`
	HFEnabled             bool                               `json:"hfEnabled"`
	HFHasUserToken        bool                               `json:"hfHasUserToken"`
	HFHasInstallToken     bool                               `json:"hfHasInstallToken"`
	HFTokenSource         string                             `json:"hfTokenSource"`
	Hotkey                string                             `json:"hotkey"`
	DictateHotkey         string                             `json:"dictateHotkey"`
	AgentHotkey           string                             `json:"agentHotkey"`
	ActiveMode            string                             `json:"activeMode"`
	HFModel               string                             `json:"hfModel"`
	Visualizer            string                             `json:"visualizer"`
	Design                string                             `json:"design"`
	VocabularyDictionary  string                             `json:"vocabularyDictionary"`
	SaveAudio             bool                               `json:"saveAudio"`
	AudioRetentionDays    int                                `json:"audioRetentionDays"`
	AudioDeviceID         string                             `json:"audioDeviceId"`
	SelectedAudioDeviceID string                             `json:"selectedAudioDeviceId"`
	ActiveProfiles        map[string]string                  `json:"activeProfiles"`
	ProviderCredentials   map[string]providerCredentialState `json:"providerCredentials"`
}

func newOverlayWindowOptions() application.WebviewWindowOptions {
	return application.WebviewWindowOptions{
		Title:          "SpeechKit",
		Width:          overlayWindowSize,
		Height:         overlayWindowSize,
		Frameless:      true,
		AlwaysOnTop:    true,
		Hidden:         true,
		BackgroundType: application.BackgroundTypeTransparent,
		URL:            "/overlay.html",
		Windows: application.WindowsWindow{
			HiddenOnTaskbar: true,
		},
	}
}

func newPillAnchorWindowOptions() application.WebviewWindowOptions {
	return newOverlayHostWindowOptions("/pill-anchor.html", pillAnchorWidth, pillAnchorHeight)
}

func newPillPanelWindowOptions() application.WebviewWindowOptions {
	return newOverlayHostWindowOptions("/pill-panel.html", pillPanelWidth, pillPanelHeight)
}

func newDotAnchorWindowOptions() application.WebviewWindowOptions {
	return newOverlayHostWindowOptions("/dot-anchor.html", dotAnchorSize, dotAnchorSize)
}

func newRadialMenuWindowOptions() application.WebviewWindowOptions {
	return newOverlayHostWindowOptions("/dot-radial.html", radialMenuSize, radialMenuSize)
}

func newAssistBubbleWindowOptions() application.WebviewWindowOptions {
	return application.WebviewWindowOptions{
		Title:          "",
		Width:          assistBubbleWidth,
		Height:         assistBubbleHeight,
		DisableResize:  true,
		Frameless:      true,
		AlwaysOnTop:    true,
		Hidden:         true,
		BackgroundType: application.BackgroundTypeTransparent,
		URL:            "/assist-bubble.html",
		Windows: application.WindowsWindow{
			HiddenOnTaskbar: true,
		},
	}
}

func assistBubblePosition(bounds screenBounds) (int, int) {
	x := bounds.X + (bounds.Width-assistBubbleWidth)/2
	y := bounds.Y + 60 // Below the top overlay area
	return x, y
}

func newOverlayHostWindowOptions(url string, width, height int) application.WebviewWindowOptions {
	return application.WebviewWindowOptions{
		Title:          "",
		Width:          width,
		Height:         height,
		DisableResize:  true,
		Frameless:      true,
		AlwaysOnTop:    true,
		Hidden:         true,
		BackgroundType: application.BackgroundTypeTransparent,
		URL:            url,
		Windows: application.WindowsWindow{
			HiddenOnTaskbar: true,
		},
	}
}

// Legacy helper retained for compatibility with older tests.
func overlayWindowPosition(bounds screenBounds, position, visualizer string) (int, int) {
	half := overlayWindowSize / 2

	if visualizer == "circle" {
		switch position {
		case "bottom":
			cx := bounds.X + bounds.Width/2
			cy := bounds.Y + bounds.Height - overlayEdgeMargin - dotBubbleH/2
			return cx - half, cy - half
		case "left":
			cx := bounds.X + overlayEdgeMargin + dotBubbleW/2
			cy := bounds.Y + bounds.Height/2
			return cx - half, cy - half
		case "right":
			cx := bounds.X + bounds.Width - overlayEdgeMargin - dotBubbleW/2
			cy := bounds.Y + bounds.Height/2
			return cx - half, cy - half
		default:
			cx := bounds.X + bounds.Width/2
			cy := bounds.Y + overlayEdgeMargin + dotBubbleH/2
			return cx - half, cy - half
		}
	}

	switch position {
	case "bottom":
		x := bounds.X + (bounds.Width-overlayWindowSize)/2
		y := bounds.Y + bounds.Height - overlayWindowSize
		return x, y
	case "left":
		x := bounds.X
		y := bounds.Y + (bounds.Height-overlayWindowSize)/2
		return x, y
	case "right":
		x := bounds.X + bounds.Width - overlayWindowSize
		y := bounds.Y + (bounds.Height-overlayWindowSize)/2
		return x, y
	default:
		x := bounds.X + (bounds.Width-overlayWindowSize)/2
		y := bounds.Y
		return x, y
	}
}

// Legacy helper retained for compatibility with older tests.
func computeBubbleRegion(wx, wy int, bounds screenBounds, position, visualizer string) bubbleRegion {
	if visualizer == "circle" {
		return bubbleRegion{
			X: wx + (overlayWindowSize-dotBubbleW)/2,
			Y: wy + (overlayWindowSize-dotBubbleH)/2,
			W: dotBubbleW, H: dotBubbleH,
		}
	}

	bw, bh := pillBubbleW, pillBubbleH
	switch position {
	case "bottom":
		return bubbleRegion{
			X: wx + (overlayWindowSize-bw)/2,
			Y: wy + overlayWindowSize - bh - overlayEdgeMargin,
			W: bw, H: bh,
		}
	case "left":
		return bubbleRegion{
			X: wx + overlayEdgeMargin,
			Y: wy + (overlayWindowSize-bh)/2,
			W: bw, H: bh,
		}
	case "right":
		return bubbleRegion{
			X: wx + overlayWindowSize - bw - overlayEdgeMargin,
			Y: wy + (overlayWindowSize-bh)/2,
			W: bw, H: bh,
		}
	default:
		return bubbleRegion{
			X: wx + (overlayWindowSize-bw)/2,
			Y: wy + overlayEdgeMargin,
			W: bw, H: bh,
		}
	}
}

func pillAnchorPosition(bounds screenBounds, position string) (int, int) {
	switch position {
	case "bottom":
		return bounds.X + (bounds.Width-pillAnchorWidth)/2, bounds.Y + bounds.Height - pillAnchorHeight - overlayEdgeMargin
	case "left":
		return bounds.X + overlayEdgeMargin, bounds.Y + (bounds.Height-pillAnchorHeight)/2
	case "right":
		return bounds.X + bounds.Width - pillAnchorWidth - overlayEdgeMargin, bounds.Y + (bounds.Height-pillAnchorHeight)/2
	default:
		return bounds.X + (bounds.Width-pillAnchorWidth)/2, bounds.Y + overlayEdgeMargin
	}
}

func pillPanelPosition(bounds screenBounds, position string) (int, int) {
	switch position {
	case "bottom":
		return bounds.X + (bounds.Width-pillPanelWidth)/2, bounds.Y + bounds.Height - pillPanelHeight - overlayEdgeMargin
	case "left":
		return bounds.X + overlayEdgeMargin, bounds.Y + (bounds.Height-pillPanelHeight)/2
	case "right":
		return bounds.X + bounds.Width - pillPanelWidth - overlayEdgeMargin, bounds.Y + (bounds.Height-pillPanelHeight)/2
	default:
		return bounds.X + (bounds.Width-pillPanelWidth)/2, bounds.Y + overlayEdgeMargin
	}
}

func dotAnchorPosition(bounds screenBounds, position string) (int, int) {
	switch position {
	case "bottom":
		return bounds.X + (bounds.Width-dotAnchorSize)/2, bounds.Y + bounds.Height - dotAnchorSize - overlayEdgeMargin
	case "left":
		return bounds.X + overlayEdgeMargin, bounds.Y + (bounds.Height-dotAnchorSize)/2
	case "right":
		return bounds.X + bounds.Width - dotAnchorSize - overlayEdgeMargin, bounds.Y + (bounds.Height-dotAnchorSize)/2
	default:
		return bounds.X + (bounds.Width-dotAnchorSize)/2, bounds.Y + overlayEdgeMargin
	}
}

func radialMenuPosition(bounds screenBounds, position string) (int, int) {
	anchorX, anchorY := dotAnchorPosition(bounds, position)
	return anchorX + dotAnchorSize/2 - radialMenuSize/2, anchorY + dotAnchorSize/2 - radialMenuSize/2
}

func clampInt(value, minValue, maxValue int) int {
	if minValue > maxValue {
		return value
	}
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func defaultOverlayFreeCenter(bounds screenBounds, visualizer, position string) (int, int) {
	if visualizer == "circle" {
		x, y := dotAnchorPosition(bounds, position)
		return x + dotAnchorSize/2, y + dotAnchorSize/2
	}
	x, y := pillAnchorPosition(bounds, position)
	return x + pillAnchorWidth/2, y + pillAnchorHeight/2
}

func resolveOverlayFreeCenter(bounds screenBounds, visualizer, position string, centerX, centerY int) (int, int) {
	if centerX == 0 && centerY == 0 {
		return defaultOverlayFreeCenter(bounds, visualizer, position)
	}
	return centerX, centerY
}

func overlayFreeWindowPosition(bounds screenBounds, centerX, centerY, width, height int) (int, int) {
	halfW := width / 2
	halfH := height / 2
	clampedX := clampInt(centerX, bounds.X+halfW, bounds.X+bounds.Width-halfW)
	clampedY := clampInt(centerY, bounds.Y+halfH, bounds.Y+bounds.Height-halfH)
	return clampedX - halfW, clampedY - halfH
}

func hasDedicatedOverlayWindows(host desktopHostState) bool {
	return host.pillAnchor != nil || host.pillPanel != nil || host.dotAnchor != nil || host.radialMenu != nil
}

func activeOverlayAnchor(host desktopHostState, visualizer string) overlayWindow {
	if visualizer == "circle" {
		if host.dotAnchor != nil {
			return host.dotAnchor
		}
		return host.overlay
	}
	if host.pillAnchor != nil {
		return host.pillAnchor
	}
	return host.overlay
}

func hideWindow(window overlayWindow) {
	if window == nil {
		return
	}
	window.Hide()
}

func showWindow(window overlayWindow) {
	if window == nil || window.IsVisible() {
		return
	}
	window.Show()
}

func (s *appState) positionOverlay() {
	s.mu.Lock()
	host := s.desktopHostStateLocked()
	runtime := s.runtimeStateLocked()
	s.mu.Unlock()

	locator := host.screenLocator
	if locator == nil {
		return
	}
	bounds, ok := locator.OverlayScreenBounds()
	if !ok {
		return
	}

	if hasDedicatedOverlayWindows(host) {
		if runtime.overlayMovable {
			centerX, centerY := resolveOverlayFreeCenter(bounds, runtime.overlayVisualizer, runtime.overlayPosition, runtime.overlayFreeX, runtime.overlayFreeY)
			if host.pillAnchor != nil {
				x, y := overlayFreeWindowPosition(bounds, centerX, centerY, pillAnchorWidth, pillAnchorHeight)
				host.pillAnchor.SetSize(pillAnchorWidth, pillAnchorHeight)
				host.pillAnchor.SetPosition(x, y)
			}
			if host.pillPanel != nil {
				x, y := overlayFreeWindowPosition(bounds, centerX, centerY, pillPanelWidth, pillPanelHeight)
				host.pillPanel.SetSize(pillPanelWidth, pillPanelHeight)
				host.pillPanel.SetPosition(x, y)
			}
			if host.dotAnchor != nil {
				x, y := overlayFreeWindowPosition(bounds, centerX, centerY, dotAnchorSize, dotAnchorSize)
				host.dotAnchor.SetSize(dotAnchorSize, dotAnchorSize)
				host.dotAnchor.SetPosition(x, y)
			}
			if host.radialMenu != nil {
				x, y := overlayFreeWindowPosition(bounds, centerX, centerY, radialMenuSize, radialMenuSize)
				host.radialMenu.SetSize(radialMenuSize, radialMenuSize)
				host.radialMenu.SetPosition(x, y)
			}
			return
		}

		if host.pillAnchor != nil {
			x, y := pillAnchorPosition(bounds, runtime.overlayPosition)
			host.pillAnchor.SetSize(pillAnchorWidth, pillAnchorHeight)
			host.pillAnchor.SetPosition(x, y)
		}
		if host.pillPanel != nil {
			x, y := pillPanelPosition(bounds, runtime.overlayPosition)
			host.pillPanel.SetSize(pillPanelWidth, pillPanelHeight)
			host.pillPanel.SetPosition(x, y)
		}
		if host.dotAnchor != nil {
			x, y := dotAnchorPosition(bounds, runtime.overlayPosition)
			host.dotAnchor.SetSize(dotAnchorSize, dotAnchorSize)
			host.dotAnchor.SetPosition(x, y)
		}
		if host.radialMenu != nil {
			x, y := radialMenuPosition(bounds, runtime.overlayPosition)
			host.radialMenu.SetSize(radialMenuSize, radialMenuSize)
			host.radialMenu.SetPosition(x, y)
		}
		return
	}

	overlay := host.overlay
	if overlay == nil {
		return
	}
	if runtime.overlayMovable {
		centerX, centerY := resolveOverlayFreeCenter(bounds, runtime.overlayVisualizer, runtime.overlayPosition, runtime.overlayFreeX, runtime.overlayFreeY)
		wx, wy := overlayFreeWindowPosition(bounds, centerX, centerY, overlayWindowSize, overlayWindowSize)
		overlay.SetPosition(wx, wy)
		return
	}
	wx, wy := overlayWindowPosition(bounds, runtime.overlayPosition, runtime.overlayVisualizer)
	overlay.SetPosition(wx, wy)
}

func (s *appState) showActiveOverlayWindow() {
	s.mu.Lock()
	host := s.desktopHostStateLocked()
	runtime := s.runtimeStateLocked()
	s.mu.Unlock()

	if !runtime.overlayEnabled {
		s.hideAllOverlayWindows()
		return
	}

	s.positionOverlay()

	if hasDedicatedOverlayWindows(host) {
		hideWindow(host.pillPanel)
		hideWindow(host.radialMenu)

		active := activeOverlayAnchor(host, runtime.overlayVisualizer)
		showWindow(active)

		if runtime.overlayVisualizer == "circle" {
			hideWindow(host.pillAnchor)
			hideWindow(host.overlay)
		} else {
			hideWindow(host.dotAnchor)
		}
		if runtime.overlayVisualizer != "circle" {
			hideWindow(host.dotAnchor)
		}
		if runtime.overlayVisualizer == "circle" {
			hideWindow(host.pillAnchor)
			hideWindow(host.overlay)
		}
		return
	}

	showWindow(host.overlay)
}

func (s *appState) hideAllOverlayWindows() {
	s.mu.Lock()
	host := s.desktopHostStateLocked()
	s.mu.Unlock()

	hideWindow(host.overlay)
	hideWindow(host.pillAnchor)
	hideWindow(host.pillPanel)
	hideWindow(host.dotAnchor)
	hideWindow(host.radialMenu)
}

func (s *appState) showPillPanel() {
	s.positionOverlay()

	s.mu.Lock()
	host := s.desktopHostStateLocked()
	runtime := s.runtimeStateLocked()
	s.mu.Unlock()

	if !runtime.overlayEnabled || runtime.overlayVisualizer == "circle" {
		return
	}

	hideWindow(host.overlay)
	hideWindow(host.pillAnchor)
	hideWindow(host.dotAnchor)
	hideWindow(host.radialMenu)
	showWindow(host.pillPanel)
}

func (s *appState) hidePillPanel() {
	s.mu.Lock()
	host := s.desktopHostStateLocked()
	runtime := s.runtimeStateLocked()
	s.mu.Unlock()

	hideWindow(host.pillPanel)
	if runtime.overlayEnabled && runtime.overlayVisualizer != "circle" {
		showWindow(activeOverlayAnchor(host, runtime.overlayVisualizer))
	}
}

func (s *appState) showRadialMenu() {
	s.positionOverlay()

	s.mu.Lock()
	host := s.desktopHostStateLocked()
	runtime := s.runtimeStateLocked()
	s.mu.Unlock()

	if !runtime.overlayEnabled || runtime.overlayVisualizer != "circle" {
		return
	}

	hideWindow(host.overlay)
	hideWindow(host.pillAnchor)
	hideWindow(host.pillPanel)
	hideWindow(host.dotAnchor)
	showWindow(host.radialMenu)
}

func (s *appState) hideRadialMenu() {
	s.mu.Lock()
	host := s.desktopHostStateLocked()
	runtime := s.runtimeStateLocked()
	s.mu.Unlock()

	hideWindow(host.radialMenu)
	if runtime.overlayEnabled && runtime.overlayVisualizer == "circle" {
		showWindow(activeOverlayAnchor(host, runtime.overlayVisualizer))
	}
}

func (s *appState) showAssistBubble(text string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	bubble := s.assistBubble
	locator := s.screenLocator
	s.mu.Unlock()

	if bubble == nil {
		return
	}

	// Position the bubble near the top center of the active screen.
	if locator != nil {
		if bounds, ok := locator.OverlayScreenBounds(); ok {
			x, y := assistBubblePosition(bounds)
			bubble.SetPosition(x, y)
		}
	}

	// Show the window and inject text via JS.
	showWindow(bubble)
	bubble.SetIgnoreMouseEvents(false)
	escapedText := escapeJS(text)
	bubble.ExecJS(fmt.Sprintf(`if(window.__assistBubble){window.__assistBubble.show("%s")}`, escapedText))
}

func (s *appState) doneResetDelayValue() time.Duration {
	if s.doneResetDelay > 0 {
		return s.doneResetDelay
	}
	return defaultDoneResetDelay
}

func (s *appState) setLevel(level float64) {
	var logMessage string

	if level < 0 {
		level = 0
	}
	if level > 1 {
		level = 1
	}

	s.mu.Lock()
	if s.currentState != "recording" {
		level = 0
	}
	if level < s.overlayLevel {
		level = s.overlayLevel * 0.82
	}
	s.overlayLevel = level
	visualLevel := normalizeOverlayLevel(level)
	phase := overlayPhase(s.currentState, visualLevel)
	if phase != s.overlayPhase {
		logMessage = fmt.Sprintf(
			"Overlay audio: phase=%s raw=%.3f visual=%.3f",
			phase, level, visualLevel,
		)
	}
	s.overlayPhase = phase
	s.syncSpeechKitSnapshotLocked()
	s.mu.Unlock()

	if logMessage != "" {
		s.addLog(logMessage, "info")
	}
}

func (s *appState) setOverlayEnabled(enabled bool) {
	s.mu.Lock()
	s.overlayEnabled = enabled
	s.syncSpeechKitSnapshotLocked()
	s.mu.Unlock()

	if enabled {
		s.setState("idle", "")
		return
	}

	s.hideAllOverlayWindows()
}

func (s *appState) syncOverlayToActiveScreen() {
	s.mu.Lock()
	enabled := s.overlayEnabled
	s.mu.Unlock()

	if !enabled {
		return
	}

	s.positionOverlay()
}

func (s *appState) overlaySnapshot() overlaySnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	dictateHotkey := s.dictateHotkey
	if dictateHotkey == "" {
		dictateHotkey = s.hotkey
	}
	agentHotkey := s.agentHotkey
	if agentHotkey == "" {
		agentHotkey = dictateHotkey
	}
	activeMode := s.activeMode
	if activeMode == "" {
		activeMode = "dictate"
	}
	audioDeviceID := s.audioDeviceID
	activeProfiles := cloneStringMap(s.activeProfiles)
	level := s.overlayLevel
	if s.currentState != "recording" {
		level = 0
	}
	level = normalizeOverlayLevel(level)
	phase := s.overlayPhase
	if phase == "" {
		phase = overlayPhase(s.currentState, level)
	}

	return overlaySnapshot{
		State:                 s.currentState,
		Phase:                 phase,
		Text:                  s.overlayText,
		Level:                 level,
		Visible:               s.overlayEnabled,
		Visualizer:            s.overlayVisualizer,
		Design:                s.overlayDesign,
		Hotkey:                s.hotkey,
		DictateHotkey:         dictateHotkey,
		AgentHotkey:           agentHotkey,
		ActiveMode:            activeMode,
		Position:              s.overlayPosition,
		Movable:               s.overlayMovable,
		PositionFreeX:         s.overlayFreeX,
		PositionFreeY:         s.overlayFreeY,
		LastTranscription:     s.lastTranscriptionText,
		QuickNoteMode:         s.quickNoteMode,
		AudioDeviceID:         audioDeviceID,
		SelectedAudioDeviceID: audioDeviceID,
		ActiveProfiles:        activeProfiles,
	}
}

func normalizeOverlayLevel(level float64) float64 {
	if level <= 0 {
		return 0
	}

	boosted := math.Pow(math.Min(1, level*overlayVisualizerGain), 0.72)
	if boosted < overlayVisualizerFloor {
		return overlayVisualizerFloor
	}
	return math.Min(1, boosted)
}

func overlayPhase(state string, level float64) string {
	switch state {
	case "recording":
		if level >= overlaySpeakingThreshold {
			return "speaking"
		}
		return "listening"
	case "processing":
		return "thinking"
	case "done":
		return "done"
	default:
		return "idle"
	}
}

func (s *appState) settingsSnapshot(cfg *config.Config) settingsSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	dictateHotkey := s.dictateHotkey
	if dictateHotkey == "" {
		if cfg.General.DictateHotkey != "" {
			dictateHotkey = cfg.General.DictateHotkey
		} else {
			dictateHotkey = s.hotkey
		}
	}
	agentHotkey := s.agentHotkey
	if agentHotkey == "" {
		if cfg.General.AgentHotkey != "" {
			agentHotkey = cfg.General.AgentHotkey
		} else {
			agentHotkey = dictateHotkey
		}
	}
	activeMode := s.activeMode
	if activeMode == "" {
		if cfg.General.ActiveMode != "" {
			activeMode = cfg.General.ActiveMode
		} else {
			activeMode = "dictate"
		}
	}
	audioDeviceID := s.audioDeviceID
	if audioDeviceID == "" {
		audioDeviceID = cfg.Audio.DeviceID
	}
	storeBackend := cfg.Store.Backend
	if storeBackend == "" {
		storeBackend = "sqlite"
	}
	hfAvailable := config.ManagedHuggingFaceAvailableInBuild()
	tokenStatus := secrets.TokenStatus{ActiveSource: secrets.TokenSourceNone}
	if hfAvailable {
		var err error
		tokenStatus, err = config.HuggingFaceTokenStatus(cfg)
		if err != nil {
			tokenStatus.ActiveSource = "none"
		}
	}
	return settingsSnapshot{
		OverlayEnabled:        s.overlayEnabled,
		OverlayPosition:       s.overlayPosition,
		OverlayMovable:        s.overlayMovable,
		OverlayFreeX:          s.overlayFreeX,
		OverlayFreeY:          s.overlayFreeY,
		StoreBackend:          storeBackend,
		SQLitePath:            cfg.Store.SQLitePath,
		PostgresConfigured:    strings.TrimSpace(cfg.Store.PostgresDSN) != "",
		PostgresDSN:           cfg.Store.PostgresDSN,
		MaxAudioStorageMB:     cfg.Store.MaxAudioStorageMB,
		HFAvailable:           hfAvailable,
		HFEnabled:             hfAvailable && cfg.HuggingFace.Enabled,
		HFHasUserToken:        tokenStatus.HasUserToken,
		HFHasInstallToken:     tokenStatus.HasInstallToken,
		HFTokenSource:         string(tokenStatus.ActiveSource),
		Hotkey:                dictateHotkey,
		DictateHotkey:         dictateHotkey,
		AgentHotkey:           agentHotkey,
		ActiveMode:            activeMode,
		HFModel:               cfg.HuggingFace.Model,
		Visualizer:            s.overlayVisualizer,
		Design:                cfg.UI.Design,
		VocabularyDictionary:  cfg.Vocabulary.Dictionary,
		SaveAudio:             cfg.Store.SaveAudio,
		AudioRetentionDays:    cfg.Store.AudioRetentionDays,
		AudioDeviceID:         audioDeviceID,
		SelectedAudioDeviceID: audioDeviceID,
		ActiveProfiles:        cloneStringMap(s.activeProfiles),
		ProviderCredentials:   providerCredentialStates(cfg),
	}
}

func (s *appState) overlayFreeCenter() (int, int) {
	if s == nil {
		return 0, 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.overlayFreeX, s.overlayFreeY
}

func (s *appState) updateOverlayFreeCenter(centerX, centerY int) bool {
	if s == nil {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.overlayMovable {
		return false
	}

	s.overlayFreeX = centerX
	s.overlayFreeY = centerY
	s.syncSpeechKitSnapshotLocked()
	return true
}

func (s *appState) updateOverlayFreeCenterFromPanel(x, y int) bool {
	return s.updateOverlayFreeCenter(x+pillPanelWidth/2, y+pillPanelHeight/2)
}
