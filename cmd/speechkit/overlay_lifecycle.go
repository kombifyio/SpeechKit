package main

import (
	"fmt"
)

// This file contains appState methods that control show/hide of overlay
// window surfaces (pill, dot, radial, assist bubble) and their positioning
// on the active screen.

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
		referenceMetrics := pillPanelMetrics
		if runtime.overlayVisualizer == "circle" {
			referenceMetrics = dotAnchorMetrics
		}
		if runtime.overlayMovable {
			monitorKey, centerX, centerY := resolveOverlayFreeCenterForMonitor(bounds, referenceMetrics, runtime.overlayVisualizer, runtime.overlayPosition, runtime.overlayFreeX, runtime.overlayFreeY, runtime.overlayMonitorCenters)
			s.syncResolvedOverlayFreeCenter(monitorKey, centerX, centerY)
			positionFreeOverlayWindow(host.pillAnchor, bounds, centerX, centerY, pillAnchorMetrics)
			positionFreeOverlayWindow(host.pillPanel, bounds, centerX, centerY, pillPanelMetrics)
			positionFreeOverlayWindow(host.dotAnchor, bounds, centerX, centerY, dotAnchorMetrics)
			positionFreeOverlayWindow(host.radialMenu, bounds, centerX, centerY, radialMenuMetrics)
			return
		}

		positionAnchoredOverlayWindow(host.pillAnchor, bounds, runtime.overlayPosition, pillAnchorMetrics)
		positionAnchoredOverlayWindow(host.pillPanel, bounds, runtime.overlayPosition, pillPanelMetrics)
		positionAnchoredOverlayWindow(host.dotAnchor, bounds, runtime.overlayPosition, dotAnchorMetrics)
		if host.radialMenu != nil {
			x, y := radialMenuPosition(bounds, runtime.overlayPosition)
			setOverlayWindowFrame(host.radialMenu, x, y, radialMenuMetrics)
		}
		return
	}

	overlay := host.overlay
	if overlay == nil {
		return
	}
	if runtime.overlayMovable {
		monitorKey, centerX, centerY := resolveOverlayFreeCenterForMonitor(bounds, overlayWindowMetrics, runtime.overlayVisualizer, runtime.overlayPosition, runtime.overlayFreeX, runtime.overlayFreeY, runtime.overlayMonitorCenters)
		s.syncResolvedOverlayFreeCenter(monitorKey, centerX, centerY)
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
		hideWindow(host.overlay)
		active := activeOverlayAnchor(host, runtime.overlayVisualizer)

		if runtime.overlayVisualizer == "circle" {
			hideWindow(host.pillAnchor)
			showWindow(active)
			return
		}

		hideWindow(host.dotAnchor)
		showWindow(active)
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
	bubble.ExecJS(fmt.Sprintf(`if(window.__assistBubble){window.__assistBubble.show(%q)}`, escapedText))
}

func (s *appState) setOverlayEnabled(enabled bool) {
	s.mu.Lock()
	s.overlayEnabled = enabled
	s.syncSpeechKitSnapshotLocked()
	s.mu.Unlock()

	s.refreshOverlayWindows()
}

func (s *appState) refreshOverlayWindows() {
	if s == nil {
		return
	}

	s.mu.Lock()
	enabled := s.overlayEnabled
	s.mu.Unlock()

	if !enabled {
		s.hideAllOverlayWindows()
		return
	}

	s.showActiveOverlayWindow()
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
