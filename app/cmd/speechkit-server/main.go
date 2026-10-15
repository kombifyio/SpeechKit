//go:build linux

// Package main is the canonical kombify SpeechKit Linux container
// server. It exposes all three modes (Dictation, Assist, Voice Agent)
// over HTTP / WebSocket.
//
// This binary is intentionally Linux-only — the Windows desktop app
// lives under `app/cmd/speechkit` and shares the same internal packages.
// The build tag above ensures `go build ./...` on Windows skips this
// file and does not break the Wails desktop build.
//
// Usage:
//
//	speechkit-server --config /etc/speechkit/config.toml [--modes dictation,assist,voiceagent]
//
// See docs/server/README.md for the full operator guide.
package main

import (
	"fmt"
	"os"

	"github.com/kombifyio/SpeechKit/app/internal/productid"
	"github.com/kombifyio/SpeechKit/app/internal/server/cli"
)

// version is overridden at build time via `-ldflags="-X main.version=..."`.
var version = "dev"

func main() {
	// The server shares the reference client's data directory, secret store
	// and policy roots.
	if err := productid.Register(productid.Reference); err != nil {
		fmt.Fprintln(os.Stderr, "speechkit-server:", err)
		os.Exit(1)
	}
	cli.Run(cli.Options{
		Banner:  "speechkit-server",
		Version: version,
		// Empty default → Run() leaves cfg.Server.Modes as it came
		// from config.toml, which itself defaults to nil = all three
		// modes enabled. This is the "kitchen sink" deploy.
		DefaultModes: nil,
	})
}
