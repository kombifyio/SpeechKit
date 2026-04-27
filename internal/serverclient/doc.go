// Package serverclient is the client-side transport adapter that lets a
// device-target (cmd/speechkit) or a local-target binary delegate one or
// more modes (Dictation, Assist, Voice Agent) to a remote SpeechKit
// Server-Target instead of running the Framework kernel in-process.
//
// Each mode adapter implements the same Go interface that the kernel
// already exposes for in-process use:
//
//	stt.STTProvider              for Dictation       (POST /v1/dictation/transcribe)
//	assistpkg.Processor          for Assist          (POST /v1/assist/process)
//	voiceagent.LiveProvider      for Voice Agent     (POST /v1/voiceagent/sessions + WS)
//
// Callers therefore swap a serverclient adapter for the in-process
// implementation without touching their orchestration code. The choice is
// made by config.ModeModelSelection.ResolvedModeSource() per mode; see the
// device-target's app_init.go for the routing.
//
// The package is intentionally cross-platform pure Go (no build tag) so it
// works on Windows desktop, Linux daemons, and anywhere else a Go binary
// might want to embed SpeechKit "as a service".
package serverclient
