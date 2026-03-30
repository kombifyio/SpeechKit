package main

import "testing"

func TestDesktopHostStateLockedSnapshotsOverlayWindows(t *testing.T) {
	pillAnchor := &fakeOverlayWindow{}
	pillPanel := &fakeOverlayWindow{}
	dotAnchor := &fakeOverlayWindow{}
	radialMenu := &fakeOverlayWindow{}

	state := &appState{
		pillAnchor: pillAnchor,
		pillPanel:  pillPanel,
		dotAnchor:  dotAnchor,
		radialMenu: radialMenu,
	}

	host := state.desktopHostStateLocked()

	if host.pillAnchor != pillAnchor {
		t.Fatal("pillAnchor snapshot missing")
	}
	if host.pillPanel != pillPanel {
		t.Fatal("pillPanel snapshot missing")
	}
	if host.dotAnchor != dotAnchor {
		t.Fatal("dotAnchor snapshot missing")
	}
	if host.radialMenu != radialMenu {
		t.Fatal("radialMenu snapshot missing")
	}
}
