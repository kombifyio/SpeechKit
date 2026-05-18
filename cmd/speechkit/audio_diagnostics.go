package main

import (
	"errors"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/audio"
)

// audioStartupDiagnostic classifies an audio.Open failure into a stable
// category string + an actionable user-facing guidance string so a
// support bundle log entry or a console message tells the operator
// immediately whether to ask the user about Windows microphone
// permissions, a missing USB device, an app holding the device, or a
// build/install issue. Recognised categories:
//
//   - unsupported_backend  — the configured [audio] backend isn't
//     compiled into this build.
//   - backend_unavailable  — no default backend in this build (typical
//     for a cgo-disabled cross-platform tool).
//   - permission_denied    — Windows microphone privacy gate blocks us.
//   - no_device            — no input device detected.
//   - device_busy          — another app holds the default device.
//   - unknown              — fallback when none of the patterns match;
//     guidance points the user at --collect-support-bundle.
//
// audioStartupDiagnostic must remain side-effect-free; callers compose
// the slog entry themselves so the surrounding bootstrap log shape can
// evolve without touching this helper.
func audioStartupDiagnostic(err error) (category, guidance string) {
	if err == nil {
		return "ok", ""
	}
	switch {
	case errors.Is(err, audio.ErrUnsupportedBackend):
		return "unsupported_backend", "The configured [audio] backend is not compiled into this build. Reinstall the matching SpeechKit edition — the Windows MinGW build is required for WASAPI capture."
	case errors.Is(err, audio.ErrBackendUnavailable):
		return "backend_unavailable", "No default audio backend is available in this build. Set [audio] backend explicitly in config.toml or reinstall a build that includes a WASAPI backend."
	}

	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "permission"),
		strings.Contains(msg, "access denied"),
		strings.Contains(msg, "access is denied"):
		return "permission_denied", "Windows is blocking microphone access. Open Settings -> Privacy & security -> Microphone and confirm 'Microphone access' is on for both system and apps; ensure 'kombify SpeechKit' is allowed."
	case strings.Contains(msg, "no device"),
		strings.Contains(msg, "no input"),
		strings.Contains(msg, "device not found"),
		strings.Contains(msg, "no default"):
		return "no_device", "No input audio device was detected. Plug in a microphone or open Settings -> System -> Sound -> Input and pick a working device, then restart SpeechKit."
	case strings.Contains(msg, "busy"),
		strings.Contains(msg, "in use"),
		strings.Contains(msg, "already initialized"):
		return "device_busy", "The default microphone is held in exclusive mode by another application. Close apps that capture audio (Zoom, Teams, OBS, browser tabs in a call, etc.) and restart SpeechKit."
	}

	return "unknown", "Run 'SpeechKit.exe --collect-support-bundle out.zip' and attach the resulting ZIP to a support ticket so we can attribute this failure."
}
