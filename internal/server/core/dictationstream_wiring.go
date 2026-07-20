//go:build linux

package core

import (
	"log/slog"
	"time"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/server/dictation"
	"github.com/kombifyio/SpeechKit/internal/server/wssession"
)

// wireDictationStream mounts the streaming Dictation WebSocket surface
// (POST /v1/dictation/stream/sessions → ticket → WS with live partials).
// Called only when the dictation mode is enabled and the STT router exists.
//
// The health component is non-blocking by design: batch dictation is the
// mode's readiness contract; streaming is a capability on top. A deployment
// without a streaming-capable provider (e.g. local-only whisper.cpp) stays
// green on /readyz and reports the capability gap here and in the
// session-create response (capabilities.streaming=false).
func wireDictationStream(cfg *config.Config, app *App) {
	streamCfg := cfg.Server.DictationStream
	if !streamCfg.Enabled {
		app.Health.SetReadyWithOptions("mode.dictation_stream", StatusDisabled, "configured off", ComponentOptions{
			Blocking: false,
			Kind:     "feature",
		})
		return
	}

	ticketTTL := time.Duration(cfg.Server.TicketTTLSec) * time.Second
	if cfg.Server.TicketTTLSec <= 0 {
		ticketTTL = 0
	}
	manager, err := wssession.NewSessionManager(wssession.Options{
		TicketTTL:              ticketTTL,
		MaxGlobalSessions:      streamCfg.MaxGlobalSessions,
		MaxPerIdentitySessions: streamCfg.MaxPerIdentitySessions,
	})
	if err != nil {
		app.Health.SetReadyWithOptions("mode.dictation_stream", StatusUnavailable, err.Error(), ComponentOptions{
			Blocking: false,
			Kind:     "feature",
		})
		slog.Warn("dictation stream: session manager init failed", "err", err)
		return
	}

	idleTimeout := time.Duration(streamCfg.IdleTimeoutSec) * time.Second
	if streamCfg.IdleTimeoutSec < 0 {
		// Negative disables the watchdog explicitly; NewStreamHandler treats
		// negatives as "disabled".
		idleTimeout = -1
	}
	h, err := dictation.NewStreamHandler(dictation.StreamHandlerOptions{
		Manager:            manager,
		Router:             app.STTRouter,
		PublicURL:          cfg.Server.PublicURL,
		AllowedOrigins:     cfg.Server.CORSAllowedOrigins,
		TrustedProxyCIDRs:  cfg.Server.TrustedProxyCIDRs,
		IdleTimeout:        idleTimeout,
		MaxSessionDuration: time.Duration(streamCfg.MaxSessionSec) * time.Second,
		MaxStreamAudio:     time.Duration(streamCfg.MaxStreamAudioSeconds) * time.Second,
		ReadLimit:          cfg.Server.WSReadLimitBytes,
	})
	if err != nil {
		app.Health.SetReadyWithOptions("mode.dictation_stream", StatusUnavailable, err.Error(), ComponentOptions{
			Blocking: false,
			Kind:     "feature",
		})
		slog.Warn("dictation stream: handler init failed", "err", err)
		return
	}
	h.Mount(app.Mux)

	if app.STTRouter.HasDictationStreaming() {
		app.Health.SetReadyWithOptions("mode.dictation_stream", StatusOK, "listening", ComponentOptions{
			Blocking: false,
			Kind:     "feature",
		})
	} else {
		app.Health.SetReadyWithOptions("mode.dictation_stream", StatusDegraded,
			"listening; no streaming-capable STT provider configured (clients fall back to batch)", ComponentOptions{
				Blocking: false,
				Kind:     "feature",
			})
	}
	slog.Info("dictation streaming enabled",
		"create", "/v1/dictation/stream/sessions",
		"ws", "/v1/dictation/stream/sessions/{id}/ws",
		"streaming_provider_available", app.STTRouter.HasDictationStreaming())
}
