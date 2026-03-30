package main

import "testing"

func TestPillAnchorWindowOptions(t *testing.T) {
	opts := newPillAnchorWindowOptions()

	if !opts.AlwaysOnTop {
		t.Fatal("pill anchor must be always-on-top")
	}
	if !opts.Hidden {
		t.Fatal("pill anchor should start hidden")
	}
	if !opts.Frameless {
		t.Fatal("pill anchor should be frameless")
	}
	if opts.URL != "/pill-anchor.html" {
		t.Fatalf("pill anchor URL = %q", opts.URL)
	}
	if opts.Width != pillAnchorWidth || opts.Height != pillAnchorHeight {
		t.Fatalf("pill anchor size = %dx%d, want %dx%d", opts.Width, opts.Height, pillAnchorWidth, pillAnchorHeight)
	}
}

func TestPillPanelWindowOptions(t *testing.T) {
	opts := newPillPanelWindowOptions()

	if !opts.AlwaysOnTop {
		t.Fatal("pill panel must be always-on-top")
	}
	if !opts.Hidden {
		t.Fatal("pill panel should start hidden")
	}
	if !opts.Frameless {
		t.Fatal("pill panel should be frameless")
	}
	if opts.URL != "/pill-panel.html" {
		t.Fatalf("pill panel URL = %q", opts.URL)
	}
	if opts.Width != pillPanelWidth || opts.Height != pillPanelHeight {
		t.Fatalf("pill panel size = %dx%d, want %dx%d", opts.Width, opts.Height, pillPanelWidth, pillPanelHeight)
	}
}

func TestDotAnchorWindowOptions(t *testing.T) {
	opts := newDotAnchorWindowOptions()

	if !opts.AlwaysOnTop {
		t.Fatal("dot anchor must be always-on-top")
	}
	if !opts.Hidden {
		t.Fatal("dot anchor should start hidden")
	}
	if !opts.Frameless {
		t.Fatal("dot anchor should be frameless")
	}
	if opts.URL != "/dot-anchor.html" {
		t.Fatalf("dot anchor URL = %q", opts.URL)
	}
	if opts.Width != dotAnchorSize || opts.Height != dotAnchorSize {
		t.Fatalf("dot anchor size = %dx%d, want %dx%d", opts.Width, opts.Height, dotAnchorSize, dotAnchorSize)
	}
}

func TestRadialMenuWindowOptions(t *testing.T) {
	opts := newRadialMenuWindowOptions()

	if !opts.AlwaysOnTop {
		t.Fatal("radial menu must be always-on-top")
	}
	if !opts.Hidden {
		t.Fatal("radial menu should start hidden")
	}
	if !opts.Frameless {
		t.Fatal("radial menu should be frameless")
	}
	if opts.URL != "/dot-radial.html" {
		t.Fatalf("radial menu URL = %q", opts.URL)
	}
	if opts.Width != radialMenuSize || opts.Height != radialMenuSize {
		t.Fatalf("radial menu size = %dx%d, want %dx%d", opts.Width, opts.Height, radialMenuSize, radialMenuSize)
	}
}

func TestSetStateShowsPillAnchorForPillVisualizer(t *testing.T) {
	pillAnchor := &fakeOverlayWindow{}
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
		screenLocator:     &fakeScreenLocator{bounds: screenBounds{X: 0, Y: 0, Width: 1920, Height: 1080}, ok: true},
	}

	state.setState("idle", "")

	if pillAnchor.showCalls != 1 {
		t.Fatalf("pill anchor show calls = %d, want 1", pillAnchor.showCalls)
	}
	if pillPanel.hideCalls == 0 {
		t.Fatal("pill panel should be hidden when pill anchor is active")
	}
	if dotAnchor.hideCalls == 0 {
		t.Fatal("dot anchor should be hidden in pill mode")
	}
	if radialMenu.hideCalls == 0 {
		t.Fatal("radial menu should be hidden in pill mode")
	}
}

func TestSetStateShowsDotAnchorForCircleVisualizer(t *testing.T) {
	pillAnchor := &fakeOverlayWindow{}
	pillPanel := &fakeOverlayWindow{}
	dotAnchor := &fakeOverlayWindow{}
	radialMenu := &fakeOverlayWindow{}
	state := &appState{
		pillAnchor:        pillAnchor,
		pillPanel:         pillPanel,
		dotAnchor:         dotAnchor,
		radialMenu:        radialMenu,
		overlayEnabled:    true,
		overlayVisualizer: "circle",
		screenLocator:     &fakeScreenLocator{bounds: screenBounds{X: 0, Y: 0, Width: 1920, Height: 1080}, ok: true},
	}

	state.setState("idle", "")

	if dotAnchor.showCalls != 1 {
		t.Fatalf("dot anchor show calls = %d, want 1", dotAnchor.showCalls)
	}
	if pillAnchor.hideCalls == 0 {
		t.Fatal("pill anchor should be hidden in circle mode")
	}
	if pillPanel.hideCalls == 0 {
		t.Fatal("pill panel should be hidden in circle mode")
	}
	if radialMenu.hideCalls == 0 {
		t.Fatal("radial menu should be hidden until explicitly opened")
	}
}
