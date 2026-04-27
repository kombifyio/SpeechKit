package main

// server_delegates.go wires the device-target's per-mode "should I run this
// locally or against a remote SpeechKit server?" decision based on the
// ResolvedModeSource() value on each ModeModelSelection plus the
// [server_connection] section.
//
// Phase 4 of the ModeSource initiative. Phase 1 shipped the e2e harness,
// Phase 2 the config surface, Phase 3 the transport adapter
// (internal/serverclient). This file is the device-target glue that
// hands those adapters to the existing kernel orchestration code.

import (
	"context"
	"errors"
	"log/slog"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/router"
	"github.com/kombifyio/SpeechKit/internal/serverclient"
	"github.com/kombifyio/SpeechKit/internal/stt"
	"github.com/kombifyio/SpeechKit/internal/voiceagent"
)

// serverDelegates holds per-mode adapters that delegate to a remote
// speechkit-server. A nil field means that mode runs locally (the
// pre-0.26 behaviour). The struct itself may also be nil — checked by
// callers via (*serverDelegates).hasDictation() etc.
type serverDelegates struct {
	client *serverclient.Client

	// dictation is non-nil when ModelSelection.Dictate.ModeSource = "server"
	// and ServerConnection.Enabled = true. Implements stt.STTProvider so
	// it can act as the cloud or replacement provider in router.Router.
	dictation *serverclient.DictationProvider

	// assist is non-nil when ModelSelection.Assist.ModeSource = "server".
	// The device-target swaps it in for the in-process Assist Pipeline.
	assist *serverclient.AssistProcessor

	// voiceAgentEnabled is true when ModelSelection.VoiceAgent.ModeSource
	// = "server". The actual VoiceAgentProvider is constructed per
	// session in prepareVoiceAgentSession (it owns a websocket connection
	// and isn't reusable across sessions).
	voiceAgentEnabled bool

	// fallbackToLocal mirrors ServerConnection.FallbackToLocal. When true,
	// transient server errors fall through to the in-process kernel; when
	// false, they surface to the user.
	fallbackToLocal bool
}

// buildServerDelegates inspects cfg and constructs a serverDelegates
// instance with the appropriate adapters wired up. Returns (nil, nil)
// when no mode opts into server execution OR when ServerConnection is
// disabled — both cases mean "stay fully local". Returns (nil, err) for
// hard misconfigurations (e.g. enabled + missing URL); callers should log
// the error and proceed with the local kernel.
func buildServerDelegates(cfg *config.Config) (*serverDelegates, error) {
	if cfg == nil {
		return nil, nil
	}
	anyServerMode := cfg.ModelSelection.Dictate.ResolvedModeSource() == config.ModeSourceServer ||
		cfg.ModelSelection.Assist.ResolvedModeSource() == config.ModeSourceServer ||
		cfg.ModelSelection.VoiceAgent.ResolvedModeSource() == config.ModeSourceServer
	if !anyServerMode {
		return nil, nil
	}
	client, err := serverclient.NewFromConfig(cfg.ServerConnection)
	if err != nil {
		if errors.Is(err, serverclient.ErrServerConnectionDisabled) {
			// Some mode is configured for "server" but the connection is
			// disabled. Treat as fully local; the UI surfaces this as a
			// warning so the user knows the toggle is dormant.
			slog.Warn("ModeSource=server requested but [server_connection] is disabled; running modes locally")
			return nil, nil
		}
		return nil, err
	}

	d := &serverDelegates{
		client:          client,
		fallbackToLocal: cfg.ServerConnection.FallbackToLocal,
	}
	if cfg.ModelSelection.Dictate.ResolvedModeSource() == config.ModeSourceServer {
		dict, dErr := serverclient.NewDictation(client)
		if dErr != nil {
			return nil, dErr
		}
		d.dictation = dict
	}
	if cfg.ModelSelection.Assist.ResolvedModeSource() == config.ModeSourceServer {
		ass, aErr := serverclient.NewAssist(client)
		if aErr != nil {
			return nil, aErr
		}
		d.assist = ass
	}
	if cfg.ModelSelection.VoiceAgent.ResolvedModeSource() == config.ModeSourceServer {
		d.voiceAgentEnabled = true
	}
	return d, nil
}

