package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
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

	showReq := httptest.NewRequest(http.MethodPost, "/overlay/pill-panel/show", nil)
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

	hideReq := httptest.NewRequest(http.MethodPost, "/overlay/pill-panel/hide", nil)
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

	showReq := httptest.NewRequest(http.MethodPost, "/overlay/radial/show", nil)
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

	hideReq := httptest.NewRequest(http.MethodPost, "/overlay/radial/hide", nil)
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
