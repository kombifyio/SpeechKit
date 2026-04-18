package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"fmt"
	"slices"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/models"
	"github.com/kombifyio/SpeechKit/internal/router"
	"github.com/kombifyio/SpeechKit/internal/secrets"
	"github.com/kombifyio/SpeechKit/internal/store"
	"github.com/kombifyio/SpeechKit/internal/stt"
	"github.com/kombifyio/SpeechKit/internal/tray"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/wailsapp/wails/v3/pkg/application"
)

type fakeOverlayWindow struct {
	showCalls     int
	hideCalls     int
	minimiseCalls int
	visible       bool
	scripts       []string
	ignoreMouse   []bool
	positions     [][2]int
	sizes         [][2]int
}

func (f *fakeOverlayWindow) Show() application.Window {
	f.showCalls++
	f.visible = true
	return nil
}

func (f *fakeOverlayWindow) Hide() application.Window {
	f.hideCalls++
	f.visible = false
	return nil
}

func (f *fakeOverlayWindow) Minimise() application.Window {
	f.minimiseCalls++
	f.visible = false
	return nil
}

func (f *fakeOverlayWindow) IsVisible() bool {
	return f.visible
}

func (f *fakeOverlayWindow) ExecJS(script string) {
	f.scripts = append(f.scripts, script)
}

func (f *fakeOverlayWindow) SetIgnoreMouseEvents(ignore bool) application.Window {
	f.ignoreMouse = append(f.ignoreMouse, ignore)
	return nil
}

func (f *fakeOverlayWindow) SetPosition(x, y int) {
	f.positions = append(f.positions, [2]int{x, y})
}

func (f *fakeOverlayWindow) SetSize(w, h int) application.Window {
	f.sizes = append(f.sizes, [2]int{w, h})
	return nil
}

type fakeSettingsWindow struct {
	scripts         []string
	visible         bool
	showCalls       int
	restoreCalls    int
	unMinimiseCalls int
	focusCalls      int
}

func (f *fakeSettingsWindow) ExecJS(script string) {
	f.scripts = append(f.scripts, script)
}

func (f *fakeSettingsWindow) Show() application.Window {
	f.showCalls++
	f.visible = true
	return nil
}

func (f *fakeSettingsWindow) IsVisible() bool { return f.visible }

func (f *fakeSettingsWindow) Restore() {
	f.restoreCalls++
}

func (f *fakeSettingsWindow) UnMinimise() {
	f.unMinimiseCalls++
}

func (f *fakeSettingsWindow) Focus() {
	f.focusCalls++
}

type fakeTray struct {
	states []tray.State
}

func (f *fakeTray) SetState(state tray.State) {
	f.states = append(f.states, state)
}

type fakeScreenLocator struct {
	bounds screenBounds
	ok     bool
	calls  int
}

func (f *fakeScreenLocator) OverlayScreenBounds() (screenBounds, bool) {
	f.calls++
	return f.bounds, f.ok
}

func unsetEnvForTest(t *testing.T, name string) {
	t.Helper()

	value, ok := os.LookupEnv(name)
	if err := os.Unsetenv(name); err != nil {
		t.Fatalf("unset %s: %v", name, err)
	}
	t.Cleanup(func() {
		var err error
		if ok {
			err = os.Setenv(name, value)
		} else {
			err = os.Unsetenv(name)
		}
		if err != nil {
			t.Fatalf("restore %s: %v", name, err)
		}
	})
}

func isolateManagedHFEnvForTest(t *testing.T) {
	t.Helper()

	for _, name := range []string{
		"HF_TOKEN",
		"DOPPLER_PROJECT",
		"DOPPLER_CONFIG",
		"SPEECHKIT_ENABLE_MANAGED_HF",
	} {
		unsetEnvForTest(t, name)
	}
}

func TestOverlayWindowOptionsKeepOverlayAboveApps(t *testing.T) {
	opts := newOverlayWindowOptions()

	if !opts.AlwaysOnTop {
		t.Fatal("overlay must be configured as always-on-top")
	}
	if !opts.Hidden {
		t.Fatal("overlay should start hidden")
	}
	if !opts.Frameless {
		t.Fatal("overlay should be frameless")
	}
	if opts.BackgroundType != application.BackgroundTypeTransparent {
		t.Fatalf("background type = %v, want transparent", opts.BackgroundType)
	}
	if opts.URL != "/overlay.html" {
		t.Fatalf("overlay URL = %q", opts.URL)
	}
	if !opts.Windows.HiddenOnTaskbar {
		t.Fatal("overlay should stay off the taskbar")
	}
	if opts.Width != overlayWindowSize {
		t.Fatalf("overlay width = %d, want %d", opts.Width, overlayWindowSize)
	}
	if opts.Height != overlayWindowSize {
		t.Fatalf("overlay height = %d, want %d", opts.Height, overlayWindowSize)
	}
}

func TestOverlayWindowOptionsStartsCompact(t *testing.T) {
	opts := newOverlayWindowOptions()

	// Compact window: just the bubble, dynamically expanded on hover
	if opts.Width != overlayWindowSize {
		t.Fatalf("overlay width = %d, want %d (compact)", opts.Width, overlayWindowSize)
	}
	if opts.Height != overlayWindowSize {
		t.Fatalf("overlay height = %d, want %d (compact)", opts.Height, overlayWindowSize)
	}
}

func TestPillPanelWindowOptionsStayCompactAndLocked(t *testing.T) {
	opts := newPillPanelWindowOptions()

	if opts.Width >= 260 {
		t.Fatalf("pill panel width = %d, want compact width under 260", opts.Width)
	}
	if opts.Height > 36 {
		t.Fatalf("pill panel height = %d, want compact height up to 36", opts.Height)
	}
	if !opts.Frameless {
		t.Fatal("pill panel should be frameless")
	}
	if !opts.DisableResize {
		t.Fatal("pill panel should disable resizing")
	}
	if opts.Title != "" {
		t.Fatalf("pill panel title = %q, want empty", opts.Title)
	}
}

func TestDotAnchorWindowOptionsStayCompact(t *testing.T) {
	opts := newDotAnchorWindowOptions()

	if opts.Width > 24 {
		t.Fatalf("dot anchor width = %d, want width up to 24", opts.Width)
	}
	if opts.Height > 24 {
		t.Fatalf("dot anchor height = %d, want height up to 24", opts.Height)
	}
	if !opts.DisableResize {
		t.Fatal("dot anchor should disable resizing")
	}
}

func TestRadialMenuWindowOptionsStayCompact(t *testing.T) {
	opts := newRadialMenuWindowOptions()

	if opts.Width > 120 {
		t.Fatalf("radial menu width = %d, want width up to 120", opts.Width)
	}
	if opts.Height > 120 {
		t.Fatalf("radial menu height = %d, want height up to 120", opts.Height)
	}
	if !opts.DisableResize {
		t.Fatal("radial menu should disable resizing")
	}
}

func TestRadialMenuPositionCentersAroundDotAnchor(t *testing.T) {
	bounds := screenBounds{X: 0, Y: 0, Width: 1920, Height: 1080}
	dotX, dotY := dotAnchorPosition(bounds, "right")
	radialX, radialY := radialMenuPosition(bounds, "right")

	wantX := dotX + dotAnchorSize/2 - radialMenuSize/2
	wantY := dotY + dotAnchorSize/2 - radialMenuSize/2
	if radialX != wantX || radialY != wantY {
		t.Fatalf("radial menu = (%d,%d), want centered around dot at (%d,%d)", radialX, radialY, wantX, wantY)
	}
}

func TestDashboardWindowOptionsUseCustomChrome(t *testing.T) {
	opts := newDashboardWindowOptions()

	if !opts.Frameless {
		t.Fatal("dashboard should be frameless for custom chrome")
	}
	if opts.BackgroundType != application.BackgroundTypeTranslucent {
		t.Fatalf("dashboard background type = %v, want translucent", opts.BackgroundType)
	}
	if opts.Windows.BackdropType != application.Mica {
		t.Fatalf("dashboard backdrop = %v, want Mica", opts.Windows.BackdropType)
	}
	if opts.Windows.DisableIcon != true {
		t.Fatal("dashboard titlebar icon should be disabled for the custom shell")
	}
	if opts.URL != "/dashboard.html" {
		t.Fatalf("dashboard URL = %q, want /dashboard.html", opts.URL)
	}
}

func TestOverlayWindowPositionTopCenter(t *testing.T) {
	x, y := overlayWindowPosition(screenBounds{X: 1920, Y: 0, Width: 2560, Height: 1440}, "top", "pill")

	wantX := 1920 + (2560-overlayWindowSize)/2
	if x != wantX {
		t.Fatalf("overlay x = %d, want %d", x, wantX)
	}
	if y != 0 {
		t.Fatalf("overlay y = %d, want 0", y)
	}
}

func TestPositionOverlayPlacesWindowAtTopCenter(t *testing.T) {
	overlay := &fakeOverlayWindow{}
	locator := &fakeScreenLocator{bounds: screenBounds{X: 1920, Y: 0, Width: 2560, Height: 1440}, ok: true}
	state := &appState{
		overlay:           overlay,
		screenLocator:     locator,
		overlayVisualizer: "pill",
		overlayPosition:   "top",
	}

	state.positionOverlay()

	if len(overlay.positions) != 1 {
		t.Fatalf("expected one position update, got %d", len(overlay.positions))
	}
	wantX := 1920 + (2560-overlayWindowSize)/2
	wantY := 0 // top of screen
	if overlay.positions[0] != [2]int{wantX, wantY} {
		t.Fatalf("overlay position = %v, want [%d %d]", overlay.positions[0], wantX, wantY)
	}
}

func TestPositionOverlayPlacesPillHostsAtSavedFreePoint(t *testing.T) {
	pillAnchor := &fakeOverlayWindow{}
	pillPanel := &fakeOverlayWindow{}
	locator := &fakeScreenLocator{bounds: screenBounds{X: 0, Y: 0, Width: 1920, Height: 1080}, ok: true}
	state := &appState{
		pillAnchor:        pillAnchor,
		pillPanel:         pillPanel,
		screenLocator:     locator,
		overlayVisualizer: "pill",
		overlayPosition:   "top",
		overlayMovable:    true,
		overlayFreeX:      910,
		overlayFreeY:      420,
	}

	state.positionOverlay()

	if len(pillAnchor.positions) != 1 {
		t.Fatalf("pill anchor positions = %v", pillAnchor.positions)
	}
	if len(pillPanel.positions) != 1 {
		t.Fatalf("pill panel positions = %v", pillPanel.positions)
	}

	wantAnchor := [2]int{910 - pillAnchorWidth/2, 420 - pillAnchorHeight/2}
	if pillAnchor.positions[0] != wantAnchor {
		t.Fatalf("pill anchor position = %v, want %v", pillAnchor.positions[0], wantAnchor)
	}

	wantPanel := [2]int{910 - pillPanelWidth/2, 420 - pillPanelHeight/2}
	if pillPanel.positions[0] != wantPanel {
		t.Fatalf("pill panel position = %v, want %v", pillPanel.positions[0], wantPanel)
	}
}

