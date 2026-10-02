package config

import (
	framework "github.com/kombifyio/SpeechKit/pkg/speechkit"
)

// NormalizeModeSelection trims whitespace from both profile IDs and drops the
// fallback if it is identical to the primary.
func NormalizeModeSelection(selection ModeModelSelection) ModeModelSelection {
	selection.PrimaryProfileID = framework.NormalizeProviderProfileID(selection.PrimaryProfileID)
	selection.FallbackProfileID = framework.NormalizeProviderProfileID(selection.FallbackProfileID)
	if selection.PrimaryProfileID != "" && selection.PrimaryProfileID == selection.FallbackProfileID {
		selection.FallbackProfileID = ""
	}
	return selection
}

// CloneOverlayMonitorPositions returns an independent copy of the per-monitor
// overlay positions; nil and empty inputs yield an empty, non-nil map.
func CloneOverlayMonitorPositions(input map[string]OverlayFreePosition) map[string]OverlayFreePosition {
	if len(input) == 0 {
		return map[string]OverlayFreePosition{}
	}
	cloned := make(map[string]OverlayFreePosition, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}
