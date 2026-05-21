package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/kombifyio/SpeechKit/internal/audio"
	"github.com/kombifyio/SpeechKit/internal/config"
)

// registerWakewordRoutes wires the wake-word self-test endpoint. The
// self-test records cfg.Wakeword.SelfTestDurationMs (default 3000) of
// audio through the configured capture device, computes level + score
// statistics, and returns a JSON report the settings UI can render.
//
// It does NOT exercise the actual sidecar — the sidecar runs in a
// separate process and a real-time score event is the better channel for
// that. The self-test answers a different, simpler question: "is my
// microphone actually capturing audio at a usable level?". That has been
// the silent first cause of every wake-word-doesn't-fire report we've
// seen, so making it one click to verify is high-value.
func registerWakewordRoutes(mux *http.ServeMux, cfg *config.Config, state *appState) {
	mux.HandleFunc("/api/wakeword/selftest", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		report := runWakewordSelfTest(cfg, state, wakewordSelfTestDefaultDuration)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(report)
	})
}

// wakewordSelfTestDefaultDuration is the recording window for /selftest.
// Three seconds is long enough to say "Hey <phrase>" comfortably and
// short enough to keep the click-to-result loop tight. The frontend can
// override via the duration_ms query parameter once we surface it.
const wakewordSelfTestDefaultDuration = 3 * time.Second

// WakewordSelfTestReport is the response payload of /api/wakeword/selftest.
// Keys are snake_case to match the rest of the control-plane API surface
// that the SvelteKit/React frontends already consume.
type WakewordSelfTestReport struct {
	OK            bool    `json:"ok"`
	Error         string  `json:"error,omitempty"`
	DurationMs    int64   `json:"duration_ms"`
	CapturedBytes int     `json:"captured_bytes"`
	SampleRate    int     `json:"sample_rate"`
	Channels      int     `json:"channels"`
	PeakLevel     float64 `json:"peak_level"`
	RMSLevel      float64 `json:"rms_level"`
	RequestedDID  string  `json:"requested_device_id"`
	ResolvedDID   string  `json:"resolved_device_id"`
	ResolvedDName string  `json:"resolved_device_name"`
	DeviceKind    string  `json:"device_kind"` // "requested" | "default" | "default-fallback"
	Backend       string  `json:"backend"`
	Phrase        string  `json:"phrase"`
	PhraseID      string  `json:"phrase_id"`
	EffectiveThr  float32 `json:"effective_threshold"`
	Advice        string  `json:"advice"`
	HeartbeatHint string  `json:"heartbeat_hint,omitempty"`
}

