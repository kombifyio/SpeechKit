// Command agentbridge-codex demonstrates the External Coding Agent Bridge in
// exec mode: one prompt in, normalized events out. It runs against the real
// codex CLI when installed and signed in, or against the fakecodex fixture:
//
//	go build -o fakecodex.exe ./tools/fakecodex
//	FAKECODEX_SCRIPT=pkg/speechkit/agentbridge/codex/testdata/simple-turn.jsonl \
//	  go run ./examples/agentbridge-codex -binary ./fakecodex.exe -prompt "hello"
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/agentbridge"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/agentbridge/codex"
)

func main() {
	os.Exit(run())
}

// run keeps deferred cleanup (bridge.Close, cancel) ahead of the process
// exit code — os.Exit in main would skip the defers.
func run() int {
	binary := flag.String("binary", "", "codex binary path (empty = PATH lookup)")
	project := flag.String("project", ".", "project directory the agent may work in")
	prompt := flag.String("prompt", "Summarize what this repository does.", "turn prompt")
	timeout := flag.Duration("timeout", 60*time.Second, "max wait for the turn")
	flag.Parse()

	bridge := codex.New(codex.Config{BinaryPath: *binary})
	defer func() { _ = bridge.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	st := bridge.Status(ctx)
	fmt.Printf("status: installed=%v auth=%s plan=%q mode=%s version=%q\n", st.Installed, st.Auth, st.Plan, st.Mode, st.Version)
	if st.Detail != "" {
		fmt.Printf("detail: %s\n", st.Detail)
	}
	if st.Mode == agentbridge.ModeUnavailable {
		return 1
	}

	ref, err := bridge.StartTurn(ctx, agentbridge.TurnRequest{
		Project: agentbridge.Project{Alias: "example", Path: *project, Sandbox: agentbridge.SandboxReadOnly},
		Prompt:  *prompt,
		Sandbox: agentbridge.SandboxReadOnly,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "start turn: %v\n", err)
		return 1
	}
	fmt.Printf("turn started (thread=%q)\n", ref.ThreadID)

	for {
		select {
		case ev, ok := <-bridge.Events():
			if !ok {
				return 0
			}
			switch ev.Type {
			case agentbridge.EventItemStarted, agentbridge.EventItemCompleted:
				if ev.Item != nil {
					fmt.Printf("%-15s %-18s %s\n", ev.Type, ev.Item.Kind, ev.Item.Summary)
				}
			case agentbridge.EventError:
				fmt.Printf("%-15s %s\n", ev.Type, ev.Err)
				return 0
			case agentbridge.EventTurnCompleted:
				fmt.Printf("%-15s thread=%s\n", ev.Type, ev.ThreadID)
				return 0
			default:
				fmt.Printf("%-15s thread=%s\n", ev.Type, ev.ThreadID)
			}
		case <-ctx.Done():
			fmt.Fprintln(os.Stderr, "timeout waiting for turn")
			return 1
		}
	}
}
