// Command box-evidence-harness is a headless "fake box": it drives the exact
// Voice-Agent WebSocket path the kombify-Box firmware uses and reports the two
// runtime facts the firmware hardcodes against.
//
// Flow (mirrors kombify-box milestones M1.2-M1.4):
//
//  1. Bearer POST /v1/voiceagent/sessions -> single-use ticket.
//  2. WS dial with Sec-WebSocket-Protocol: ticket.<t> and NO Origin header
//     (the ESP32 sends none; the server must run SPEECHKIT_ALLOW_EMPTY_ORIGIN=1).
//  3. start{provider:"deepgram"} within 5 s -> state:listening.
//  4. Stream a 16 kHz S16LE mono utterance, then audio_end.
//  5. Collect input_transcript + response audio until the turn ends.
//
// It then reports:
//   - EMPTY-ORIGIN accepted: proves the server config the firmware relies on.
//   - DOWNLINK ~24 kHz: cross-checks the response PCM byte count against the
//     output-transcript word count so the firmware's 24->48 kHz upsampler is
//     not built on a stale rate assumption.
//
// This is a repo/CI evidence tool, not a distributable client. It reuses the
// public SDK (pkg/speechkit/client) so it also doubles as a minimal net_ws
// reference. See ROADMAP.md (v0.49.0 M1) and the box repo's
// docs/roadmap-standalone-voice.md (M1.0).
//
// Exit codes: 0 = evidence PASS, 2 = blocked_by_auth (no token / 401 / 403;
// CI treats this as skipped), 1 = hard failure (dial rejected, no listening,
// empty transcript, no audio, or downlink rate drift).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/client"
)

const (
	exitPass          = 0
	exitFailure       = 1
	exitBlockedByAuth = 2
)

func main() {
	var (
		server   = flag.String("server", envFirst("SPEECHKIT_SERVER_URL"), "speechkit-server base URL (e.g. http://localhost:8080)")
		token    = flag.String("token", envFirst("SPEECHKIT_TOKEN", "SPEECHKIT_SERVER_TOKEN"), "service bearer token for session creation")
		provider = flag.String("provider", envOr("SPEECHKIT_VA_PROVIDER", "deepgram"), "Voice Agent backend selected in the start frame")
		audio    = flag.String("audio", "testdata/e2e/voiceagent/turn1.wav", "16 kHz S16LE mono WAV to stream as the utterance")
		timeout  = flag.Duration("timeout", 30*time.Second, "overall deadline for the evidence run")
		verbose  = flag.Bool("verbose", false, "log every inbound frame")
	)
	flag.Parse()

	os.Exit(run(runOptions{
		server:    strings.TrimSpace(*server),
		token:     strings.TrimSpace(*token),
		provider:  strings.TrimSpace(*provider),
		audioPath: strings.TrimSpace(*audio),
		timeout:   *timeout,
		verbose:   *verbose,
	}))
}

type runOptions struct {
	server    string
	token     string
	provider  string
	audioPath string
	timeout   time.Duration
	verbose   bool
}

func run(opts runOptions) int {
	if opts.server == "" {
		fmt.Fprintln(os.Stderr, "box-evidence-harness: -server (or SPEECHKIT_SERVER_URL) is required")
		return exitFailure
	}
	if opts.token == "" {
		// The session-create call is Bearer-authenticated. Without a token the
		// run cannot start, so report blocked_by_auth like provider-live-gate.
		fmt.Println("RESULT: blocked_by_auth (no SPEECHKIT_TOKEN / SPEECHKIT_SERVER_TOKEN provided)")
		return exitBlockedByAuth
	}

	rate, pcm, err := loadUtterance(opts.audioPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "box-evidence-harness: load utterance: %v\n", err)
		return exitFailure
	}
	if rate != wsUplinkRate {
		fmt.Fprintf(os.Stderr, "box-evidence-harness: utterance is %d Hz; the WS uplink contract is %d Hz S16LE mono\n", rate, wsUplinkRate)
		return exitFailure
	}

	ctx, cancel := context.WithTimeout(context.Background(), opts.timeout)
	defer cancel()

	api, err := client.New(client.Options{BaseURL: opts.server, Token: opts.token, Timeout: opts.timeout})
	if err != nil {
		fmt.Fprintf(os.Stderr, "box-evidence-harness: client: %v\n", err)
		return exitFailure
	}

	fmt.Printf("→ POST %s/v1/voiceagent/sessions (bearer)\n", opts.server)
	ticket, err := api.CreateVoiceAgentSession(ctx)
	if err != nil {
		if isAuthError(err) {
			fmt.Printf("RESULT: blocked_by_auth (session create rejected: %v)\n", err)
			return exitBlockedByAuth
		}
		fmt.Fprintf(os.Stderr, "box-evidence-harness: create session: %v\n", err)
		return exitFailure
	}
	fmt.Printf("  session=%s ticket-ttl→%s\n", ticket.SessionID, ticket.WSSubprotocol)
	defer func() {
		delCtx, delCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer delCancel()
		_ = api.DeleteVoiceAgentSession(delCtx, ticket.SessionID)
	}()

	// DialVoiceAgent sends only Authorization + the ticket subprotocol — no
	// Origin header — exactly like the ESP32 firmware. A rejection here almost
	// always means SPEECHKIT_ALLOW_EMPTY_ORIGIN=1 is missing on the server.
	fmt.Printf("→ WS dial (no Origin header) subprotocol=%s\n", ticket.WSSubprotocol)
	session, err := api.DialVoiceAgent(ctx, ticket)
	if err != nil {
		fmt.Fprintf(os.Stderr, "box-evidence-harness: WS dial failed: %v\n", err)
		fmt.Fprintln(os.Stderr, "  hint: native clients send no Origin — the server needs SPEECHKIT_ALLOW_EMPTY_ORIGIN=1 (default deny).")
		return exitFailure
	}
	defer func() { _ = session.Close() }()
	fmt.Println("  ✓ EMPTY-ORIGIN accepted — server allows the headless/ESP32 dial")

	if err := session.SendStart(ctx, client.VoiceAgentStartFrame{Provider: opts.provider, Locale: "en"}); err != nil {
		fmt.Fprintf(os.Stderr, "box-evidence-harness: send start: %v\n", err)
		return exitFailure
	}

	ev, err := driveTurn(ctx, session, pcm, opts.verbose)
	if err != nil {
		fmt.Fprintf(os.Stderr, "box-evidence-harness: %v\n", err)
		return exitFailure
	}

	return report(ev, opts.provider)
}