func TestPositionOverlayMapsSavedFreePointToMatchingRelativeSpotOnDifferentMonitor(t *testing.T) {
	pillAnchor := &fakeOverlayWindow{}
	pillPanel := &fakeOverlayWindow{}
	sourceBounds := screenBounds{X: 0, Y: 0, Width: 1920, Height: 1080}
	targetBounds := screenBounds{X: 1920, Y: 0, Width: 2560, Height: 1440}
	centerX := sourceBounds.X + pillPanelWidth/2 + (sourceBounds.Width-pillPanelWidth)/4
	centerY := sourceBounds.Y + pillPanelHeight/2 + (sourceBounds.Height-pillPanelHeight)/4
	state := &appState{
		pillAnchor:        pillAnchor,
		pillPanel:         pillPanel,
		screenLocator:     &fakeScreenLocator{bounds: targetBounds, ok: true},
		overlayVisualizer: "pill",
		overlayPosition:   "top",
		overlayMovable:    true,
		overlayFreeX:      centerX,
		overlayFreeY:      centerY,
		overlayMonitorCenters: map[string]config.OverlayFreePosition{
			overlayMonitorKey(sourceBounds): {X: centerX, Y: centerY},
		},
	}

	state.positionOverlay()

	wantCenterX := targetBounds.X + pillPanelWidth/2 + (targetBounds.Width-pillPanelWidth)/4
	wantCenterY := targetBounds.Y + pillPanelHeight/2 + (targetBounds.Height-pillPanelHeight)/4
	wantAnchor := [2]int{wantCenterX - pillAnchorWidth/2, wantCenterY - pillAnchorHeight/2}
	wantPanel := [2]int{wantCenterX - pillPanelWidth/2, wantCenterY - pillPanelHeight/2}

	if len(pillAnchor.positions) != 1 || pillAnchor.positions[0] != wantAnchor {
		t.Fatalf("pill anchor positions = %v, want [%v]", pillAnchor.positions, wantAnchor)
	}
	if len(pillPanel.positions) != 1 || pillPanel.positions[0] != wantPanel {
		t.Fatalf("pill panel positions = %v, want [%v]", pillPanel.positions, wantPanel)
	}
	if state.overlayFreeX != wantCenterX || state.overlayFreeY != wantCenterY {
		t.Fatalf("resolved free overlay center = (%d,%d), want (%d,%d)", state.overlayFreeX, state.overlayFreeY, wantCenterX, wantCenterY)
	}
	if got := state.overlayMonitorCenters[overlayMonitorKey(targetBounds)]; got != (config.OverlayFreePosition{X: wantCenterX, Y: wantCenterY}) {
		t.Fatalf("target monitor saved center = %+v", got)
	}
}

func TestSetStateRecordingShowsOverlayAndUpdatesClients(t *testing.T) {
	overlay := &fakeOverlayWindow{}
	settings := &fakeSettingsWindow{visible: true}
	appTray := &fakeTray{}
	state := &appState{
		overlay:        overlay,
		settings:       settings,
		appTray:        appTray,
		overlayEnabled: true,
		screenLocator:  &fakeScreenLocator{bounds: screenBounds{X: 1920, Y: 0, Width: 1600, Height: 900}, ok: true},
	}

	state.setState("recording", "Recording...")

	if overlay.showCalls != 1 {
		t.Fatalf("overlay show calls = %d, want 1", overlay.showCalls)
	}
	if len(overlay.positions) != 1 || overlay.positions[0] != [2]int{1920 + (1600-overlayWindowSize)/2, 0} {
		t.Fatalf("overlay positions = %v", overlay.positions)
	}
	if len(overlay.scripts) != 0 {
		t.Fatalf("overlay scripts = %v, want none", overlay.scripts)
	}
	// Dashboard is now polled via API, no ExecJS push
	if len(appTray.states) != 1 || appTray.states[0] != tray.StateRecording {
		t.Fatalf("tray states = %v", appTray.states)
	}
	snapshot := state.overlaySnapshot()
	if snapshot.State != "recording" || snapshot.Text != "Recording..." {
		t.Fatalf("overlay snapshot = %+v", snapshot)
	}
}

func TestSetStateIdleKeepsOverlayVisibleInPassiveMode(t *testing.T) {
	overlay := &fakeOverlayWindow{}
	state := &appState{
		overlay:        overlay,
		overlayEnabled: true,
		screenLocator:  &fakeScreenLocator{bounds: screenBounds{X: 1920, Y: 0, Width: 2560, Height: 1440}, ok: true},
	}

	state.setState("idle", "")

	if overlay.showCalls != 1 {
		t.Fatalf("overlay show calls = %d, want 1", overlay.showCalls)
	}
	if overlay.hideCalls != 0 {
		t.Fatalf("overlay hide calls = %d, want 0", overlay.hideCalls)
	}
	if len(overlay.scripts) != 0 {
		t.Fatalf("overlay scripts = %v, want none", overlay.scripts)
	}
	if len(overlay.positions) != 1 || overlay.positions[0] != [2]int{1920 + (2560-overlayWindowSize)/2, 0} {
		t.Fatalf("overlay positions = %v", overlay.positions)
	}
	snapshot := state.overlaySnapshot()
	if snapshot.State != "idle" || snapshot.Text != "" || !snapshot.Visible {
		t.Fatalf("overlay snapshot = %+v", snapshot)
	}
}

func TestSyncOverlayToActiveScreenRunsWhileOverlayEnabled(t *testing.T) {
	overlay := &fakeOverlayWindow{}
	locator := &fakeScreenLocator{bounds: screenBounds{X: 1920, Y: 0, Width: 2560, Height: 1440}, ok: true}
	state := &appState{
		overlay:        overlay,
		overlayEnabled: true,
		currentState:   "idle",
		screenLocator:  locator,
	}

	state.syncOverlayToActiveScreen()

	if locator.calls != 1 {
		t.Fatalf("screen locator calls = %d, want 1", locator.calls)
	}
	if len(overlay.positions) != 1 {
		t.Fatalf("overlay positions = %v", overlay.positions)
	}

	locator.calls = 0
	overlay.positions = nil
	state.currentState = "recording"
	state.syncOverlayToActiveScreen()
	if locator.calls != 1 {
		t.Fatalf("screen locator should continue while recording, got %d calls", locator.calls)
	}
}

func TestSetStateDoesNotPushIntoHiddenSettingsWindow(t *testing.T) {
	overlay := &fakeOverlayWindow{}
	settings := &fakeSettingsWindow{visible: false}
	state := &appState{
		overlay:        overlay,
		settings:       settings,
		overlayEnabled: true,
		screenLocator:  &fakeScreenLocator{bounds: screenBounds{Width: 1600, Height: 900}, ok: true},
	}

	state.setState("recording", "Recording...")
	state.addLog("test", "info")

	if len(settings.scripts) != 0 {
		t.Fatalf("settings scripts = %v, want none while hidden", settings.scripts)
	}
}

func TestSetStateDoesNotReshowVisibleOverlay(t *testing.T) {
	overlay := &fakeOverlayWindow{visible: true}
	state := &appState{
		overlay:        overlay,
		overlayEnabled: true,
		screenLocator:  &fakeScreenLocator{bounds: screenBounds{Width: 1600, Height: 900}, ok: true},
	}

	state.setState("idle", "")

	if overlay.showCalls != 0 {
		t.Fatalf("overlay show calls = %d, want 0 when already visible", overlay.showCalls)
	}
	if len(overlay.positions) != 1 {
		t.Fatalf("overlay positions = %v", overlay.positions)
	}
}

func TestShowSettingsWindowRestoresAndFocusesHiddenWindow(t *testing.T) {
	settings := &fakeSettingsWindow{visible: false}

	showSettingsWindow(settings)

	if settings.restoreCalls != 1 {
		t.Fatalf("restore calls = %d, want 1", settings.restoreCalls)
	}
	if settings.unMinimiseCalls != 1 {
		t.Fatalf("unminimise calls = %d, want 1", settings.unMinimiseCalls)
	}
	if settings.showCalls != 1 {
		t.Fatalf("show calls = %d, want 1", settings.showCalls)
	}
	if settings.focusCalls != 1 {
		t.Fatalf("focus calls = %d, want 1", settings.focusCalls)
	}
}

func TestShowSettingsWindowDoesNotReshowVisibleWindow(t *testing.T) {
	settings := &fakeSettingsWindow{visible: true}

	showSettingsWindow(settings)

	if settings.showCalls != 0 {
		t.Fatalf("show calls = %d, want 0 for visible window", settings.showCalls)
	}
	if settings.restoreCalls != 1 || settings.unMinimiseCalls != 1 || settings.focusCalls != 1 {
		t.Fatalf("restore=%d unminimise=%d focus=%d", settings.restoreCalls, settings.unMinimiseCalls, settings.focusCalls)
	}
}

func TestAssetHandlerServesBuiltOverlayShell(t *testing.T) {
	cfg := defaultTestConfig()
	handler := assetHandler(cfg, filepath.Join(t.TempDir(), "config.toml"), &appState{}, &router.Router{}, nil, &config.InstallState{Mode: config.InstallModeLocal})
	req := httptest.NewRequest(http.MethodGet, "/overlay.html", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), `<div id="root"></div>`) {
		t.Fatalf("overlay shell = %q", rec.Body.String())
	}
}

func TestAssetHandlerServesBuiltDashboardShell(t *testing.T) {
	cfg := defaultTestConfig()
	handler := assetHandler(cfg, filepath.Join(t.TempDir(), "config.toml"), &appState{}, &router.Router{}, nil, &config.InstallState{Mode: config.InstallModeLocal})
	req := httptest.NewRequest(http.MethodGet, "/dashboard.html", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), `<div id="root"></div>`) {
		t.Fatalf("dashboard shell = %q", rec.Body.String())
	}
}

func TestAssetHandlerServesBuiltSettingsShell(t *testing.T) {
	cfg := defaultTestConfig()
	handler := assetHandler(cfg, filepath.Join(t.TempDir(), "config.toml"), &appState{}, &router.Router{}, nil, &config.InstallState{Mode: config.InstallModeLocal})
	req := httptest.NewRequest(http.MethodGet, "/settings.html", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), `<div id="root"></div>`) {
		t.Fatalf("settings shell = %q", rec.Body.String())
	}
}

func TestAssetHandlerServesVoiceAgentPrompterShell(t *testing.T) {
	cfg := defaultTestConfig()
	handler := assetHandler(cfg, filepath.Join(t.TempDir(), "config.toml"), &appState{}, &router.Router{}, nil, &config.InstallState{Mode: config.InstallModeLocal})
	req := httptest.NewRequest(http.MethodGet, "/voiceagent-prompter.html", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), `<div id="root"></div>`) {
		t.Fatalf("voice agent prompter shell = %q", rec.Body.String())
	}
}

func TestOverlayStateEndpointReturnsCurrentSnapshot(t *testing.T) {
	cfg := defaultTestConfig()
	state := &appState{
		hotkey:            cfg.General.Hotkey,
		currentState:      "recording",
		overlayText:       msgRecording,
		overlayLevel:      0.02,
		overlayVisualizer: "circle",
		overlayEnabled:    true,
	}

	handler := assetHandler(cfg, filepath.Join(t.TempDir(), "config.toml"), state, &router.Router{}, nil, &config.InstallState{Mode: config.InstallModeLocal})
	req := httptest.NewRequest(http.MethodGet, "/overlay/state", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var payload overlaySnapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}

	if payload.State != "recording" || payload.Text != msgRecording || payload.Level <= 0.05 || payload.Phase != "speaking" || !payload.Visible || payload.Visualizer != "circle" {

		t.Fatalf("overlay payload = %+v", payload)
	}
}

