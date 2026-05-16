// Example: 15-minute Voice-Agent game instructor.
//
// This is the reference an independent coding agent can adapt with a single
// prompt: "Build a 15-min voice-agent game instructor in my app using
// SpeechKit." It connects to a running speechkit-server, ensures the
// game-instructor persona/role/sequence are present (idempotent upsert when
// the bearer token has admin role, otherwise it assumes they were seeded
// from examples/voice-agent/game-instructor/config.example.toml), opens a duplex
// Voice Agent WebSocket, and drives a text-based dialogue loop bounded by a
// 15-minute deadline.
//
// Why text-mode in the demo: audio capture is OS-specific and would make
// this example unrunnable in CI. The same VoiceAgentSession also accepts
// raw PCM via SendAudio; swap stdin for a mic source to go fully voice.
//
// Run:
//
//	# 1. Start a speechkit-server seeded with this directory's config.toml
//	SPEECHKIT_SERVER_TOKEN=devtoken \
//	GOOGLE_AI_API_KEY=...           \
//	speechkit-server --config examples/voice-agent/game-instructor/config.example.toml
//
//	# 2. In another terminal, run the embedder.
//	SPEECHKIT_SERVER_URL=http://localhost:8080 \
//	SPEECHKIT_SERVER_TOKEN=devtoken            \
//	go run ./examples/voice-agent/game-instructor
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/client"
)

const (
	personaID  = "game-instructor"
	roleID     = "game-moderator"
	sequenceID = "game-flow-15min"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		duration  = flag.Duration("duration", 15*time.Minute, "session deadline; the run exits cleanly at this point")
		bootstrap = flag.Bool("bootstrap", true, "POST persona/role/sequence on startup (needs admin bearer); set false when seeded via TOML")
	)
	flag.Parse()

	c, err := client.FromEnv()
	if err != nil {
		return fmt.Errorf("client: %w", err)
	}

	rootCtx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if *bootstrap {
		if err := ensureGameInstructor(rootCtx, c); err != nil {
			// Bootstrap is best-effort: if the token isn't admin we
			// still try to use the IDs assuming they were seeded from
			// config.example.toml. Print the issue, don't bail.
			fmt.Fprintf(os.Stderr, "warning: bootstrap skipped (%v) — assuming personas are seeded via config.example.toml\n", err)
		}
	}

	ticket, err := c.CreateVoiceAgentSession(rootCtx)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	fmt.Printf("session=%s expires_at=%s\n", ticket.SessionID, ticket.ExpiresAt.Format(time.RFC3339))

	// Always release the server-side slot, even on a crash.
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := c.DeleteVoiceAgentSession(shutdownCtx, ticket.SessionID); err != nil {
			fmt.Fprintf(os.Stderr, "warn: delete session %s: %v\n", ticket.SessionID, err)
		}
	}()

	deadlineCtx, deadlineCancel := context.WithTimeout(rootCtx, *duration)
	defer deadlineCancel()

	session, err := c.DialVoiceAgent(deadlineCtx, ticket)
	if err != nil {
		return fmt.Errorf("dial ws: %w", err)
	}
	defer func() { _ = session.Close() }()

	if err := session.SendStart(deadlineCtx, client.VoiceAgentStartFrame{
		PersonaID:  personaID,
		RoleID:     roleID,
		SequenceID: sequenceID,
		Locale:     "en",
		Thinking:   "low",
	}); err != nil {
		return fmt.Errorf("send start: %w", err)
	}

	if err := runDialogue(deadlineCtx, session); err != nil {
		return fmt.Errorf("session ended: %w", err)
	}
	fmt.Println("session ended cleanly.")
	return nil
}

// runDialogue blocks until the WebSocket closes, the deadline fires, or stdin
// reaches EOF. It pumps two cooperating goroutines: stdin → WS text frames
// and WS frames → stdout transcripts. Audio frames are counted but not
// played back so this stays a portable text demo.
func runDialogue(ctx context.Context, session *client.VoiceAgentSession) error {
	var wg sync.WaitGroup
	errCh := make(chan error, 2)

	// Reader: server → stdout.
	wg.Add(1)
	go func() {
		defer wg.Done()
		errCh <- pumpServerFrames(ctx, session)
	}()

	// Writer: stdin → server text turns.
	wg.Add(1)
	go func() {
		defer wg.Done()
		errCh <- pumpUserInput(ctx, session)
	}()

	// Wait for either side to finish, then unblock the other by closing.
	first := <-errCh
	stopCtx, stopCancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer stopCancel()
	_ = session.SendStop(stopCtx)
	_ = session.Close()
	<-errCh
	wg.Wait()

	if errors.Is(first, context.Canceled) || errors.Is(first, context.DeadlineExceeded) {
		return nil
	}
	if errors.Is(first, io.EOF) {
		return nil
	}
	return first
}

