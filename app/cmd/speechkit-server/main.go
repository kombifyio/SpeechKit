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
//	speechkit-server migrate [-runtime-role speechkit_app]
//
// `migrate` is the Postgres migration step. It reads the migration
// role's DSN from SPEECHKIT_MIGRATION_POSTGRES_DSN only, never from the
// serving configuration, applies the schema chain and grants the runtime
// role its row privileges. A runtime-role server (store.postgres_runtime_role)
// never migrates itself.
//
// See docs/server/README.md for the full operator guide.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/kombifyio/SpeechKit/app/internal/productid"
	"github.com/kombifyio/SpeechKit/app/internal/server/cli"
	"github.com/kombifyio/SpeechKit/app/internal/store"
)

// version is overridden at build time via `-ldflags="-X main.version=..."`.
var version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "migrate" {
		if err := migrate(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "speechkit-server migrate:", err)
			os.Exit(1)
		}
		return
	}
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

func migrate(args []string) error {
	flags := flag.NewFlagSet("migrate", flag.ContinueOnError)
	runtimeRole := flags.String("runtime-role", "", "runtime login role to grant row privileges (empty: no grants)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return store.MigratePostgres(ctx, os.Getenv("SPEECHKIT_MIGRATION_POSTGRES_DSN"), *runtimeRole)
}