func TestSettingsStateEndpointReturnsCurrentSettings(t *testing.T) {
	restore := secrets.UseMemoryStoreForTests()
	defer restore()
	isolateManagedHFEnvForTest(t)

	cfg := defaultTestConfig()
	cfg.Store.SQLitePath = "C:\\Users\\testuser\\AppData\\Roaming\\SpeechKit\\feedback.db"
	cfg.VoiceAgent.FrameworkPrompt = "You are the durable framework prompt."
	cfg.VoiceAgent.RefinementPrompt = "Address the user by first name."
	state := &appState{
		overlayEnabled:    true,
		hotkey:            "win+alt",
		overlayVisualizer: "pill",
	}

	handler := assetHandler(cfg, filepath.Join(t.TempDir(), "config.toml"), state, &router.Router{}, nil, &config.InstallState{Mode: config.InstallModeLocal})
	req := httptest.NewRequest(http.MethodGet, "/settings/state", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var payload settingsSnapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	var rawPayload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &rawPayload); err != nil {
		t.Fatalf("decode raw payload: %v", err)
	}
	if payload.Hotkey != "win+alt" || payload.HFModel != cfg.HuggingFace.Model || payload.Visualizer != "pill" || !payload.OverlayEnabled || payload.HFEnabled != cfg.HuggingFace.Enabled {
		t.Fatalf("settings payload = %+v", payload)
	}
	if payload.AgentMode != cfg.General.AgentMode {
		t.Fatalf("settings payload = %+v", payload)
	}
	if payload.Design != "default" {
		t.Fatalf("settings payload = %+v", payload)
	}
	if !payload.SaveAudio || payload.AudioRetentionDays != 7 {
		t.Fatalf("settings payload = %+v", payload)
	}
	if payload.StoreBackend != "sqlite" || payload.SQLitePath != cfg.Store.SQLitePath {
		t.Fatalf("settings payload = %+v", payload)
	}
	if payload.PostgresConfigured || payload.PostgresDSN != "" {
		t.Fatalf("settings payload = %+v", payload)
	}
	if payload.MaxAudioStorageMB != 500 {
		t.Fatalf("settings payload = %+v", payload)
	}
	if payload.VocabularyDictionary != "" {
		t.Fatalf("settings payload = %+v", payload)
	}
	if payload.HFTokenSource != "none" || payload.HFHasUserToken || payload.HFHasInstallToken {
		t.Fatalf("settings payload = %+v", payload)
	}
	if payload.AssistHotkey != cfg.General.AssistHotkey {
		t.Fatalf("settings payload = %+v", payload)
	}
	if payload.VoiceAgentHotkey != cfg.General.VoiceAgentHotkey {
		t.Fatalf("settings payload = %+v", payload)
	}
	if payload.VoiceAgentRefinementPrompt != cfg.VoiceAgent.RefinementPrompt {
		t.Fatalf("settings payload = %+v", payload)
	}
	if _, exists := rawPayload["voiceAgentFrameworkPrompt"]; exists {
		t.Fatalf("settings payload unexpectedly exposed voiceAgentFrameworkPrompt: %+v", rawPayload)
	}
	if len(payload.Profiles) == 0 {
		t.Fatalf("settings payload profiles = %+v, want embedded model profiles", payload.Profiles)
	}
}

