//go:build linux

package httpx

import (
	"fmt"
	"net/http"
)

// WriteModeDisabled emits the canonical disabled-mode envelope used by the
// Server-Target when an endpoint exists but its owning mode is configured off.
func WriteModeDisabled(w http.ResponseWriter, mode string) {
	w.Header().Set("X-SpeechKit-Mode-State", "disabled")
	WriteErrorWithDetails(
		w,
		http.StatusServiceUnavailable,
		"mode_disabled",
		fmt.Sprintf("%s mode is disabled", mode),
		map[string]any{"mode": mode},
	)
}
