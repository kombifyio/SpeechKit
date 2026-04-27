//go:build linux

// Package main is the kombify SpeechKit Voice Server â€” a focused
// Linux container that exposes ONLY the Voice Agent mode (real-time
// audio-to-audio dialogue over WebSocket). Same Framework kernel and
// same code path as `speechkit-server`; the only difference is the
// built-in default mode set, which excludes Dictation and Assist HTTP
// routes so the binary stays small and the surface stays focused.
//
// When to use which:
//
//	speechkit-server  â†’ all three modes; one container handles
//	                    Dictation REST, Assist REST, Voice Agent WS.
//	                    Choose for single-pod deployments.
//	speechkit-voice   â†’ only Voice Agent WS. Choose when scaling
//	                    voice independently from REST traffic, or
//	                    when running a stateful voice tier on
//	                    beefier nodes (GPU, more memory) than the
//	                    stateless REST tier needs.
//
// The two are interchangeable from the device-target's
// `[server_connection]` perspective: ModeSource per mode lets the
// desktop app point Dictation/Assist at one URL and Voice Agent at
// another. See docs/server/README.md.
package main

import "github.com/kombifyio/SpeechKit/internal/server/cli"

// version is overridden at build time via `-ldflags="-X main.version=..."`.
var version = "dev"

func main() {
	cli.Run(cli.Options{
		Banner:  "speechkit-voice",
		Version: version,
		// Default to Voice-Agent-only when no `[server].modes` and no
		// `--modes` override are supplied. Operators who want a
		// different mix on this binary can still pass `--modes=...`,
		// but the contract is "this image is the Voice Server".
		DefaultModes: []string{"voiceagent"},
	})
}