// runWakewordSelfTest executes the recording + stats computation. Extracted
// from the HTTP handler so tests can exercise the logic without spinning up
// a real mux.
func runWakewordSelfTest(cfg *config.Config, state *appState, duration time.Duration) WakewordSelfTestReport {
	_ = state // reserved for future status-snapshot integration (e.g. include current heartbeat in the report)
	if cfg == nil {
		return WakewordSelfTestReport{OK: false, Error: "config unavailable"}
	}
	if duration <= 0 {
		duration = wakewordSelfTestDefaultDuration
	}

	backend := config.NormalizeWakewordBackend(cfg.Wakeword.Backend)
	rep := WakewordSelfTestReport{
		Backend:      backend,
		Phrase:       strings.TrimSpace(cfg.Wakeword.Phrase),
		PhraseID:     strings.TrimSpace(cfg.Wakeword.PhraseID),
		EffectiveThr: effectiveWakewordThreshold(cfg, backend),
		SampleRate:   audio.SampleRate,
		Channels:     audio.Channels,
		RequestedDID: cfg.Audio.DeviceID,
	}

	// Resolve device name via enumeration so the user can confirm we
	// opened the mic they expect.
	devices, listErr := audio.ListCaptureDevices(audio.Config{Backend: audio.Backend(cfg.Audio.Backend)})
	if listErr != nil {
		slog.Warn("wakeword selftest: device enumeration failed", "err", listErr)
		rep.DeviceKind = "enumeration-failed"
		rep.ResolvedDName = "(enumeration failed)"
	} else {
		matched := false
		req := strings.TrimSpace(cfg.Audio.DeviceID)
		for _, d := range devices {
			if req != "" && strings.EqualFold(strings.TrimSpace(d.ID), req) {
				rep.ResolvedDID = d.ID
				rep.ResolvedDName = d.Name
				rep.DeviceKind = "requested"
				matched = true
				break
			}
		}
		if !matched {
			rep.DeviceKind = "default"
			if req != "" {
				rep.DeviceKind = "default-fallback"
			}
			for _, d := range devices {
				if d.IsDefault {
					rep.ResolvedDID = d.ID
					rep.ResolvedDName = d.Name
					break
				}
			}
			if rep.ResolvedDName == "" {
				rep.ResolvedDName = "(system default — none flagged)"
			}
		}
	}

	session, err := audio.Open(audio.Config{
		Backend:     audio.Backend(cfg.Audio.Backend),
		DeviceID:    cfg.Audio.DeviceID,
		SampleRate:  audio.SampleRate,
		Channels:    audio.Channels,
		FrameSizeMs: 32,
		LatencyHint: "interactive",
	})
	if err != nil {
		rep.OK = false
		rep.Error = fmt.Sprintf("audio.Open failed: %v", err)
		rep.Advice = "Audio backend refused to open. Check that the selected microphone is plugged in and that no other app holds exclusive access."
		return rep
	}
	defer session.Close() //nolint:errcheck // best-effort cleanup; the report has already been built

	var (
		mu         sync.Mutex
		buf        []byte
		peak       float64
		runningSum float64
		samples    int64
	)
	session.SetPCMHandler(func(pcm []byte) {
		mu.Lock()
		buf = append(buf, pcm...)
		level := audio.PCMLevel(pcm)
		if level > peak {
			peak = level
		}
		// PCMLevel is already a normalised 0-1 abs-mean; aggregate over the
		// session by weighting by frame size so the final RMS reflects the
		// whole recording, not just the loudest moment.
		runningSum += level * float64(len(pcm))
		samples += int64(len(pcm))
		mu.Unlock()
	})
	if err := session.Start(); err != nil {
		rep.OK = false
		rep.Error = fmt.Sprintf("session.Start failed: %v", err)
		rep.Advice = "Mic device opened but failed to start. Try closing other apps that use the microphone, then retry."
		return rep
	}
	startedAt := time.Now()
	time.Sleep(duration)
	rep.DurationMs = time.Since(startedAt).Milliseconds()

	captured, stopErr := session.Stop()
	if stopErr != nil {
		slog.Warn("wakeword selftest: session.Stop reported error", "err", stopErr)
	}
	if len(captured) == 0 {
		mu.Lock()
		captured = append([]byte(nil), buf...)
		mu.Unlock()
	}

	mu.Lock()
	rep.CapturedBytes = len(captured)
	rep.PeakLevel = peak
	if samples > 0 {
		rep.RMSLevel = runningSum / float64(samples)
	}
	mu.Unlock()

	rep.OK = rep.CapturedBytes > 0
	rep.Advice = wakewordSelfTestAdvice(rep)
	return rep
}

// wakewordSelfTestAdvice converts the raw report into one actionable
// sentence the settings UI can show as a banner. The wording is tuned for
// the kombify SpeechKit user base — direct, no jargon, no "perhaps".
func wakewordSelfTestAdvice(rep WakewordSelfTestReport) string {
	if rep.CapturedBytes == 0 {
		return "No audio captured. The microphone is silent — check Windows mic privacy settings and that the right device is selected in SpeechKit's Audio panel."
	}
	if rep.DeviceKind == "default-fallback" {
		return fmt.Sprintf("Captured audio (peak %.2f), BUT your saved mic ID didn't match any current device — we fell back to \"%s\". Re-select your microphone in the Audio panel.", rep.PeakLevel, rep.ResolvedDName)
	}
	if rep.PeakLevel < 0.02 {
		return fmt.Sprintf("Captured audio but level is very low (peak %.3f). Speak closer to \"%s\" or raise the input gain in Windows Sound settings.", rep.PeakLevel, rep.ResolvedDName)
	}
	if rep.PeakLevel < 0.10 {
		return fmt.Sprintf("Captured audio at modest level (peak %.2f) from \"%s\". The wake detector should hear you — try saying the phrase clearly during the test window. If it still doesn't fire, enable Wake-word debug mode to surface raw scores.", rep.PeakLevel, rep.ResolvedDName)
	}
	return fmt.Sprintf("Mic OK: captured %d bytes (peak %.2f, RMS %.3f) from \"%s\". If the wake phrase still doesn't fire, enable Wake-word debug mode in Settings to surface detector scores per frame.", rep.CapturedBytes, rep.PeakLevel, rep.RMSLevel, rep.ResolvedDName)
}
