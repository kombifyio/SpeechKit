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

func TestPillPanelWindowOptionsFitQuickControlsAndTriModeToggles(t *testing.T) {
	const (
		outerPaddingX      = 6
		sectionGap         = 4
		inlineGap          = 2
		controlButtonWidth = 24
		activePillWidth    = 64
	)

	leftControlsWidth := controlButtonWidth*3 + inlineGap*2
	rightControlsWidth := controlButtonWidth*3 + inlineGap*2
	minPanelWidth := leftControlsWidth + sectionGap + activePillWidth + sectionGap + rightControlsWidth + outerPaddingX*2

	if pillPanelWidth < minPanelWidth {
		t.Fatalf("pill panel width = %d, want at least %d to fit left quick controls plus tri-mode toggles", pillPanelWidth, minPanelWidth)
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

func TestPrompterWindowOptionsStartCompactForVoiceAgentSidePanel(t *testing.T) {
	opts := newPrompterWindowOptions()

	if opts.Width != prompterWidth || opts.Height != prompterHeight {
		t.Fatalf("prompter size = %dx%d, want %dx%d", opts.Width, opts.Height, prompterWidth, prompterHeight)
	}
	if opts.Width > 400 {
		t.Fatalf("prompter width = %d, want compact side panel <= 400", opts.Width)
	}
	if opts.Height > 520 {
		t.Fatalf("prompter height = %d, want compact side panel <= 520", opts.Height)
	}
	if opts.MinWidth > 420 {
		t.Fatalf("prompter min width = %d, want resizable compact width <= 420", opts.MinWidth)
	}
	if opts.MinHeight > 420 {
		t.Fatalf("prompter min height = %d, want compact minimum height <= 420", opts.MinHeight)
	}
	if opts.MinWidth > prompterCollapsedWidth {
		t.Fatalf("prompter min width = %d, want <= collapsed width %d", opts.MinWidth, prompterCollapsedWidth)
	}
	if opts.MinHeight > prompterCollapsedHeight {
		t.Fatalf("prompter min height = %d, want <= collapsed height %d", opts.MinHeight, prompterCollapsedHeight)
	}
	if !opts.Frameless {
		t.Fatal("prompter should be frameless for shared custom chrome")
	}
}

func TestPrompterPositionKeepsWindowInsideVisibleScreenArea(t *testing.T) {
	x, y := prompterPosition(screenBounds{X: 0, Y: 0, Width: 800, Height: 600})

	if x < 20 {
		t.Fatalf("prompter x = %d, want at least 20", x)
	}
	if y < 20 {
		t.Fatalf("prompter y = %d, want at least 20", y)
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

func TestPositionOverlayAppliesDedicatedHostMetricsForAnchoredSurfaces(t *testing.T) {
	locator := &fakeScreenLocator{bounds: screenBounds{X: 100, Y: 50, Width: 1600, Height: 900}, ok: true}
	pillAnchor := &fakeOverlayWindow{}
	pillPanel := &fakeOverlayWindow{}
	dotAnchor := &fakeOverlayWindow{}
	radialMenu := &fakeOverlayWindow{}
	state := &appState{
		pillAnchor:        pillAnchor,
		pillPanel:         pillPanel,
		dotAnchor:         dotAnchor,
		radialMenu:        radialMenu,
		screenLocator:     locator,
		overlayVisualizer: "pill",
		overlayPosition:   "right",
	}

	state.positionOverlay()

	if got := pillAnchor.sizes; len(got) != 1 || got[0] != [2]int{pillAnchorWidth, pillAnchorHeight} {
		t.Fatalf("pill anchor sizes = %v, want [[%d %d]]", got, pillAnchorWidth, pillAnchorHeight)
	}
	if got := pillPanel.sizes; len(got) != 1 || got[0] != [2]int{pillPanelWidth, pillPanelHeight} {
		t.Fatalf("pill panel sizes = %v, want [[%d %d]]", got, pillPanelWidth, pillPanelHeight)
	}
	if got := dotAnchor.sizes; len(got) != 1 || got[0] != [2]int{dotAnchorSize, dotAnchorSize} {
		t.Fatalf("dot anchor sizes = %v, want [[%d %d]]", got, dotAnchorSize, dotAnchorSize)
	}
	if got := radialMenu.sizes; len(got) != 1 || got[0] != [2]int{radialMenuSize, radialMenuSize} {
		t.Fatalf("radial menu sizes = %v, want [[%d %d]]", got, radialMenuSize, radialMenuSize)
	}

	wantPillAnchor := [2]int{100 + 1600 - pillAnchorWidth - overlayEdgeMargin, 50 + (900-pillAnchorHeight)/2}
	if got := pillAnchor.positions; len(got) != 1 || got[0] != wantPillAnchor {
		t.Fatalf("pill anchor positions = %v, want [%v]", got, wantPillAnchor)
	}
	wantPillPanel := [2]int{100 + 1600 - pillPanelWidth - overlayEdgeMargin, 50 + (900-pillPanelHeight)/2}
	if got := pillPanel.positions; len(got) != 1 || got[0] != wantPillPanel {
		t.Fatalf("pill panel positions = %v, want [%v]", got, wantPillPanel)
	}
	wantDotAnchor := [2]int{100 + 1600 - dotAnchorSize - overlayEdgeMargin, 50 + (900-dotAnchorSize)/2}
	if got := dotAnchor.positions; len(got) != 1 || got[0] != wantDotAnchor {
		t.Fatalf("dot anchor positions = %v, want [%v]", got, wantDotAnchor)
	}
	wantRadial := [2]int{
		wantDotAnchor[0] + dotAnchorSize/2 - radialMenuSize/2,
		wantDotAnchor[1] + dotAnchorSize/2 - radialMenuSize/2,
	}
	if got := radialMenu.positions; len(got) != 1 || got[0] != wantRadial {
		t.Fatalf("radial menu positions = %v, want [%v]", got, wantRadial)
	}
}

func TestShowActiveOverlayWindowHidesInactiveDedicatedWindows(t *testing.T) {
	tests := []struct {
		name               string
		visualizer         string
		wantPillAnchorShow int
		wantDotAnchorShow  int
	}{
		{
			name:               "pill",
			visualizer:         "pill",
			wantPillAnchorShow: 1,
			wantDotAnchorShow:  0,
		},
		{
			name:               "circle",
			visualizer:         "circle",
			wantPillAnchorShow: 0,
			wantDotAnchorShow:  1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			legacy := &fakeOverlayWindow{visible: true}
			pillAnchor := &fakeOverlayWindow{}
			pillPanel := &fakeOverlayWindow{visible: true}
			dotAnchor := &fakeOverlayWindow{}
			radialMenu := &fakeOverlayWindow{visible: true}
			state := &appState{
				overlay:           legacy,
				pillAnchor:        pillAnchor,
				pillPanel:         pillPanel,
				dotAnchor:         dotAnchor,
				radialMenu:        radialMenu,
				overlayEnabled:    true,
				overlayVisualizer: tc.visualizer,
				screenLocator:     &fakeScreenLocator{bounds: screenBounds{X: 0, Y: 0, Width: 1920, Height: 1080}, ok: true},
			}

			state.showActiveOverlayWindow()

			if legacy.hideCalls == 0 {
				t.Fatal("legacy overlay should be hidden when dedicated windows are available")
			}
			if pillPanel.hideCalls == 0 {
				t.Fatal("pill panel should be hidden before returning to the anchor")
			}
			if radialMenu.hideCalls == 0 {
				t.Fatal("radial menu should be hidden before returning to the anchor")
			}
			if pillAnchor.showCalls != tc.wantPillAnchorShow {
				t.Fatalf("pill anchor show calls = %d, want %d", pillAnchor.showCalls, tc.wantPillAnchorShow)
			}
			if dotAnchor.showCalls != tc.wantDotAnchorShow {
				t.Fatalf("dot anchor show calls = %d, want %d", dotAnchor.showCalls, tc.wantDotAnchorShow)
			}
			if tc.visualizer == "pill" && dotAnchor.hideCalls == 0 {
				t.Fatal("dot anchor should be hidden in pill mode")
			}
			if tc.visualizer == "circle" && pillAnchor.hideCalls == 0 {
				t.Fatal("pill anchor should be hidden in circle mode")
			}
		})
	}
}