func TestSettingsUpdateRoundTripPersistsAgentMode(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.General.AgentMode = "assist"
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	state := &appState{
		overlayEnabled:    true,
		overlayVisualizer: "pill",
		overlayPosition:   "top",
	}
	handler := assetHandler(cfg, cfgPath, state, &router.Router{}, nil, &config.InstallState{Mode: config.InstallModeLocal})

	form := url.Values{
		"dictate_hotkey":             {"win+alt"},
		"assist_hotkey":              {"ctrl+win+j"},
		"voice_agent_hotkey":         {"ctrl+shift+v"},
		"active_mode":                {"voice_agent"},
		"overlay_enabled":            {"1"},
		"overlay_visualizer":         {"pill"},
		"overlay_design":             {"default"},
		"overlay_position":           {"top"},
		"store_backend":              {"sqlite"},
		"store_sqlite_path":          {cfg.Store.SQLitePath},
		"store_save_audio":           {"1"},
		"store_audio_retention_days": {"7"},
		"store_max_audio_storage_mb": {"500"},
	}
	updateReq := httptest.NewRequest(http.MethodPost, "/settings/update", strings.NewReader(form.Encode()))
	updateReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	updateRec := httptest.NewRecorder()

	handler.ServeHTTP(updateRec, updateReq)

	if updateRec.Code != http.StatusOK {
		t.Fatalf("update status = %d, body=%s", updateRec.Code, updateRec.Body.String())
	}
	if got, want := cfg.General.AssistHotkey, "ctrl+win+j"; got != want {
		t.Fatalf("cfg.General.AssistHotkey = %q, want %q", got, want)
	}
	if got, want := cfg.General.VoiceAgentHotkey, "ctrl+shift+v"; got != want {
		t.Fatalf("cfg.General.VoiceAgentHotkey = %q, want %q", got, want)
	}

	stateReq := httptest.NewRequest(http.MethodGet, "/settings/state", http.NoBody)
	stateRec := httptest.NewRecorder()
	handler.ServeHTTP(stateRec, stateReq)

	if stateRec.Code != http.StatusOK {
		t.Fatalf("state status = %d, body=%s", stateRec.Code, stateRec.Body.String())
	}

	var payload settingsSnapshot
	if err := json.Unmarshal(stateRec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if got, want := payload.AssistHotkey, "ctrl+win+j"; got != want {
		t.Fatalf("payload.AssistHotkey = %q, want %q", got, want)
	}
	if got, want := payload.VoiceAgentHotkey, "ctrl+shift+v"; got != want {
		t.Fatalf("payload.VoiceAgentHotkey = %q, want %q", got, want)
	}
	if got, want := payload.ActiveMode, "voice_agent"; got != want {
		t.Fatalf("payload.ActiveMode = %q, want %q", got, want)
	}
}

func TestSettingsRoutesPersistVocabularyDictionary(t *testing.T) {
	cfg := defaultTestConfig()
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	state := &appState{
		overlayEnabled:    true,
		overlayVisualizer: "pill",
		overlayPosition:   "top",
	}
	handler := assetHandler(cfg, cfgPath, state, &router.Router{}, nil, &config.InstallState{Mode: config.InstallModeLocal})

	form := url.Values{
		"dictate_hotkey":             {"win+alt"},
		"agent_hotkey":               {"ctrl+shift+k"},
		"active_mode":                {"dictate"},
		"overlay_enabled":            {"1"},
		"overlay_visualizer":         {"pill"},
		"overlay_design":             {"default"},
		"overlay_position":           {"top"},
		"store_backend":              {"sqlite"},
		"store_sqlite_path":          {cfg.Store.SQLitePath},
		"store_save_audio":           {"1"},
		"store_audio_retention_days": {"7"},
		"store_max_audio_storage_mb": {"500"},
		"vocabulary_dictionary":      {"kombi fire => Kombify\r\nAcmeOS"},
	}
	req := httptest.NewRequest(http.MethodPost, "/settings/update", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if got, want := cfg.Vocabulary.Dictionary, "kombi fire => Kombify\nAcmeOS"; got != want {
		t.Fatalf("cfg.Vocabulary.Dictionary = %q, want %q", got, want)
	}
	if got, want := state.vocabularyDictionary, "kombi fire => Kombify\nAcmeOS"; got != want {
		t.Fatalf("state.vocabularyDictionary = %q, want %q", got, want)
	}

	stateReq := httptest.NewRequest(http.MethodGet, "/settings/state", http.NoBody)
	stateRec := httptest.NewRecorder()
	handler.ServeHTTP(stateRec, stateReq)

	if stateRec.Code != http.StatusOK {
		t.Fatalf("settings/state status = %d", stateRec.Code)
	}

	var payload settingsSnapshot
	if err := json.Unmarshal(stateRec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode settings payload: %v", err)
	}
	if got, want := payload.VocabularyDictionary, "kombi fire => Kombify\nAcmeOS"; got != want {
		t.Fatalf("payload.VocabularyDictionary = %q, want %q", got, want)
	}
}

func TestSettingsSnapshotExposesPostgresConfiguration(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.Store.Backend = "postgres"
	cfg.Store.PostgresDSN = "postgres://speechkit:secret@localhost:5432/speechkit?sslmode=disable"
	cfg.Store.MaxAudioStorageMB = 1024
	state := &appState{
		overlayEnabled:    true,
		overlayPosition:   "top",
		overlayVisualizer: "pill",
		activeMode:        "agent",
	}

	payload := state.settingsSnapshot(cfg)

	if payload.StoreBackend != "postgres" {
		t.Fatalf("StoreBackend = %q, want postgres", payload.StoreBackend)
	}
	if !payload.PostgresConfigured {
		t.Fatal("PostgresConfigured = false, want true")
	}
	if payload.PostgresDSN != cfg.Store.PostgresDSN {
		t.Fatalf("PostgresDSN = %q", payload.PostgresDSN)
	}
	if payload.MaxAudioStorageMB != 1024 {
		t.Fatalf("MaxAudioStorageMB = %d, want 1024", payload.MaxAudioStorageMB)
	}
}

func TestSettingsRoutesPersistAndClearUserHuggingFaceToken(t *testing.T) {
	restore := secrets.UseMemoryStoreForTests()
	defer restore()
	restoreBuild := config.OverrideManagedHuggingFaceBuildForTests("1")
	defer restoreBuild()
	t.Setenv("HF_TOKEN", "")
	t.Setenv("DOPPLER_PROJECT", "")
	t.Setenv("DOPPLER_CONFIG", "")
	prevFactory := newHuggingFaceProvider
	newHuggingFaceProvider = func(model, token string) stt.STTProvider {
		return &fakeProvider{name: "huggingface"}
	}
	defer func() { newHuggingFaceProvider = prevFactory }()

	cfg := defaultTestConfig()
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	state := &appState{
		overlayEnabled:    true,
		overlayVisualizer: "pill",
		overlayPosition:   "top",
	}

	handler := assetHandler(cfg, cfgPath, state, &router.Router{}, nil, &config.InstallState{Mode: config.InstallModeLocal})

	setForm := url.Values{
		"hf_token": {"user-token"},
	}
	setReq := httptest.NewRequest(http.MethodPost, "/settings/huggingface/token", strings.NewReader(setForm.Encode()))
	setReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	setRec := httptest.NewRecorder()

	handler.ServeHTTP(setRec, setReq)

	if setRec.Code != http.StatusOK {
		t.Fatalf("set status = %d, body=%s", setRec.Code, setRec.Body.String())
	}

	statusReq := httptest.NewRequest(http.MethodGet, "/settings/state", http.NoBody)
	statusRec := httptest.NewRecorder()
	handler.ServeHTTP(statusRec, statusReq)

	if statusRec.Code != http.StatusOK {
		t.Fatalf("state status = %d", statusRec.Code)
	}

	var payload settingsSnapshot
	if err := json.Unmarshal(statusRec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode state payload: %v", err)
	}
	if !payload.HFHasUserToken || payload.HFTokenSource != "user" {
		t.Fatalf("settings payload = %+v", payload)
	}

	clearReq := httptest.NewRequest(http.MethodPost, "/settings/huggingface/token/clear", http.NoBody)
	clearRec := httptest.NewRecorder()
	handler.ServeHTTP(clearRec, clearReq)

	if clearRec.Code != http.StatusOK {
		t.Fatalf("clear status = %d, body=%s", clearRec.Code, clearRec.Body.String())
	}

	statusRec = httptest.NewRecorder()
	handler.ServeHTTP(statusRec, statusReq)
	if err := json.Unmarshal(statusRec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode cleared state payload: %v", err)
	}
	if payload.HFHasUserToken || payload.HFTokenSource != "none" {
		t.Fatalf("settings payload after clear = %+v", payload)
	}
}

func TestSettingsRoutesRejectUserHuggingFaceTokenInOSSBuild(t *testing.T) {
	restore := secrets.UseMemoryStoreForTests()
	defer restore()
	restoreBuild := config.OverrideManagedHuggingFaceBuildForTests("0")
	defer restoreBuild()

	cfg := defaultTestConfig()
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	state := &appState{
		overlayEnabled:    true,
		overlayVisualizer: "pill",
		overlayPosition:   "top",
	}

	handler := assetHandler(cfg, cfgPath, state, &router.Router{}, nil, &config.InstallState{Mode: config.InstallModeLocal})

	setForm := url.Values{
		"hf_token": {"user-token"},
	}
	setReq := httptest.NewRequest(http.MethodPost, "/settings/huggingface/token", strings.NewReader(setForm.Encode()))
	setReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	setRec := httptest.NewRecorder()

	handler.ServeHTTP(setRec, setReq)

	if setRec.Code != http.StatusOK {
		t.Fatalf("set status = %d, body=%s", setRec.Code, setRec.Body.String())
	}
	if !strings.Contains(setRec.Body.String(), "Hugging Face is not available in this build") {
		t.Fatalf("set response = %s", setRec.Body.String())
	}

	statusReq := httptest.NewRequest(http.MethodGet, "/settings/state", http.NoBody)
	statusRec := httptest.NewRecorder()
	handler.ServeHTTP(statusRec, statusReq)

	if statusRec.Code != http.StatusOK {
		t.Fatalf("state status = %d", statusRec.Code)
	}

	var payload settingsSnapshot
	if err := json.Unmarshal(statusRec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode state payload: %v", err)
	}
	if payload.HFHasUserToken || payload.HFTokenSource != "none" {
		t.Fatalf("settings payload = %+v", payload)
	}
}

func TestModelProfilesEndpointFiltersHFRoutedProfilesInOSSBuild(t *testing.T) {
	restoreBuild := config.OverrideManagedHuggingFaceBuildForTests("0")
	defer restoreBuild()

	cfg := defaultTestConfig()
	handler := assetHandler(cfg, filepath.Join(t.TempDir(), "config.toml"), &appState{}, &router.Router{}, nil, &config.InstallState{Mode: config.InstallModeLocal})

	req := httptest.NewRequest(http.MethodGet, "/models/profiles", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var payload struct {
		Profiles []struct {
			ExecutionMode string `json:"executionMode"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	for _, profile := range payload.Profiles {
		if profile.ExecutionMode == "hf_routed" {
			t.Fatalf("expected hf_routed profiles to be filtered from OSS builds, payload=%s", rec.Body.String())
		}
	}
}

func TestModelProfilesEndpointReturnsOnlySwitchableModalities(t *testing.T) {
	cfg := defaultTestConfig()
	handler := assetHandler(cfg, filepath.Join(t.TempDir(), "config.toml"), &appState{}, &router.Router{}, nil, &config.InstallState{Mode: config.InstallModeLocal})

	req := httptest.NewRequest(http.MethodGet, "/models/profiles", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var payload struct {
		Profiles []struct {
			ID       string `json:"id"`
			Modality string `json:"modality"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}

	if len(payload.Profiles) == 0 {
		t.Fatal("expected switchable model profiles")
	}

	allowed := map[string]bool{"stt": true, "utility": true, "assist": true, "realtime_voice": true}
	foundGemma := false
	for _, profile := range payload.Profiles {
		if !allowed[profile.Modality] {
			t.Fatalf("unexpected modality %q in switchable catalog", profile.Modality)
		}
		if profile.ID == "utility.ollama.gemma4-e4b" {
			foundGemma = true
		}
	}
	if !foundGemma {
		t.Fatal("expected Gemma 4 local utility profile in switchable catalog")
	}
}

func TestActivateUtilityOllamaProfileUpdatesConfigAndRuntime(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.Providers.Ollama.BaseURL = "http://localhost:11434"
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	state := &appState{
		activeProfiles: map[string]string{},
	}
	handler := assetHandler(cfg, cfgPath, state, &router.Router{}, nil, &config.InstallState{Mode: config.InstallModeLocal})

	form := url.Values{
		"modality":   {"utility"},
		"profile_id": {"utility.ollama.gemma4-e4b"},
	}
	req := httptest.NewRequest(http.MethodPost, "/models/profiles/activate", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if !cfg.Providers.Ollama.Enabled {
		t.Fatal("expected ollama provider enabled")
	}
	if cfg.Providers.Ollama.UtilityModel != "gemma4:e4b" {
		t.Fatalf("ollama utility model = %q, want %q", cfg.Providers.Ollama.UtilityModel, "gemma4:e4b")
	}
	if got := state.activeProfiles["utility"]; got != "utility.ollama.gemma4-e4b" {
		t.Fatalf("active utility profile = %q, want %q", got, "utility.ollama.gemma4-e4b")
	}
	if state.genkitRT == nil {
		t.Fatal("expected genkit runtime to be reloaded")
	}
	if state.summarizeFlow == nil {
		t.Fatal("expected summarize flow to be reloaded")
	}
}

func TestActivateAssistOllamaProfileUpdatesConfigAndRuntime(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.Providers.Ollama.BaseURL = "http://localhost:11434"
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	state := &appState{
		activeProfiles: map[string]string{},
	}
	handler := assetHandler(cfg, cfgPath, state, &router.Router{}, nil, &config.InstallState{Mode: config.InstallModeLocal})

	form := url.Values{
		"modality":   {"assist"},
		"profile_id": {"assist.ollama.gemma4-e4b"},
	}
	req := httptest.NewRequest(http.MethodPost, "/models/profiles/activate", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if !cfg.Providers.Ollama.Enabled {
		t.Fatal("expected ollama provider enabled")
	}
	if cfg.Providers.Ollama.AssistModel != "gemma4:e4b" {
		t.Fatalf("ollama assist model = %q, want %q", cfg.Providers.Ollama.AssistModel, "gemma4:e4b")
	}
	if got := state.activeProfiles["assist"]; got != "assist.ollama.gemma4-e4b" {
		t.Fatalf("active assist profile = %q, want %q", got, "assist.ollama.gemma4-e4b")
	}
	if state.genkitRT == nil {
		t.Fatal("expected genkit runtime to be reloaded")
	}
	if state.assistFlow == nil {
		t.Fatal("expected assist flow to be reloaded")
	}
}

func TestSetLevelStoresOverlayMeterWhileRecording(t *testing.T) {
	overlay := &fakeOverlayWindow{}
	state := &appState{
		overlay:        overlay,
		currentState:   "recording",
		overlayEnabled: true,
	}

	state.setLevel(0.42)

	if len(overlay.scripts) != 0 {
		t.Fatalf("overlay scripts = %v, want none", overlay.scripts)
	}
	snapshot := state.overlaySnapshot()
	if snapshot.Level <= 0.42 || snapshot.Phase != "speaking" {
		t.Fatalf("overlay snapshot = %+v", snapshot)
	}
}

func TestSetLevelResetsSnapshotOutsideRecording(t *testing.T) {
	overlay := &fakeOverlayWindow{}
	state := &appState{
		overlay:        overlay,
		currentState:   "idle",
		overlayEnabled: true,
	}

	state.setLevel(0.73)

	if len(overlay.scripts) != 0 {
		t.Fatalf("overlay scripts = %v, want none", overlay.scripts)
	}
	snapshot := state.overlaySnapshot()
	if snapshot.Level != 0 {
		t.Fatalf("overlay snapshot level = %.2f, want 0", snapshot.Level)
	}
}

func defaultTestConfig() *config.Config {
	return &config.Config{
		General: config.GeneralConfig{
			Hotkey:                   "win+alt",
			DictateHotkey:            "win+alt",
			AssistHotkey:             "ctrl+win",
			VoiceAgentHotkey:         "ctrl+shift",
			DictateHotkeyBehavior:    config.HotkeyBehaviorPushToTalk,
			AssistHotkeyBehavior:     config.HotkeyBehaviorPushToTalk,
			VoiceAgentHotkeyBehavior: config.HotkeyBehaviorPushToTalk,
			DictateEnabled:           true,
			AssistEnabled:            true,
			VoiceAgentEnabled:        true,
			AgentHotkey:              "ctrl+win",
			AgentMode:                "assist",
			ActiveMode:               "none",
		},
		VoiceAgent: config.VoiceAgentConfig{
			CloseBehavior: config.VoiceAgentCloseBehaviorContinue,
		},
		HuggingFace: config.HuggingFaceConfig{
			Model: "openai/whisper-large-v3",
		},
		UI: config.UIConfig{
			OverlayEnabled:  true,
			OverlayPosition: "top",
			Visualizer:      "pill",
			Design:          "default",
		},
		Store: config.StoreConfig{
			Backend:            "sqlite",
			SaveAudio:          true,
			AudioRetentionDays: 7,
			MaxAudioStorageMB:  500,
		},
	}
}

type fakeProvider struct {
	name      string
	healthErr error
}

func (f *fakeProvider) Transcribe(ctx context.Context, audio []byte, opts stt.TranscribeOpts) (*stt.Result, error) {
	return &stt.Result{Provider: f.name}, nil
}

func (f *fakeProvider) Name() string {
	return f.name
}

func (f *fakeProvider) Health(ctx context.Context) error {
	return f.healthErr
}

func TestSaveSettingsUpdatesConfigAndRuntime(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.HuggingFace.Enabled = false
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	state := &appState{
		overlayEnabled: true,
	}
	sttRouter := &router.Router{}

	form := url.Values{
		"dictate_hotkey":             {"win+alt+d"},
		"assist_hotkey":              {"ctrl+win+j"},
		"voice_agent_hotkey":         {"ctrl+shift+k"},
		"hf_enabled":                 {"0"},
		"hf_model":                   {"openai/whisper-large-v3-turbo"},
		"overlay_enabled":            {"1"},
		"overlay_visualizer":         {"circle"},
		"overlay_design":             {"kombify"},
		"overlay_position":           {"bottom"},
		"overlay_movable":            {"1"},
		"overlay_free_x":             {"884"},
		"overlay_free_y":             {"412"},
		"store_backend":              {"postgres"},
		"store_postgres_dsn":         {"postgres://speechkit:secret@localhost:5432/speechkit?sslmode=disable"},
		"store_audio_retention_days": {"30"},
		"store_max_audio_storage_mb": {"1024"},
	}
	req := httptest.NewRequest(http.MethodPost, "/settings/update", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	t.Setenv("SPEECHKIT_ENABLE_MANAGED_HF", "1")

	flash := saveSettings(context.Background(), req, cfgPath, cfg, state, sttRouter)

	if !strings.Contains(flash, msgSaved) {
		t.Fatalf("flash = %q", flash)
	}
	if cfg.General.Hotkey != "win+alt+d" {
		t.Fatalf("hotkey = %q", cfg.General.Hotkey)
	}
	if cfg.HuggingFace.Model != "openai/whisper-large-v3-turbo" {
		t.Fatalf("model = %q", cfg.HuggingFace.Model)
	}
	if cfg.HuggingFace.Enabled {
		t.Fatal("huggingface should stay disabled")
	}
	if !cfg.UI.OverlayEnabled {
		t.Fatal("overlay should stay enabled")
	}
	if cfg.UI.Visualizer != "circle" {

		t.Fatalf("visualizer = %q", cfg.UI.Visualizer)
	}
	if cfg.UI.Design != "kombify" {
		t.Fatalf("design = %q", cfg.UI.Design)
	}
	if cfg.UI.OverlayPosition != "bottom" {
		t.Fatalf("overlay position = %q", cfg.UI.OverlayPosition)
	}
	if !cfg.UI.OverlayMovable {
		t.Fatal("overlay movable = false")
	}
	if cfg.UI.OverlayFreeX != 884 || cfg.UI.OverlayFreeY != 412 {
		t.Fatalf("free overlay coordinates = (%d,%d)", cfg.UI.OverlayFreeX, cfg.UI.OverlayFreeY)
	}
	if cfg.Store.Backend != "postgres" {
		t.Fatalf("store backend = %q", cfg.Store.Backend)
	}
	if cfg.Store.PostgresDSN == "" {
		t.Fatal("expected postgres dsn to persist")
	}
	if cfg.Store.AudioRetentionDays != 30 {
		t.Fatalf("audio retention = %d", cfg.Store.AudioRetentionDays)
	}
	if cfg.Store.MaxAudioStorageMB != 1024 {
		t.Fatalf("max audio storage = %d", cfg.Store.MaxAudioStorageMB)
	}

	reloaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load saved config: %v", err)
	}
	if reloaded.General.Hotkey != "win+alt+d" {
		t.Fatalf("reloaded hotkey = %q", reloaded.General.Hotkey)
	}
	if reloaded.HuggingFace.Model != "openai/whisper-large-v3-turbo" {
		t.Fatalf("reloaded model = %q", reloaded.HuggingFace.Model)
	}
	if reloaded.HuggingFace.Enabled {
		t.Fatal("reloaded huggingface should stay disabled")
	}
	if reloaded.UI.Visualizer != "circle" {

		t.Fatalf("reloaded visualizer = %q", reloaded.UI.Visualizer)
	}
	if reloaded.UI.Design != "kombify" {
		t.Fatalf("reloaded design = %q", reloaded.UI.Design)
	}
	if reloaded.UI.OverlayPosition != "bottom" {
		t.Fatalf("reloaded overlay position = %q", reloaded.UI.OverlayPosition)
	}
	if !reloaded.UI.OverlayMovable {
		t.Fatal("reloaded overlay movable = false")
	}
	if reloaded.UI.OverlayFreeX != 884 || reloaded.UI.OverlayFreeY != 412 {
		t.Fatalf("reloaded free overlay coordinates = (%d,%d)", reloaded.UI.OverlayFreeX, reloaded.UI.OverlayFreeY)
	}
	if reloaded.Store.Backend != "postgres" {
		t.Fatalf("reloaded store backend = %q", reloaded.Store.Backend)
	}
	if reloaded.Store.PostgresDSN == "" {
		t.Fatal("expected reloaded postgres dsn to persist")
	}
	if reloaded.Store.AudioRetentionDays != 30 {
		t.Fatalf("reloaded audio retention = %d", reloaded.Store.AudioRetentionDays)
	}
	if reloaded.Store.MaxAudioStorageMB != 1024 {
		t.Fatalf("reloaded max audio storage = %d", reloaded.Store.MaxAudioStorageMB)
	}
}

func TestSaveSettingsRejectsPostgresBackendWithoutDSN(t *testing.T) {
	cfg := defaultTestConfig()
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	state := &appState{}
	sttRouter := &router.Router{}

	form := url.Values{
		"store_backend":      {"postgres"},
		"overlay_enabled":    {"1"},
		"overlay_visualizer": {"pill"},
		"overlay_design":     {"default"},
	}
	req := httptest.NewRequest(http.MethodPost, "/settings/update", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	t.Setenv("SPEECHKIT_ENABLE_MANAGED_HF", "1")

	flash := saveSettings(context.Background(), req, cfgPath, cfg, state, sttRouter)

	if !strings.Contains(flash, msgPostgresDSNReq) {
		t.Fatalf("flash = %q", flash)
	}
	if cfg.Store.Backend != "sqlite" {
		t.Fatalf("store backend = %q, want sqlite", cfg.Store.Backend)
	}
}

func TestSaveSettingsKeepsHFDisabledWithoutManagedToken(t *testing.T) {
	restore := secrets.UseMemoryStoreForTests()
	defer restore()
	restoreBuild := config.OverrideManagedHuggingFaceBuildForTests("1")
	defer restoreBuild()
	isolateManagedHFEnvForTest(t)

	cfg := defaultTestConfig()
	cfg.HuggingFace.Enabled = false
	cfg.Routing.Strategy = "cloud-only"
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	state := &appState{}
	sttRouter := &router.Router{}

	form := url.Values{
		"hf_model":           {"openai/whisper-large-v3"},
		"overlay_enabled":    {"1"},
		"overlay_visualizer": {"pill"},
		"overlay_design":     {"default"},
	}
	req := httptest.NewRequest(http.MethodPost, "/settings/update", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	t.Setenv("SPEECHKIT_ENABLE_MANAGED_HF", "1")

	flash := saveSettings(context.Background(), req, cfgPath, cfg, state, sttRouter)

	if !strings.Contains(flash, msgSaved) {
		t.Fatalf("flash = %q", flash)
	}
	if cfg.HuggingFace.Enabled {
		t.Fatal("huggingface should remain disabled without a managed token")
	}
}

func TestSaveSettingsAppliesManagedHFWithStoredUserToken(t *testing.T) {
	restore := secrets.UseMemoryStoreForTests()
	defer restore()
	restoreBuild := config.OverrideManagedHuggingFaceBuildForTests("1")
	defer restoreBuild()
	prevFactory := newHuggingFaceProvider
	newHuggingFaceProvider = func(model, token string) stt.STTProvider {
		return &fakeProvider{name: "huggingface"}
	}
	defer func() { newHuggingFaceProvider = prevFactory }()

	if err := secrets.SetUserHuggingFaceToken("user-token"); err != nil {
		t.Fatalf("set user token: %v", err)
	}

	cfg := defaultTestConfig()
	cfg.HuggingFace.Enabled = false
	cfg.Routing.Strategy = "cloud-only"
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	state := &appState{}
	sttRouter := &router.Router{}

	form := url.Values{
		"hf_model":           {"openai/whisper-large-v3"},
		"overlay_enabled":    {"1"},
		"overlay_visualizer": {"pill"},
		"overlay_design":     {"default"},
	}
	req := httptest.NewRequest(http.MethodPost, "/settings/update", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	t.Setenv("SPEECHKIT_ENABLE_MANAGED_HF", "1")

	flash := saveSettings(context.Background(), req, cfgPath, cfg, state, sttRouter)

	if !strings.Contains(flash, msgSaved) {
		t.Fatalf("flash = %q", flash)
	}
	if !cfg.HuggingFace.Enabled {
		t.Fatal("huggingface should be enabled when a stored user token exists")
	}
	if sttRouter.HuggingFace() == nil {
		t.Fatal("expected huggingface provider to be configured from stored user token")
	}
}

func TestSaveSettingsAllowsNonHFChangesWithoutHFValidation(t *testing.T) {
	restoreBuild := config.OverrideManagedHuggingFaceBuildForTests("1")
	defer restoreBuild()

	cfg := defaultTestConfig()
	cfg.HuggingFace.Enabled = true
	cfg.HuggingFace.TokenEnv = "TEST_SPEECHKIT_MISSING_HF_TOKEN"
	cfg.HuggingFace.Model = "openai/whisper-large-v3"
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	state := &appState{overlayEnabled: true}
	sttRouter := &router.Router{}

	form := url.Values{
		"dictate_hotkey":     {"win+alt+d"},
		"assist_hotkey":      {"ctrl+win+j"},
		"voice_agent_hotkey": {"ctrl+shift+k"},
		"hf_enabled":         {"1"},
		"hf_model":           {"openai/whisper-large-v3"},
		"overlay_enabled":    {"1"},
		"overlay_visualizer": {"circle"},
		"overlay_design":     {"default"},
	}
	req := httptest.NewRequest(http.MethodPost, "/settings/update", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	flash := saveSettings(context.Background(), req, cfgPath, cfg, state, sttRouter)

	if !strings.Contains(flash, msgSaved) {
		t.Fatalf("flash = %q", flash)
	}
	if cfg.General.Hotkey != "win+alt+d" {
		t.Fatalf("hotkey = %q", cfg.General.Hotkey)
	}
	if cfg.UI.Visualizer != "circle" {

		t.Fatalf("visualizer = %q", cfg.UI.Visualizer)
	}
}

func TestSaveSettingsKeepsManagedHFEnabledWhenBuildDefaultsAreActive(t *testing.T) {
	restoreBuild := config.OverrideManagedHuggingFaceBuildForTests("1")
	defer restoreBuild()

	cfg := defaultTestConfig()
	cfg.HuggingFace.Enabled = false
	cfg.Routing.Strategy = "cloud-only"
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	state := &appState{overlayEnabled: true}
	sttRouter := &router.Router{}

	form := url.Values{
		"hf_enabled":         {"0"},
		"hf_model":           {"openai/whisper-large-v3"},
		"overlay_enabled":    {"1"},
		"overlay_visualizer": {"pill"},
		"overlay_design":     {"default"},
	}
	req := httptest.NewRequest(http.MethodPost, "/settings/update", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	t.Setenv("SPEECHKIT_ENABLE_MANAGED_HF", "1")
	t.Setenv("HF_TOKEN", "test-token")

	flash := saveSettings(context.Background(), req, cfgPath, cfg, state, sttRouter)

	if !strings.Contains(flash, msgSaved) {
		t.Fatalf("flash = %q", flash)
	}
	if !cfg.HuggingFace.Enabled {
		t.Fatal("managed huggingface should remain enabled in managed builds")
	}
	if sttRouter.HuggingFace() == nil {
		t.Fatal("managed huggingface provider should stay configured")
	}
}

func TestProviderCredentialSaveRefreshesRuntimeProvidersAndProfiles(t *testing.T) {
	restore := secrets.UseMemoryStoreForTests()
	defer restore()

	cfg := defaultTestConfig()
	cfg.Providers.OpenAI.Enabled = true
	cfg.Providers.OpenAI.APIKeyEnv = "SPEECHKIT_TEST_OPENAI_KEY"
	cfg.Providers.OpenAI.STTModel = "whisper-1"
	cfg.Providers.OpenAI.UtilityModel = "gpt-5.4-mini-2026-03-17"
	cfg.Providers.OpenAI.AssistModel = "gpt-5.4-2026-03-05"
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	state := &appState{
		overlayEnabled: true,
	}
	sttRouter := &router.Router{}
	handler := assetHandler(cfg, cfgPath, state, sttRouter, nil, &config.InstallState{Mode: config.InstallModeLocal})

	form := url.Values{
		"provider":   {"openai"},
		"credential": {"test-openai-key"},
	}
	req := httptest.NewRequest(http.MethodPost, "/settings/provider-credentials/save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if sttRouter.Cloud("openai") == nil {
		t.Fatal("expected openai provider to be configured in the runtime router")
	}
	if !slices.Contains(runtimeAvailableProviders(t.Context(), sttRouter), "openai") {
		t.Fatalf("runtime providers = %v, want openai to be available", runtimeAvailableProviders(t.Context(), sttRouter))
	}
	if !slices.Contains(state.providers, "openai") {
		t.Fatalf("state.providers = %v, want openai", state.providers)
	}
	if got, want := state.activeProfiles[string(models.ModalitySTT)], "stt.openai.whisper-1"; got != want {
		t.Fatalf("active STT profile = %q, want %q", got, want)
	}
	if got, want := state.activeProfiles[string(models.ModalityUtility)], "utility.openai.gpt-5.4-mini"; got != want {
		t.Fatalf("active utility profile = %q, want %q", got, want)
	}
	if got, want := state.activeProfiles[string(models.ModalityAssist)], "assist.openai.gpt-5.4"; got != want {
		t.Fatalf("active assist profile = %q, want %q", got, want)
	}
	if state.genkitRT == nil {
		t.Fatal("expected genkit runtime to be rebuilt after saving provider credentials")
	}
	if len(state.genkitRT.ModelInfos()) == 0 {
		t.Fatal("expected model infos to be available after rebuilding the runtime")
	}
}

func TestSaveSettingsSwitchesActiveOverlayWindowToDotVisualizer(t *testing.T) {
	cfg := defaultTestConfig()
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	pillAnchor := &fakeOverlayWindow{visible: true}
	pillPanel := &fakeOverlayWindow{}
	dotAnchor := &fakeOverlayWindow{}
	radialMenu := &fakeOverlayWindow{}
	state := &appState{
		pillAnchor:        pillAnchor,
		pillPanel:         pillPanel,
		dotAnchor:         dotAnchor,
		radialMenu:        radialMenu,
		overlayEnabled:    true,
		overlayVisualizer: "pill",
		overlayPosition:   "top",
		screenLocator:     &fakeScreenLocator{bounds: screenBounds{X: 0, Y: 0, Width: 1920, Height: 1080}, ok: true},
	}
	sttRouter := &router.Router{}

	form := url.Values{
		"dictate_hotkey":             {"win+alt"},
		"assist_hotkey":              {"ctrl+win"},
		"voice_agent_hotkey":         {"ctrl+shift"},
		"dictate_enabled":            {"1"},
		"assist_enabled":             {"1"},
		"voice_agent_enabled":        {"1"},
		"overlay_enabled":            {"1"},
		"overlay_visualizer":         {"circle"},
		"overlay_design":             {"default"},
		"overlay_position":           {"top"},
		"store_backend":              {"sqlite"},
		"store_sqlite_path":          {cfg.Store.SQLitePath},
		"store_save_audio":           {"1"},
		"store_audio_retention_days": {"7"},
		"store_max_audio_storage_mb": {"500"},
	}
	req := httptest.NewRequest(http.MethodPost, "/settings/update", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	flash := saveSettings(context.Background(), req, cfgPath, cfg, state, sttRouter)

	if !strings.Contains(flash, msgSaved) {
		t.Fatalf("flash = %q", flash)
	}
	if got := state.overlaySnapshot().Visualizer; got != "circle" {
		t.Fatalf("overlay visualizer = %q, want circle", got)
	}
	if dotAnchor.showCalls == 0 {
		t.Fatal("dot anchor should be shown after switching to the dot visualizer")
	}
	if pillAnchor.hideCalls == 0 {
		t.Fatal("pill anchor should be hidden after switching to the dot visualizer")
	}
}

func TestBuildRouterEnablesHFWhenConfiguredAndTokenAvailable(t *testing.T) {
	restoreBuild := config.OverrideManagedHuggingFaceBuildForTests("1")
	defer restoreBuild()

	cfg := defaultTestConfig()
	cfg.HuggingFace.Enabled = true
	cfg.HuggingFace.TokenEnv = "HF_TOKEN"
	t.Setenv("HF_TOKEN", "test-token")

	r, msgs := buildRouter(cfg)

	if r.HuggingFace() == nil {
		t.Fatal("expected huggingface provider to be configured")
	}
	if len(msgs) == 0 || !strings.Contains(strings.Join(msgs, " "), "HuggingFace:") {
		t.Fatalf("provider log = %v", msgs)
	}
}

func TestBuildRouterAutoEnablesManagedHFForCloudOnlyWhenExplicitlyEnabled(t *testing.T) {
	restoreBuild := config.OverrideManagedHuggingFaceBuildForTests("1")
	defer restoreBuild()

	cfg := defaultTestConfig()
	cfg.HuggingFace.Enabled = false
	cfg.Local.Enabled = false
	cfg.VPS.Enabled = false
	cfg.Routing.Strategy = "cloud-only"
	t.Setenv("SPEECHKIT_ENABLE_MANAGED_HF", "1")
	t.Setenv("HF_TOKEN", "test-token")

	if !config.ApplyManagedIntegrationDefaults(cfg) {
		t.Fatal("expected managed defaults to be applied")
	}

	r, _ := buildRouter(cfg)

	if r.HuggingFace() == nil {
		t.Fatal("expected huggingface provider after managed defaults")
	}
}

func TestBuildRouterWarnsWhenHFConfiguredWithoutToken(t *testing.T) {
	restore := secrets.UseMemoryStoreForTests()
	defer restore()
	restoreBuild := config.OverrideManagedHuggingFaceBuildForTests("1")
	defer restoreBuild()

	cfg := defaultTestConfig()
	cfg.HuggingFace.Enabled = true
	cfg.HuggingFace.TokenEnv = "TEST_SPEECHKIT_MISSING_HF_TOKEN"

	r, msgs := buildRouter(cfg)

	if r.HuggingFace() != nil {
		t.Fatal("expected huggingface provider to stay nil without token")
	}
	if len(msgs) == 0 || !strings.Contains(strings.Join(msgs, " "), "TEST_SPEECHKIT_MISSING_HF_TOKEN not found") {
		t.Fatalf("provider log = %v", msgs)
	}
}

func TestDefaultLocalModelPathPrefersBundleModelsDirectory(t *testing.T) {
	exeDir := t.TempDir()
	modelName := "ggml-small.bin"
	want := filepath.Join(exeDir, "models", modelName)
	if err := os.MkdirAll(filepath.Dir(want), 0o755); err != nil {
		t.Fatalf("mkdir models: %v", err)
	}
	if err := os.WriteFile(want, []byte("fake model"), 0o644); err != nil {
		t.Fatalf("write model: %v", err)
	}

	got := defaultLocalModelPath(exeDir, t.TempDir(), modelName)

	if got != want {
		t.Fatalf("model path = %q, want %q", got, want)
	}
}

func TestDefaultLocalModelPathFallsBackToLocalAppData(t *testing.T) {
	localAppData := t.TempDir()
	modelName := "ggml-small.bin"
	want := filepath.Join(localAppData, "SpeechKit", "models", modelName)

	got := defaultLocalModelPath("", localAppData, modelName)

	if got != want {
		t.Fatalf("model path = %q, want %q", got, want)
	}
}

func TestBuildRouterPrefersStoredUserTokenOverEnvToken(t *testing.T) {
	restore := secrets.UseMemoryStoreForTests()
	defer restore()
	restoreBuild := config.OverrideManagedHuggingFaceBuildForTests("1")
	defer restoreBuild()

	if err := secrets.SetInstallHuggingFaceToken("install-token"); err != nil {
		t.Fatalf("set install token: %v", err)
	}
	if err := secrets.SetUserHuggingFaceToken("user-token"); err != nil {
		t.Fatalf("set user token: %v", err)
	}

	cfg := defaultTestConfig()
	cfg.HuggingFace.Enabled = true
	cfg.HuggingFace.TokenEnv = "HF_TOKEN"
	t.Setenv("HF_TOKEN", "env-token")

	r, msgs := buildRouter(cfg)

	if r.HuggingFace() == nil {
		t.Fatal("expected huggingface provider to be configured")
	}
	if len(msgs) == 0 || !strings.Contains(strings.Join(msgs, " "), "source: user") {
		t.Fatalf("provider log = %v", msgs)
	}
}

func TestSettingsSnapshotIncludesHuggingFaceTokenStatus(t *testing.T) {
	restore := secrets.UseMemoryStoreForTests()
	defer restore()
	restoreBuild := config.OverrideManagedHuggingFaceBuildForTests("1")
	defer restoreBuild()

	if err := secrets.SetInstallHuggingFaceToken("install-token"); err != nil {
		t.Fatalf("set install token: %v", err)
	}

	cfg := defaultTestConfig()
	state := &appState{
		overlayEnabled:    true,
		overlayPosition:   "top",
		overlayVisualizer: "pill",
		activeMode:        "dictate",
	}

	payload := state.settingsSnapshot(cfg)

	if !payload.HFHasInstallToken {
		t.Fatal("expected install token status in settings snapshot")
	}
	if payload.HFHasUserToken {
		t.Fatal("did not expect user token status in settings snapshot")
	}
	if payload.HFTokenSource != string(secrets.TokenSourceInstall) {
		t.Fatalf("token source = %q", payload.HFTokenSource)
	}
}

func TestValidateCloudProvidersKeepsConfiguredProvidersAfterHealthFailure(t *testing.T) {
	r := &router.Router{}
	r.SetHuggingFace(&fakeProvider{name: "huggingface", healthErr: errors.New("temporary outage")})
	r.SetVPS(&fakeProvider{name: "vps", healthErr: errors.New("temporary outage")})

	msgs := validateCloudProviders(context.Background(), r)

	if r.HuggingFace() == nil {
		t.Fatal("huggingface provider should stay configured after failed startup health check")
	}
	if r.VPS() == nil {
		t.Fatal("vps provider should stay configured after failed startup health check")
	}
	joined := strings.Join(msgs, " | ")
	if !strings.Contains(joined, "HuggingFace unavailable") || !strings.Contains(joined, "VPS unavailable") {
		t.Fatalf("messages = %q", joined)
	}
}

func TestMissingProviderHintForCloudOnlyWithNoEnabledProviders(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.HuggingFace.Enabled = false
	cfg.VPS.Enabled = false
	cfg.Local.Enabled = false
	cfg.Routing.Strategy = "cloud-only"

	hint := missingProviderHint(cfg)

	if !strings.Contains(hint, "Cloud-only routing") {
		t.Fatalf("hint = %q", hint)
	}
}

func TestMissingProviderHintForHFWithoutToken(t *testing.T) {
	restore := secrets.UseMemoryStoreForTests()
	defer restore()
	restoreBuild := config.OverrideManagedHuggingFaceBuildForTests("1")
	defer restoreBuild()

	cfg := defaultTestConfig()
	cfg.HuggingFace.Enabled = true
	cfg.HuggingFace.TokenEnv = "TEST_SPEECHKIT_MISSING_HF_TOKEN"
	cfg.VPS.Enabled = false

	hint := missingProviderHint(cfg)

	if !strings.Contains(hint, "TEST_SPEECHKIT_MISSING_HF_TOKEN") {
		t.Fatalf("hint = %q", hint)
	}
}

func TestOverlayPositionBottom(t *testing.T) {
	bounds := screenBounds{X: 0, Y: 0, Width: 1920, Height: 1080}
	x, y := overlayWindowPosition(bounds, "bottom", "pill")

	wantX := (1920 - overlayWindowSize) / 2
	wantY := 1080 - overlayWindowSize
	if x != wantX {
		t.Fatalf("overlay x = %d, want %d", x, wantX)
	}
	if y != wantY {
		t.Fatalf("overlay y = %d, want %d", y, wantY)
	}
}

func TestComputeBubbleRegionBottomUsesWindowY(t *testing.T) {
	bubble := computeBubbleRegion(810, 780, "bottom", "pill")

	wantX := 810 + (overlayWindowSize-pillBubbleW)/2
	wantY := 780 + overlayWindowSize - pillBubbleH - overlayEdgeMargin
	if bubble.X != wantX || bubble.Y != wantY {
		t.Fatalf("bottom bubble = %+v, want X=%d Y=%d", bubble, wantX, wantY)
	}
}

func TestOverlayPositionLeft(t *testing.T) {
	bounds := screenBounds{X: 0, Y: 0, Width: 2560, Height: 1440}
	x, y := overlayWindowPosition(bounds, "left", "pill")

	wantX := 0
	wantY := (1440 - overlayWindowSize) / 2
	if x != wantX {
		t.Fatalf("overlay x = %d, want %d", x, wantX)
	}
	if y != wantY {
		t.Fatalf("overlay y = %d, want %d", y, wantY)
	}
}

func TestOverlayPositionRight(t *testing.T) {
	bounds := screenBounds{X: 0, Y: 0, Width: 2560, Height: 1440}
	x, y := overlayWindowPosition(bounds, "right", "pill")

	wantX := 2560 - overlayWindowSize
	wantY := (1440 - overlayWindowSize) / 2
	if x != wantX {
		t.Fatalf("overlay x = %d, want %d", x, wantX)
	}
	if y != wantY {
		t.Fatalf("overlay y = %d, want %d", y, wantY)
	}
}

func TestOverlayPositionDefaultsToTop(t *testing.T) {
	bounds := screenBounds{X: 0, Y: 0, Width: 1920, Height: 1080}
	xEmpty, yEmpty := overlayWindowPosition(bounds, "", "pill")
	xTop, yTop := overlayWindowPosition(bounds, "top", "pill")

	if xEmpty != xTop || yEmpty != yTop {
		t.Fatalf("empty position (%d,%d) differs from top (%d,%d)", xEmpty, yEmpty, xTop, yTop)
	}
}

func TestDashboardHistoryEndpoint(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s, err := store.New(store.StoreConfig{Backend: "sqlite", SQLitePath: dbPath, SaveAudio: true, MaxAudioStorageMB: 100})
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	defer s.Close()

	audio := []byte("fake wav bytes")
	if err := s.SaveTranscription(context.Background(), "test text", "de", "hf", "openai/whisper-large-v3", 2400, 300, audio); err != nil {
		t.Fatalf("store.SaveTranscription: %v", err)
	}

	cfg := defaultTestConfig()
	state := &appState{}
	handler := assetHandler(cfg, filepath.Join(t.TempDir(), "config.toml"), state, &router.Router{}, s, &config.InstallState{Mode: config.InstallModeLocal})

	req := httptest.NewRequest(http.MethodGet, "/dashboard/history", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var entries []struct {
		ID         int64  `json:"id"`
		Model      string `json:"model"`
		DurationMs int64  `json:"durationMs"`
		Audio      *struct {
			StorageKind string `json:"storageKind"`
			SizeBytes   int64  `json:"sizeBytes"`
			DurationMs  int64  `json:"durationMs"`
		} `json:"audio"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected non-empty history array")
	}
	if entries[0].Model != "openai/whisper-large-v3" {
		t.Fatalf("history model = %q, want %q", entries[0].Model, "openai/whisper-large-v3")
	}
	if entries[0].DurationMs != 2400 {
		t.Fatalf("history duration = %d, want %d", entries[0].DurationMs, 2400)
	}
	if entries[0].Audio == nil {
		t.Fatal("expected audio metadata in history entry")
	}
	if entries[0].Audio.StorageKind != "local-file" {
		t.Fatalf("audio storage kind = %q, want %q", entries[0].Audio.StorageKind, "local-file")
	}
	if entries[0].Audio.SizeBytes != int64(len(audio)) {
		t.Fatalf("audio size = %d, want %d", entries[0].Audio.SizeBytes, len(audio))
	}
}

func TestDashboardHistoryEndpointFallsBackToConfiguredModelHints(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s, err := store.New(store.StoreConfig{
		Backend:                 "sqlite",
		SQLitePath:              dbPath,
		SaveAudio:               true,
		MaxAudioStorageMB:       100,
		TranscriptionModelHints: map[string]string{"huggingface": "openai/whisper-large-v3", "hf": "openai/whisper-large-v3"},
	})
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	defer s.Close()

	if err := s.SaveTranscription(context.Background(), "test text", "de", "hf", "", 2400, 300, nil); err != nil {
		t.Fatalf("store.SaveTranscription: %v", err)
	}

	cfg := defaultTestConfig()
	state := &appState{}
	handler := assetHandler(cfg, filepath.Join(t.TempDir(), "config.toml"), state, &router.Router{}, s, &config.InstallState{Mode: config.InstallModeLocal})

	req := httptest.NewRequest(http.MethodGet, "/dashboard/history", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var entries []struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if entries[0].Model != "openai/whisper-large-v3" {
		t.Fatalf("history model = %q, want %q", entries[0].Model, "openai/whisper-large-v3")
	}
}

func TestDashboardHistoryEndpointEmptyWhenNoStore(t *testing.T) {
	cfg := defaultTestConfig()
	state := &appState{}
	handler := assetHandler(cfg, filepath.Join(t.TempDir(), "config.toml"), state, &router.Router{}, nil, &config.InstallState{Mode: config.InstallModeLocal})

	req := httptest.NewRequest(http.MethodGet, "/dashboard/history", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	body := strings.TrimSpace(rec.Body.String())
	if body != "[]" {
		t.Fatalf("body = %q, want %q", body, "[]")
	}
}

func TestDashboardStatsEndpoint(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(store.StoreConfig{Backend: "sqlite", SQLitePath: dbPath, SaveAudio: true, AudioRetentionDays: 7, MaxAudioStorageMB: 100})
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	defer s.Close()

	if err := s.SaveTranscription(context.Background(), "one two three four", "de", "hf", "", 2400, 300, nil); err != nil {
		t.Fatalf("store.SaveTranscription: %v", err)
	}
	if _, err := s.SaveQuickNote(context.Background(), "quick note body", "de", "capture", 1200, 180, nil); err != nil {
		t.Fatalf("store.SaveQuickNote: %v", err)
	}

	cfg := defaultTestConfig()
	state := &appState{}
	handler := assetHandler(cfg, filepath.Join(t.TempDir(), "config.toml"), state, &router.Router{}, s, &config.InstallState{Mode: config.InstallModeLocal})

	req := httptest.NewRequest(http.MethodGet, "/dashboard/stats", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var payload struct {
		Transcriptions        int     `json:"transcriptions"`
		QuickNotes            int     `json:"quickNotes"`
		TotalWords            int     `json:"totalWords"`
		AverageWordsPerMinute float64 `json:"averageWordsPerMinute"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Transcriptions != 1 {
		t.Fatalf("payload.Transcriptions = %d, want 1", payload.Transcriptions)
	}
	if payload.QuickNotes != 1 {
		t.Fatalf("payload.QuickNotes = %d, want 1", payload.QuickNotes)
	}
	if payload.TotalWords < 7 {
		t.Fatalf("payload.TotalWords = %d, want at least 7", payload.TotalWords)
	}
	if payload.AverageWordsPerMinute <= 0 {
		t.Fatalf("payload.AverageWordsPerMinute = %f, want > 0", payload.AverageWordsPerMinute)
	}
}

func TestDashboardQuickNotesEndpointIncludesAudioMetadata(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s, err := store.New(store.StoreConfig{Backend: "sqlite", SQLitePath: dbPath, SaveAudio: true, MaxAudioStorageMB: 100})
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	defer s.Close()

	audio := []byte("fake quick note wav")
	if _, err := s.SaveQuickNote(context.Background(), "quick note body", "de", "capture", 1200, 180, audio); err != nil {
		t.Fatalf("store.SaveQuickNote: %v", err)
	}

	cfg := defaultTestConfig()
	state := &appState{}
	handler := assetHandler(cfg, filepath.Join(t.TempDir(), "config.toml"), state, &router.Router{}, s, &config.InstallState{Mode: config.InstallModeLocal})

	req := httptest.NewRequest(http.MethodGet, "/dashboard/quicknotes", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var entries []struct {
		ID         int64 `json:"id"`
		DurationMs int64 `json:"durationMs"`
		Audio      *struct {
			StorageKind string `json:"storageKind"`
			SizeBytes   int64  `json:"sizeBytes"`
			DurationMs  int64  `json:"durationMs"`
		} `json:"audio"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if entries[0].DurationMs != 1200 {
		t.Fatalf("quick note duration = %d, want %d", entries[0].DurationMs, 1200)
	}
	if entries[0].Audio == nil {
		t.Fatal("expected audio metadata in quick note entry")
	}
	if entries[0].Audio.StorageKind != "local-file" {
		t.Fatalf("audio storage kind = %q, want %q", entries[0].Audio.StorageKind, "local-file")
	}
	if entries[0].Audio.SizeBytes != int64(len(audio)) {
		t.Fatalf("audio size = %d, want %d", entries[0].Audio.SizeBytes, len(audio))
	}
}

func TestDashboardAudioDownloadEndpoint(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s, err := store.New(store.StoreConfig{Backend: "sqlite", SQLitePath: dbPath, SaveAudio: true, MaxAudioStorageMB: 100})
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	defer s.Close()

	audio := []byte("fake wav bytes")
	if err := s.SaveTranscription(context.Background(), "test text", "de", "hf", "", 2400, 300, audio); err != nil {
		t.Fatalf("store.SaveTranscription: %v", err)
	}

	cfg := defaultTestConfig()
	state := &appState{}
	handler := assetHandler(cfg, filepath.Join(t.TempDir(), "config.toml"), state, &router.Router{}, s, &config.InstallState{Mode: config.InstallModeLocal})

	req := httptest.NewRequest(http.MethodGet, "/dashboard/audio?kind=transcription&id=1", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "audio/wav" {
		t.Fatalf("content type = %q, want %q", got, "audio/wav")
	}
	if !strings.Contains(rec.Header().Get("Content-Disposition"), "transcription-1.wav") {
		t.Fatalf("content disposition = %q, want transcription filename", rec.Header().Get("Content-Disposition"))
	}
	if got := rec.Body.Bytes(); !bytes.Equal(got, audio) {
		t.Fatalf("body = %q, want %q", string(got), string(audio))
	}
}

func TestDashboardAudioRevealEndpoint(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s, err := store.New(store.StoreConfig{Backend: "sqlite", SQLitePath: dbPath, SaveAudio: true, MaxAudioStorageMB: 100})
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	defer s.Close()

	audio := []byte("fake quick note wav")
	id, err := s.SaveQuickNote(context.Background(), "quick note body", "de", "capture", 1200, 180, audio)
	if err != nil {
		t.Fatalf("store.SaveQuickNote: %v", err)
	}

	note, err := s.GetQuickNote(context.Background(), id)
	if err != nil {
		t.Fatalf("store.GetQuickNote: %v", err)
	}

	var revealedPath string
	prevReveal := revealAudioFileInShell
	revealAudioFileInShell = func(path string) error {
		revealedPath = path
		return nil
	}
	defer func() {
		revealAudioFileInShell = prevReveal
	}()

	cfg := defaultTestConfig()
	state := &appState{}
	handler := assetHandler(cfg, filepath.Join(t.TempDir(), "config.toml"), state, &router.Router{}, s, &config.InstallState{Mode: config.InstallModeLocal})

	form := url.Values{
		"kind": {"quicknote"},
		"id":   {strconv.FormatInt(id, 10)},
	}
	req := httptest.NewRequest(http.MethodPost, "/dashboard/audio/reveal", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if revealedPath != note.AudioPath {
		t.Fatalf("revealed path = %q, want %q", revealedPath, note.AudioPath)
	}
}

func TestDashboardLogsEndpoint(t *testing.T) {
	cfg := defaultTestConfig()
	state := &appState{}
	state.addLog("test log message", "info")

	handler := assetHandler(cfg, filepath.Join(t.TempDir(), "config.toml"), state, &router.Router{}, nil, &config.InstallState{Mode: config.InstallModeLocal})

	req := httptest.NewRequest(http.MethodGet, "/dashboard/logs", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var entries []logEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	found := false
	for _, e := range entries {
		if e.Message == "test log message" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("log entries = %+v, want entry with message %q", entries, "test log message")
	}
}

func TestQuickNoteRouteOpenCaptureDispatchesRuntimeCommand(t *testing.T) {
	cfg := defaultTestConfig()
	var commands []speechkit.Command
	state := &appState{}
	state.engine = speechkit.NewRuntime(speechkit.Snapshot{}, speechkit.Hooks{
		HandleCommand: func(_ context.Context, command speechkit.Command) error {
			commands = append(commands, command.Clone())
			return nil
		},
	})

	handler := assetHandler(cfg, filepath.Join(t.TempDir(), "config.toml"), state, &router.Router{}, nil, &config.InstallState{Mode: config.InstallModeLocal})
	req := httptest.NewRequest(http.MethodPost, "/quicknotes/open-capture", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if len(commands) != 1 {
		t.Fatalf("commands = %d, want 1", len(commands))
	}
	if got, want := commands[0].Type, speechkit.CommandOpenQuickCapture; got != want {
		t.Fatalf("commands[0].Type = %q, want %q", got, want)
	}
}

func TestQuickNoteRouteRecordModeDispatchesRuntimeCommand(t *testing.T) {
	cfg := defaultTestConfig()
	var commands []speechkit.Command
	state := &appState{}
	state.engine = speechkit.NewRuntime(speechkit.Snapshot{}, speechkit.Hooks{
		HandleCommand: func(_ context.Context, command speechkit.Command) error {
			commands = append(commands, command.Clone())
			return nil
		},
	})

	handler := assetHandler(cfg, filepath.Join(t.TempDir(), "config.toml"), state, &router.Router{}, nil, &config.InstallState{Mode: config.InstallModeLocal})
	req := httptest.NewRequest(http.MethodPost, "/quicknotes/record-mode?id=7", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if len(commands) != 1 {
		t.Fatalf("commands = %d, want 1", len(commands))
	}
	if got, want := commands[0].Type, speechkit.CommandArmQuickNoteRecording; got != want {
		t.Fatalf("commands[0].Type = %q, want %q", got, want)
	}
	if got, want := commands[0].NoteID, int64(7); got != want {
		t.Fatalf("commands[0].NoteID = %d, want %d", got, want)
	}
}

func TestQuickNoteSummaryRouteReturnsGeneratedSummary(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s, err := store.New(store.StoreConfig{Backend: "sqlite", SQLitePath: dbPath, SaveAudio: true, MaxAudioStorageMB: 100})
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	defer s.Close()

	id, err := s.SaveQuickNote(
		context.Background(),
		"Meeting with the Android team. Finalise the manifest fix before release. Update CI so lint and tests run on every pull request.",
		"de",
		"manual",
		0,
		0,
		nil,
	)
	if err != nil {
		t.Fatalf("store.SaveQuickNote: %v", err)
	}

	cfg := defaultTestConfig()
	state := &appState{}
	handler := assetHandler(cfg, filepath.Join(t.TempDir(), "config.toml"), state, &router.Router{}, s, &config.InstallState{Mode: config.InstallModeLocal})

	form := url.Values{"id": {strconv.FormatInt(id, 10)}}
	req := httptest.NewRequest(http.MethodPost, "/quicknotes/summary", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var payload struct {
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Summary == "" {
		t.Fatal("expected summary text")
	}
	if strings.Contains(payload.Summary, "available soon") {
		t.Fatalf("summary = %q, should not be placeholder", payload.Summary)
	}
	if !strings.Contains(payload.Summary, "manifest fix") {
		t.Fatalf("summary = %q, want release detail from note", payload.Summary)
	}
}

func TestQuickNoteEmailRouteReturnsDraft(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s, err := store.New(store.StoreConfig{Backend: "sqlite", SQLitePath: dbPath, SaveAudio: true, MaxAudioStorageMB: 100})
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	defer s.Close()

	id, err := s.SaveQuickNote(
		context.Background(),
		"Prepare Android release checklist. Verify assistant wiring. Share the rollout plan with QA and support.",
		"de",
		"manual",
		0,
		0,
		nil,
	)
	if err != nil {
		t.Fatalf("store.SaveQuickNote: %v", err)
	}

	cfg := defaultTestConfig()
	state := &appState{}
	handler := assetHandler(cfg, filepath.Join(t.TempDir(), "config.toml"), state, &router.Router{}, s, &config.InstallState{Mode: config.InstallModeLocal})

	form := url.Values{"id": {strconv.FormatInt(id, 10)}}
	req := httptest.NewRequest(http.MethodPost, "/quicknotes/email", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var payload struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Email == "" {
		t.Fatal("expected email draft")
	}
	if strings.Contains(payload.Email, "available soon") {
		t.Fatalf("email = %q, should not be placeholder", payload.Email)
	}
	if !strings.Contains(payload.Email, "Betreff:") {
		t.Fatalf("email = %q, want subject line", payload.Email)
	}
	if !strings.Contains(payload.Email, "Android release checklist") {
		t.Fatalf("email = %q, want note context in draft", payload.Email)
	}
}

func TestEscapeJS(t *testing.T) {
	tests := []struct {
		name  string
		input string
		check func(t *testing.T, result string)
	}{
		{
			name:  "normal string",
			input: "hello",
			check: func(t *testing.T, result string) {
				if result != "hello" {
					t.Fatalf("escapeJS(%q) = %q, want %q", "hello", result, "hello")
				}
			},
		},
		{
			name:  "string with quotes",
			input: `he said "hi"`,
			check: func(t *testing.T, result string) {
				if !strings.Contains(result, `\"`) {
					t.Fatalf("escapeJS(%q) = %q, want escaped quotes", `he said "hi"`, result)
				}
			},
		},
		{
			name:  "string with backslash",
			input: `path\to\file`,
			check: func(t *testing.T, result string) {
				if !strings.Contains(result, `\\`) {
					t.Fatalf("escapeJS(%q) = %q, want escaped backslashes", `path\to\file`, result)
				}
			},
		},
		{
			name:  "empty string",
			input: "",
			check: func(t *testing.T, result string) {
				if result != "" {
					t.Fatalf("escapeJS(%q) = %q, want empty", "", result)
				}
			},
		},
		{
			name:  "string with newlines",
			input: "line1\nline2",
			check: func(t *testing.T, result string) {
				if !strings.Contains(result, `\n`) {
					t.Fatalf("escapeJS(%q) = %q, want escaped newline", "line1\nline2", result)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.check(t, escapeJS(tc.input))
		})
	}
}

func TestOverlayPhase(t *testing.T) {
	tests := []struct {
		state string
		level float64
		want  string
	}{
		{"recording", 0.5, "speaking"},
		{"recording", 0.01, "listening"},
		{"processing", 0, "thinking"},
		{"done", 0, "done"},
		{"idle", 0, "idle"},
		{"", 0, "idle"},
	}

	for _, tc := range tests {
		t.Run(fmt.Sprintf("%s_%.2f", tc.state, tc.level), func(t *testing.T) {
			got := overlayPhase(tc.state, tc.level)
			if got != tc.want {
				t.Fatalf("overlayPhase(%q, %.2f) = %q, want %q", tc.state, tc.level, got, tc.want)
			}
		})
	}
}

func TestNormalizeOverlayLevel(t *testing.T) {
	tests := []struct {
		name  string
		input float64
		check func(t *testing.T, result float64)
	}{
		{
			name:  "zero",
			input: 0,
			check: func(t *testing.T, result float64) {
				if result != 0 {
					t.Fatalf("normalizeOverlayLevel(0) = %f, want 0", result)
				}
			},
		},
		{
			name:  "negative",
			input: -0.5,
			check: func(t *testing.T, result float64) {
				if result != 0 {
					t.Fatalf("normalizeOverlayLevel(-0.5) = %f, want 0", result)
				}
			},
		},
		{
			name:  "very small positive",
			input: 0.001,
			check: func(t *testing.T, result float64) {
				if result <= 0 {
					t.Fatalf("normalizeOverlayLevel(0.001) = %f, want > 0 (boosted above floor)", result)
				}
			},
		},
		{
			name:  "max",
			input: 1.0,
			check: func(t *testing.T, result float64) {
				if result > 1.0 {
					t.Fatalf("normalizeOverlayLevel(1.0) = %f, want <= 1.0", result)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.check(t, normalizeOverlayLevel(tc.input))
		})
	}
}

func TestAddLogCapsBufAt200(t *testing.T) {
	state := &appState{}
	for i := 0; i < 210; i++ {
		state.addLog(fmt.Sprintf("entry %d", i), "info")
	}

	state.mu.Lock()
	n := len(state.logEntries)
	state.mu.Unlock()

	if n != 200 {
		t.Fatalf("log entries = %d, want 200", n)
	}
}

// ---------------------------------------------------------------------------
// Features & Auth endpoint tests
// ---------------------------------------------------------------------------

func TestFeaturesEndpoint_LocalMode(t *testing.T) {
	cfg := defaultTestConfig()
	handler := assetHandler(cfg, filepath.Join(t.TempDir(), "config.toml"), &appState{}, &router.Router{}, nil, &config.InstallState{Mode: config.InstallModeLocal})

	req := httptest.NewRequest(http.MethodGet, "/features", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload["cloudMode"] != false {
		t.Fatalf("cloudMode = %v, want false", payload["cloudMode"])
	}
	if payload["installMode"] != "local" {
		t.Fatalf("installMode = %v, want %q", payload["installMode"], "local")
	}
}

func TestFeaturesEndpoint_CloudMode(t *testing.T) {
	cfg := defaultTestConfig()
	handler := assetHandler(cfg, filepath.Join(t.TempDir(), "config.toml"), &appState{}, &router.Router{}, nil, &config.InstallState{Mode: config.InstallModeCloud})

	req := httptest.NewRequest(http.MethodGet, "/features", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload["cloudMode"] != true {
		t.Fatalf("cloudMode = %v, want true", payload["cloudMode"])
	}
	if payload["installMode"] != "cloud" {
		t.Fatalf("installMode = %v, want %q", payload["installMode"], "cloud")
	}
}

func TestAuthStatusEndpoint_NoProvider(t *testing.T) {
	cfg := defaultTestConfig()
	handler := assetHandler(cfg, filepath.Join(t.TempDir(), "config.toml"), &appState{}, &router.Router{}, nil, &config.InstallState{Mode: config.InstallModeLocal})

	req := httptest.NewRequest(http.MethodGet, "/auth/status", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload["available"] != false {
		t.Fatalf("available = %v, want false", payload["available"])
	}
	if payload["loggedIn"] != false {
		t.Fatalf("loggedIn = %v, want false", payload["loggedIn"])
	}
}

func TestAuthLoginEndpoint_NoProvider(t *testing.T) {
	cfg := defaultTestConfig()
	handler := assetHandler(cfg, filepath.Join(t.TempDir(), "config.toml"), &appState{}, &router.Router{}, nil, &config.InstallState{Mode: config.InstallModeLocal})

	req := httptest.NewRequest(http.MethodPost, "/auth/login", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var payload map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload["error"] == "" {
		t.Fatal("expected error message about auth not available")
	}
	if !strings.Contains(payload["error"], "not available") {
		t.Fatalf("error = %q, want message about auth not available", payload["error"])
	}
}

func TestAuthLogoutEndpoint(t *testing.T) {
	cfg := defaultTestConfig()
	handler := assetHandler(cfg, filepath.Join(t.TempDir(), "config.toml"), &appState{}, &router.Router{}, nil, &config.InstallState{Mode: config.InstallModeLocal})

	req := httptest.NewRequest(http.MethodPost, "/auth/logout", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var payload map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload["message"] != "Logged out" {
		t.Fatalf("message = %q, want %q", payload["message"], "Logged out")
	}
}
