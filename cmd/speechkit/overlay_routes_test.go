package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/router"
)

func TestOverlayPillPanelRoutesSwapAnchorAndPanel(t *testing.T) {
	cfg := defaultTestConfig()
	pillAnchor := &fakeOverlayWindow{visible: true}
	pillPanel := &fakeOverlayWindow{}
	state := &appState{
		pillAnchor:        pillAnchor,
		pillPanel:         pillPanel,
		overlayEnabled:    true,
		overlayVisualizer: "pill",
		screenLocator:     &fakeScreenLocator{bounds: screenBounds{X: 0, Y: 0, Width: 1920, Height: 1080}, ok: true},
	}
	handler := assetHandler(cfg, filepath.Join(t.TempDir(), "config.toml"), state, &router.Router{}, nil, &config.InstallState{Mode: config.InstallModeLocal})

	showReq := httptest.NewRequest(http.MethodPost, "/overlay/pill-panel/show", http.NoBody)
	showRec := httptest.NewRecorder()
	handler.ServeHTTP(showRec, showReq)

	if showRec.Code != http.StatusOK {
		t.Fatalf("show status = %d, want %d", showRec.Code, http.StatusOK)
	}
	if pillAnchor.hideCalls == 0 {
		t.Fatal("pill anchor should hide when pill panel opens")
	}
	if pillPanel.showCalls == 0 {
		t.Fatal("pill panel should show when route is called")
	}

	hideReq := httptest.NewRequest(http.MethodPost, "/overlay/pill-panel/hide", http.NoBody)
	hideRec := httptest.NewRecorder()
	handler.ServeHTTP(hideRec, hideReq)

	if hideRec.Code != http.StatusOK {
		t.Fatalf("hide status = %d, want %d", hideRec.Code, http.StatusOK)
	}
	if pillPanel.hideCalls == 0 {
		t.Fatal("pill panel should hide when route is called")
	}
	if pillAnchor.showCalls == 0 {
		t.Fatal("pill anchor should reappear when pill panel closes")
	}
}

func TestOverlayRadialRoutesSwapDotAndMenu(t *testing.T) {
	cfg := defaultTestConfig()
	dotAnchor := &fakeOverlayWindow{visible: true}
	radialMenu := &fakeOverlayWindow{}
	state := &appState{
		dotAnchor:         dotAnchor,
		radialMenu:        radialMenu,
		overlayEnabled:    true,
		overlayVisualizer: "circle",
		screenLocator:     &fakeScreenLocator{bounds: screenBounds{X: 0, Y: 0, Width: 1920, Height: 1080}, ok: true},
	}
	handler := assetHandler(cfg, filepath.Join(t.TempDir(), "config.toml"), state, &router.Router{}, nil, &config.InstallState{Mode: config.InstallModeLocal})

	showReq := httptest.NewRequest(http.MethodPost, "/overlay/radial/show", http.NoBody)
	showRec := httptest.NewRecorder()
	handler.ServeHTTP(showRec, showReq)

	if showRec.Code != http.StatusOK {
		t.Fatalf("show status = %d, want %d", showRec.Code, http.StatusOK)
	}
	if dotAnchor.hideCalls == 0 {
		t.Fatal("dot anchor should hide when radial menu opens")
	}
	if radialMenu.showCalls == 0 {
		t.Fatal("radial menu should show when route is called")
	}

	hideReq := httptest.NewRequest(http.MethodPost, "/overlay/radial/hide", http.NoBody)
	hideRec := httptest.NewRecorder()
	handler.ServeHTTP(hideRec, hideReq)

	if hideRec.Code != http.StatusOK {
		t.Fatalf("hide status = %d, want %d", hideRec.Code, http.StatusOK)
	}
	if radialMenu.hideCalls == 0 {
		t.Fatal("radial menu should hide when route is called")
	}
	if dotAnchor.showCalls == 0 {
		t.Fatal("dot anchor should reappear when radial menu closes")
	}
}