func pumpServerFrames(ctx context.Context, session *client.VoiceAgentSession) error {
	audioBytes := 0
	for {
		msg, err := session.ReadMessage(ctx)
		if err != nil {
			return err
		}
		if msg.Audio != nil {
			audioBytes += len(msg.Audio)
			continue
		}
		f := msg.Frame
		switch f.Type {
		case "state":
			fmt.Printf("[state=%s]\n", f.State)
		case "input_transcript":
			if f.Text != "" {
				fmt.Printf("you: %s%s\n", f.Text, doneMarker(f.Done))
			}
		case "output_transcript":
			if f.Text != "" {
				fmt.Printf("agent: %s%s\n", f.Text, doneMarker(f.Done))
			}
		case "sequence_step":
			fmt.Printf("[sequence_step %s #%d → %s]\n", f.StepID, f.StepIndex, f.Status)
		case "interrupted":
			fmt.Println("[barge-in]")
		case "error":
			return fmt.Errorf("server error %s: %s", f.Code, f.Message)
		case "session_end":
			fmt.Printf("[session_end reason=%s audio_bytes=%d]\n", f.Reason, audioBytes)
			return io.EOF
		}
	}
}

func pumpUserInput(ctx context.Context, session *client.VoiceAgentSession) error {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 16*1024), 1<<20)
	fmt.Println("type a turn and press Enter (empty line advances sequence step; '/quit' ends session):")
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return err
			}
			return io.EOF
		}
		line := strings.TrimSpace(scanner.Text())
		switch line {
		case "":
			if err := session.AdvanceStep(ctx, "user requested next step"); err != nil {
				return fmt.Errorf("advance_step: %w", err)
			}
		case "/quit":
			return session.SendStop(ctx)
		default:
			if err := session.SendText(ctx, line); err != nil {
				return fmt.Errorf("send text: %w", err)
			}
		}
	}
}

func doneMarker(done bool) string {
	if done {
		return ""
	}
	return " …"
}

// ensureGameInstructor PUTs the three records the example depends on. The
// server's persona handler treats POST as idempotent upsert when the bearer
// has the admin role. When the bearer is not admin this returns an HTTP
// error and the caller is expected to have seeded the TOML.
func ensureGameInstructor(ctx context.Context, c *client.Client) error {
	persona := map[string]any{
		"id":               personaID,
		"display_name":     "Game Instructor",
		"description":      "Energetic moderator running a 15-min trivia game with one player.",
		"voice":            "Puck",
		"locale":           "en",
		"default_role":     roleID,
		"default_sequence": sequenceID,
		"tags":             []string{"game", "demo"},
	}
	role := map[string]any{
		"id":           roleID,
		"display_name": "Game Moderator",
		"system_prompt": strings.Join([]string{
			"You are a friendly, fast-paced game instructor running a 15-minute voice session with one player.",
			"Greet briefly, explain rules under 30 seconds, then run the game one short turn at a time.",
			"React to the player's answer before posing the next one. Keep each turn under 40 words.",
			"Track score out loud at natural moments. Never invent rules mid-game. End cleanly when the sequence step says so or when the server signals end.",
		}, " "),
		"refinement_prompt":            "Keep responses concise — under 40 words per turn.",
		"temperature":                  0.7,
		"thinking_enabled":             true,
		"thinking_level":               "low",
		"automatic_activity_detection": true,
		"vad_start_sensitivity":        "medium",
		"vad_end_sensitivity":          "medium",
		"activity_handling":            "start_of_activity_interrupts",
		"turn_coverage":                "turn_includes_only_activity",
		"context_compression_enabled":  true,
	}
	sequence := map[string]any{
		"id":           sequenceID,
		"display_name": "15-minute Trivia Round",
		"description":  "Three-phase trivia: warm-up, main round, lightning finish.",
		"completion":   "max_turns",
		"max_turns":    30,
		"steps": []map[string]any{
			{"id": "intro", "instruction": "Greet the player, explain the game in two short sentences, ask if they're ready.", "exit_criteria": "Player confirmed they are ready to start.", "max_turns": 3},
			{"id": "warmup", "instruction": "Ask 3 easy general-knowledge questions, one at a time. Acknowledge each answer briefly. Track score.", "exit_criteria": "Three warm-up questions answered or skipped.", "max_turns": 8},
			{"id": "main", "instruction": "Ask 4 medium-difficulty questions on a topic the player picks. After each, briefly explain the correct answer.", "exit_criteria": "Four main-round questions answered or skipped.", "max_turns": 12},
			{"id": "finish", "instruction": "Announce the final score, give a one-line recap of the player's best moment, offer a rematch.", "exit_criteria": "Final score and recap delivered.", "max_turns": 3},
		},
	}

	for _, step := range []struct {
		name    string
		path    string
		payload any
	}{
		{"persona", "/v1/personas", persona},
		{"role", "/v1/roles", role},
		{"sequence", "/v1/sequences", sequence},
	} {
		if _, err := c.RawJSON(ctx, http.MethodPost, step.path, step.payload); err != nil {
			return fmt.Errorf("upsert %s: %w", step.name, err)
		}
	}
	return nil
}
