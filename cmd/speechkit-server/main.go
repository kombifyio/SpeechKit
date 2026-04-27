//go:build linux

// Package main is the canonical kombify SpeechKit Linux container
// server. It exposes all three modes (Dictation, Assist, Voice Agent)
// over HTTP / WebSocket. For voice-only deployments — same Framework
// kernel, smaller resource footprint, no Dictation/Assist HTTP routes
// — use `cmd/speechkit-voice` instead.
//
// This binary is intentionally Linux-only — the Windows desktop app
// lives under `cmd/speechkit` and shares the same internal packages.
// The build tag above ensures `go build ./...` on Windows skips this
// file and does not break the Wails desktop build.
//
// Usage:
//
//	speechkit-server --config /etc/speechkit/config.toml [--modes dictation,assist,voiceagent]
//
// See docs/server/README.md for the full operator guide.
package main

import "github.com/kombifyio/SpeechKit/internal/server/cli"

// version is overridden at build time via `-ldflags="-X main.version=..."`.
var version = "dev"

func main() {
	cli.Run(cli.Options{
		Banner:  "speechkit-server",
		Version: version,
		// Empty default → Run() leaves cfg.Server.Modes as it came
		// from config.toml, which itself defaults to nil = all three
		// modes enabled. This is the "kitchen sink" deploy.
		DefaultModes: nil,
	})
}