// turnEvidence is what one utterance turn produced.
type turnEvidence struct {
	reachedListening bool
	inputTranscript  string
	outputTranscript string
	responseBytes    int
}

func report(ev turnEvidence, provider string) int {
	fmt.Println()
	fmt.Println("── evidence ──────────────────────────────────────────────")
	fmt.Printf("  provider           : %s\n", provider)
	fmt.Printf("  reached listening  : %v\n", ev.reachedListening)
	fmt.Printf("  input_transcript   : %q\n", ev.inputTranscript)
	fmt.Printf("  output_transcript  : %q\n", ev.outputTranscript)
	fmt.Printf("  response audio     : %d bytes\n", ev.responseBytes)

	fail := false
	if !ev.reachedListening {
		fmt.Println("  ✗ session never reached state:listening")
		fail = true
	}
	if strings.TrimSpace(ev.inputTranscript) == "" {
		fmt.Println("  ✗ STT produced no input_transcript for the streamed utterance")
		fail = true
	}
	if ev.responseBytes == 0 {
		fmt.Println("  ✗ server returned no response audio")
		fail = true
	} else if ev.responseBytes%2 != 0 {
		fmt.Printf("  ✗ response audio byte count %d is odd — not S16LE-aligned\n", ev.responseBytes)
		fail = true
	}

	// Downlink rate cross-check. Words in the output transcript give a rough
	// ground-truth duration (~2.5 synthesized words/sec); the rate whose implied
	// duration best matches that estimate is the server's true downlink rate.
	words := countWords(ev.outputTranscript)
	best, durs := inferDownlinkRate(ev.responseBytes, words)
	fmt.Println("  downlink rate cross-check (S16LE mono):")
	for _, rate := range downlinkCandidates {
		marker := "  "
		if rate == best {
			marker = "→ "
		}
		fmt.Printf("      %s%5d Hz ⇒ %.2fs\n", marker, rate, durs[rate])
	}
	if words >= 2 {
		if best == wsDownlinkRate {
			fmt.Printf("  ✓ DOWNLINK ~%d kHz — %d transcript words imply the %d Hz reading (firmware 24→48k upsample is correct)\n",
				wsDownlinkRate/1000, words, wsDownlinkRate)
		} else {
			fmt.Printf("  ✗ downlink rate DRIFT: %d transcript words best match %d Hz, not the contracted %d Hz\n",
				words, best, wsDownlinkRate)
			fail = true
		}
	} else {
		d := durs[wsDownlinkRate]
		if d >= 0.2 && d <= 60 {
			fmt.Printf("  ~ DOWNLINK not cross-checked (no output transcript); %.2fs @%d Hz is plausible\n", d, wsDownlinkRate)
		} else {
			fmt.Printf("  ✗ no transcript to cross-check and %.2fs @%d Hz is implausible\n", d, wsDownlinkRate)
			fail = true
		}
	}

	fmt.Println("──────────────────────────────────────────────────────────")
	if fail {
		fmt.Println("RESULT: fail")
		return exitFailure
	}
	fmt.Println("RESULT: passed")
	return exitPass
}

func isAuthError(err error) bool {
	var httpErr client.HTTPError
	if errors.As(err, &httpErr) {
		return httpErr.StatusCode == 401 || httpErr.StatusCode == 403
	}
	return false
}

func envFirst(keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