// hasDictation reports whether Dictation should run server-side. nil-safe.
func (d *serverDelegates) hasDictation() bool { return d != nil && d.dictation != nil }

// hasAssist reports whether Assist should run server-side. nil-safe.
func (d *serverDelegates) hasAssist() bool { return d != nil && d.assist != nil }

// hasVoiceAgent reports whether Voice Agent should run server-side. nil-safe.
func (d *serverDelegates) hasVoiceAgent() bool { return d != nil && d.voiceAgentEnabled }

// shouldFallback reports whether a server-side error should be retried
// against the in-process kernel. nil-safe (returns false).
func (d *serverDelegates) shouldFallback() bool { return d != nil && d.fallbackToLocal }

// newVoiceAgentProvider constructs a fresh server-backed LiveProvider.
// Each Voice Agent session creates one; the provider owns the underlying
// websocket so it cannot be reused across sessions.
func (d *serverDelegates) newVoiceAgentProvider(cfg *config.Config) voiceagent.LiveProvider {
	if !d.hasVoiceAgent() {
		return nil
	}
	// Persona/Role/Sequence IDs are not yet exposed in the device UI;
	// callers can plumb them later via WithPersona/WithRole/WithSequence.
	// For 0.26.1 we let the server's default persona handle the session.
	_ = cfg
	provider, err := serverclient.NewVoiceAgent(d.client)
	if err != nil {
		slog.Error("server-delegated Voice Agent provider construction failed", "err", err)
		return nil
	}
	return provider
}

// compositeTranscriber wraps a server-side stt.STTProvider with an
// in-process router.Router as fallback. Run() tries the server first;
// if it errors and FallbackToLocal is true, the local router gets a
// second shot.
//
// The fallback is intentionally narrow: only network-class errors trigger
// it. Application errors (e.g. provider_unavailable returned as a typed
// envelope) propagate as-is so misconfigurations aren't silently masked.
type compositeTranscriber struct {
	server   stt.STTProvider
	local    Transcriber // existing routerTranscriber.Route(ctx, audio, durationSecs, opts) shape
	fallback bool
}

// Transcriber is the surface routerTranscriber satisfies; we re-declare
// it here so this file stays free of the local-only type's exact shape.
type Transcriber interface {
	Route(ctx context.Context, audio []byte, audioDurationSecs float64, opts stt.TranscribeOpts) (*stt.Result, error)
}

// newCompositeTranscriber wires server-first with optional local
// fallback. server must be non-nil; local is required only when
// fallback is true.
func newCompositeTranscriber(server stt.STTProvider, local Transcriber, fallback bool) *compositeTranscriber {
	return &compositeTranscriber{server: server, local: local, fallback: fallback}
}

// dictationTranscriber picks the right Transcriber for the Dictation
// mode: a compositeTranscriber when ServerDelegates.hasDictation() is
// true, otherwise the in-process router. Centralised here so the wiring
// stays declarative at the main.go construction site.
func dictationTranscriber(local *router.Router, d *serverDelegates) Transcriber {
	if d == nil || !d.hasDictation() {
		return local
	}
	return newCompositeTranscriber(d.dictation, local, d.shouldFallback())
}

// Route satisfies the Transcriber interface routerTranscriber expects.
func (c *compositeTranscriber) Route(ctx context.Context, audio []byte, audioDurationSecs float64, opts stt.TranscribeOpts) (*stt.Result, error) {
	_ = audioDurationSecs // server doesn't care; the kernel router uses it for "prefer local under N seconds" heuristics

	res, err := c.server.Transcribe(ctx, audio, opts)
	if err == nil {
		return res, nil
	}

	if !c.fallback || c.local == nil {
		return nil, err
	}
	// Only fall back on transport-class errors. Typed *ServerError with a
	// non-empty Code is an application error — surface it.
	var se *serverclient.ServerError
	if errors.As(err, &se) && se != nil && se.Code != "" {
		return nil, err
	}

	slog.Warn("server transcribe failed, falling back to local kernel",
		"err", err)
	return c.local.Route(ctx, audio, audioDurationSecs, opts)
}
