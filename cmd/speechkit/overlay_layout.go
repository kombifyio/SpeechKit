package main

import (
	"fmt"
	"math"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/kombifyio/SpeechKit/internal/config"
)

// This file contains pure helpers for overlay window construction, geometry,
// and visual-phase math. None of these touch appState — they are safe to call
// from tests without any runtime wiring.

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
	return newOverlayHostWindowOptions("/pill-anchor.html", pillAnchorMetrics)
}

func newPillPanelWindowOptions() application.WebviewWindowOptions {
	return newOverlayHostWindowOptions("/pill-panel.html", pillPanelMetrics)
}

func newDotAnchorWindowOptions() application.WebviewWindowOptions {
	return newOverlayHostWindowOptions("/dot-anchor.html", dotAnchorMetrics)
}

func newRadialMenuWindowOptions() application.WebviewWindowOptions {
	return newOverlayHostWindowOptions("/dot-radial.html", radialMenuMetrics)
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

func newPrompterWindowOptions() application.WebviewWindowOptions {
	return application.WebviewWindowOptions{
		Title:            "SpeechKit Conversation",
		Width:            prompterWidth,
		Height:           prompterHeight,
		MinWidth:         300,
		MinHeight:        112,
		DisableResize:    false,
		Frameless:        true,
		AlwaysOnTop:      true,
		Hidden:           true,
		BackgroundType:   application.BackgroundTypeTranslucent,
		URL:              "/voiceagent-prompter.html",
		BackgroundColour: application.NewRGBA(12, 18, 27, 236),
		Windows: application.WindowsWindow{
			HiddenOnTaskbar: false,
			Theme:           application.Dark,
			BackdropType:    application.Acrylic,
			DisableIcon:     true,
		},
	}
}

func prompterPosition(bounds screenBounds) (int, int) {
	x := bounds.X + bounds.Width - prompterWidth - 20
	y := bounds.Y + bounds.Height - prompterHeight - 60
	if minX := bounds.X + 20; x < minX {
		x = minX
	}
	if minY := bounds.Y + 20; y < minY {
		y = minY
	}
	return x, y
}

func newOverlayHostWindowOptions(url string, metrics overlayHostMetrics) application.WebviewWindowOptions {
	return application.WebviewWindowOptions{
		Title:          "",
		Width:          metrics.Width,
		Height:         metrics.Height,
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
	position = normalizeOverlayPosition(position)
	half := overlayWindowSize / 2

	if visualizer == "circle" {
		switch position {
		case "left":
			cx := bounds.X + overlayEdgeMargin + dotBubbleW/2
			cy := bounds.Y + bounds.Height/2
			return cx - half, cy - half
		case "right":
			cx := bounds.X + bounds.Width - overlayEdgeMargin - dotBubbleW/2
			cy := bounds.Y + bounds.Height/2
			return cx - half, cy - half
		case "top":
			cx := bounds.X + bounds.Width/2
			cy := bounds.Y + overlayEdgeMargin + dotBubbleH/2
			return cx - half, cy - half
		default:
			cx := bounds.X + bounds.Width/2
			cy := bounds.Y + bounds.Height - overlayEdgeMargin - dotBubbleH/2
			return cx - half, cy - half
		}
	}

	switch position {
	case "left":
		x := bounds.X
		y := bounds.Y + (bounds.Height-overlayWindowSize)/2
		return x, y
	case "right":
		x := bounds.X + bounds.Width - overlayWindowSize
		y := bounds.Y + (bounds.Height-overlayWindowSize)/2
		return x, y
	case "top":
		x := bounds.X + (bounds.Width-overlayWindowSize)/2
		y := bounds.Y
		return x, y
	default:
		x := bounds.X + (bounds.Width-overlayWindowSize)/2
		y := bounds.Y + bounds.Height - overlayWindowSize
		return x, y
	}
}

// Legacy helper retained for compatibility with older tests.
func computeBubbleRegion(wx, wy int, position, visualizer string) bubbleRegion {
	position = normalizeOverlayPosition(position)
	if visualizer == "circle" {
		return bubbleRegion{
			X: wx + (overlayWindowSize-dotBubbleW)/2,
			Y: wy + (overlayWindowSize-dotBubbleH)/2,
			W: dotBubbleW, H: dotBubbleH,
		}
	}

	bw, bh := pillBubbleW, pillBubbleH
	switch position {
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
	case "top":
		return bubbleRegion{
			X: wx + (overlayWindowSize-bw)/2,
			Y: wy + overlayEdgeMargin,
			W: bw, H: bh,
		}
	default:
		return bubbleRegion{
			X: wx + (overlayWindowSize-bw)/2,
			Y: wy + overlayWindowSize - bh - overlayEdgeMargin,
			W: bw, H: bh,
		}
	}
}

func overlayAnchoredPosition(bounds screenBounds, position string, metrics overlayHostMetrics) (int, int) {
	position = normalizeOverlayPosition(position)
	visibleW, visibleH := overlayVisibleMetrics(metrics)
	centerX := bounds.X + bounds.Width/2
	centerY := bounds.Y + bounds.Height/2

	switch position {
	case "left":
		centerX = bounds.X + overlayEdgeMargin + visibleW/2
	case "right":
		centerX = bounds.X + bounds.Width - overlayEdgeMargin - visibleW/2
	case "top":
		centerY = bounds.Y + overlayEdgeMargin + visibleH/2
	default:
		centerY = bounds.Y + bounds.Height - overlayEdgeMargin - visibleH/2
	}

	return centerX - metrics.Width/2, centerY - metrics.Height/2
}

func normalizeOverlayPosition(position string) string {
	switch strings.TrimSpace(position) {
	case "top", "bottom", "left", "right":
		return strings.TrimSpace(position)
	default:
		return "bottom"
	}
}

func overlayVisibleMetrics(metrics overlayHostMetrics) (int, int) {
	switch metrics {
	case pillAnchorMetrics, pillPanelMetrics:
		return pillBubbleW, pillBubbleH
	case dotAnchorMetrics:
		return dotBubbleW, dotBubbleH
	default:
		return metrics.Width, metrics.Height
	}
}

func pillAnchorPosition(bounds screenBounds, position string) (int, int) {
	return overlayAnchoredPosition(bounds, position, pillAnchorMetrics)
}

func dotAnchorPosition(bounds screenBounds, position string) (int, int) {
	return overlayAnchoredPosition(bounds, position, dotAnchorMetrics)
}

func radialMenuPosition(bounds screenBounds, position string) (int, int) {
	anchorX, anchorY := dotAnchorPosition(bounds, position)
	return anchorX + dotAnchorMetrics.Width/2 - radialMenuMetrics.Width/2, anchorY + dotAnchorMetrics.Height/2 - radialMenuMetrics.Height/2
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
		return x + dotAnchorMetrics.Width/2, y + dotAnchorMetrics.Height/2
	}
	x, y := pillAnchorPosition(bounds, position)
	return x + pillAnchorMetrics.Width/2, y + pillAnchorMetrics.Height/2
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

func overlayMonitorKey(bounds screenBounds) string {
	return fmt.Sprintf("%d,%d,%d,%d", bounds.X, bounds.Y, bounds.Width, bounds.Height)
}

func parseOverlayMonitorKey(key string) (screenBounds, bool) {
	var bounds screenBounds
	if _, err := fmt.Sscanf(
		strings.TrimSpace(key),
		"%d,%d,%d,%d",
		&bounds.X,
		&bounds.Y,
		&bounds.Width,
		&bounds.Height,
	); err != nil {
		return screenBounds{}, false
	}
	if bounds.Width <= 0 || bounds.Height <= 0 {
		return screenBounds{}, false
	}
	return bounds, true
}

func cloneOverlayMonitorPositions(input map[string]config.OverlayFreePosition) map[string]config.OverlayFreePosition {
	if len(input) == 0 {
		return map[string]config.OverlayFreePosition{}
	}
	cloned := make(map[string]config.OverlayFreePosition, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}

func overlayCenterRange(bounds screenBounds, metrics overlayHostMetrics) (int, int, int, int) {
	halfW := metrics.Width / 2
	halfH := metrics.Height / 2
	return bounds.X + halfW, bounds.X + bounds.Width - halfW, bounds.Y + halfH, bounds.Y + bounds.Height - halfH
}

func overlayCenterRatios(bounds screenBounds, centerX, centerY int, metrics overlayHostMetrics) (float64, float64) {
	minX, maxX, minY, maxY := overlayCenterRange(bounds, metrics)
	clampedX := clampInt(centerX, minX, maxX)
	clampedY := clampInt(centerY, minY, maxY)

	ratioX := 0.5
	if maxX > minX {
		ratioX = float64(clampedX-minX) / float64(maxX-minX)
	}
	ratioY := 0.5
	if maxY > minY {
		ratioY = float64(clampedY-minY) / float64(maxY-minY)
	}

	return ratioX, ratioY
}

func overlayCenterFromRatios(bounds screenBounds, ratioX, ratioY float64, metrics overlayHostMetrics) (int, int) {
	minX, maxX, minY, maxY := overlayCenterRange(bounds, metrics)
	centerX := minX
	if maxX > minX {
		centerX = minX + int(math.Round(float64(maxX-minX)*math.Max(0, math.Min(1, ratioX))))
	}
	centerY := minY
	if maxY > minY {
		centerY = minY + int(math.Round(float64(maxY-minY)*math.Max(0, math.Min(1, ratioY))))
	}
	return centerX, centerY
}

func resolveOverlayReferenceMonitorBounds(centerX, centerY int, positions map[string]config.OverlayFreePosition) (screenBounds, bool) {
	for key, saved := range positions {
		if saved.X != centerX || saved.Y != centerY {
			continue
		}
		if bounds, ok := parseOverlayMonitorKey(key); ok {
			return bounds, true
		}
	}
	return screenBounds{}, false
}

func resolveOverlayFreeCenterForMonitor(bounds screenBounds, metrics overlayHostMetrics, visualizer, position string, centerX, centerY int, positions map[string]config.OverlayFreePosition) (string, int, int) {
	monitorKey := overlayMonitorKey(bounds)
	if centerX == 0 && centerY == 0 {
		resolvedX, resolvedY := defaultOverlayFreeCenter(bounds, visualizer, position)
		return monitorKey, resolvedX, resolvedY
	}
	referenceBounds, ok := resolveOverlayReferenceMonitorBounds(centerX, centerY, positions)
	if !ok {
		resolvedX, resolvedY := resolveOverlayFreeCenter(bounds, visualizer, position, centerX, centerY)
		return monitorKey, resolvedX, resolvedY
	}
	ratioX, ratioY := overlayCenterRatios(referenceBounds, centerX, centerY, metrics)
	resolvedX, resolvedY := overlayCenterFromRatios(bounds, ratioX, ratioY, metrics)
	return monitorKey, resolvedX, resolvedY
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

func setOverlayWindowFrame(window overlayWindow, x, y int, metrics overlayHostMetrics) {
	if window == nil {
		return
	}
	window.SetSize(metrics.Width, metrics.Height)
	window.SetPosition(x, y)
}

func positionAnchoredOverlayWindow(window overlayWindow, bounds screenBounds, position string, metrics overlayHostMetrics) {
	x, y := overlayAnchoredPosition(bounds, position, metrics)
	setOverlayWindowFrame(window, x, y, metrics)
}

func positionFreeOverlayWindow(window overlayWindow, bounds screenBounds, centerX, centerY int, metrics overlayHostMetrics) {
	x, y := overlayFreeWindowPosition(bounds, centerX, centerY, metrics.Width, metrics.Height)
	setOverlayWindowFrame(window, x, y, metrics)
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

func modeAvailabilityFromState(dictateEnabled, assistEnabled, voiceAgentEnabled bool, dictateHotkey, assistHotkey, voiceAgentHotkey string) (modeAvailabilitySnapshot, modeAvailabilitySnapshot) {
	modeEnabled := modeAvailabilitySnapshot{
		Dictate:    dictateEnabled,
		Assist:     assistEnabled,
		VoiceAgent: voiceAgentEnabled,
	}
	bindings := configuredModeBindings(dictateEnabled, assistEnabled, voiceAgentEnabled, dictateHotkey, assistHotkey, voiceAgentHotkey)
	availableModes := modeAvailabilitySnapshot{
		Dictate:    bindings[modeDictate] != "",
		Assist:     bindings[modeAssist] != "",
		VoiceAgent: bindings[modeVoiceAgent] != "",
	}
	return modeEnabled, availableModes
}