func TestOverlayShowDashboardRouteRestoresWindowAndDispatchesRefreshEvent(t *testing.T) {
	cfg := defaultTestConfig()
	dashboard := &fakeSettingsWindow{visible: false}
	state := &appState{
		dashboard: dashboard,
		settings:  dashboard,
	}
	handler := assetHandler(cfg, filepath.Join(t.TempDir(), "config.toml"), state, &router.Router{}, nil, &config.InstallState{Mode: config.InstallModeLocal})

	req := httptest.NewRequest(http.MethodPost, "/overlay/show-dashboard", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if dashboard.showCalls != 1 {
		t.Fatalf("show calls = %d, want 1", dashboard.showCalls)
	}
	if dashboard.restoreCalls != 1 || dashboard.unMinimiseCalls != 1 || dashboard.focusCalls != 1 {
		t.Fatalf("restore=%d unminimise=%d focus=%d", dashboard.restoreCalls, dashboard.unMinimiseCalls, dashboard.focusCalls)
	}
	if len(dashboard.scripts) != 1 || dashboard.scripts[0] == "" {
		t.Fatalf("dashboard scripts = %v, want one refresh dispatch script", dashboard.scripts)
	}
	if want := "speechkit:dashboard-show"; !strings.Contains(dashboard.scripts[0], want) {
		t.Fatalf("dashboard refresh script = %q, want substring %q", dashboard.scripts[0], want)
	}
}

func TestOverlayFreeCenterRoutesUpdateRuntimeAndPersistSavedPosition(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.UI.OverlayMovable = true
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save initial config: %v", err)
	}

	pillAnchor := &fakeOverlayWindow{}
	pillPanel := &fakeOverlayWindow{}
	state := &appState{
		pillAnchor:            pillAnchor,
		pillPanel:             pillPanel,
		overlayEnabled:        true,
		overlayVisualizer:     "pill",
		overlayPosition:       "top",
		overlayMovable:        true,
		overlayMonitorKey:     overlayMonitorKey(screenBounds{X: 0, Y: 0, Width: 1920, Height: 1080}),
		screenLocator:         &fakeScreenLocator{bounds: screenBounds{X: 0, Y: 0, Width: 1920, Height: 1080}, ok: true},
		overlayMonitorCenters: map[string]config.OverlayFreePosition{},
	}
	handler := assetHandler(cfg, cfgPath, state, &router.Router{}, nil, &config.InstallState{Mode: config.InstallModeLocal})

	moveReq := httptest.NewRequest(http.MethodPost, "/overlay/free-center", strings.NewReader("center_x=700&center_y=420"))
	moveReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	moveRec := httptest.NewRecorder()
	handler.ServeHTTP(moveRec, moveReq)

	if moveRec.Code != http.StatusOK {
		t.Fatalf("move status = %d, want %d", moveRec.Code, http.StatusOK)
	}
	if state.overlayFreeX != 700 || state.overlayFreeY != 420 {
		t.Fatalf("runtime overlay free center = (%d,%d), want (700,420)", state.overlayFreeX, state.overlayFreeY)
	}
	if len(pillPanel.positions) == 0 {
		t.Fatal("pill panel should be repositioned when runtime move route is called")
	}

	saveReq := httptest.NewRequest(http.MethodPost, "/overlay/free-center/save", strings.NewReader("center_x=700&center_y=420"))
	saveReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	saveRec := httptest.NewRecorder()
	handler.ServeHTTP(saveRec, saveReq)

	if saveRec.Code != http.StatusOK {
		t.Fatalf("save status = %d, want %d", saveRec.Code, http.StatusOK)
	}

	reloaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if reloaded.UI.OverlayFreeX != 700 || reloaded.UI.OverlayFreeY != 420 {
		t.Fatalf("saved overlay free center = (%d,%d), want (700,420)", reloaded.UI.OverlayFreeX, reloaded.UI.OverlayFreeY)
	}
	if got := reloaded.UI.OverlayMonitorPositions[state.overlayMonitorKey]; got != (config.OverlayFreePosition{X: 700, Y: 420}) {
		t.Fatalf("saved monitor position = %+v", got)
	}
}
