package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/client"
)

// uplinkFrameBytes is one 20 ms frame of 16 kHz S16LE mono audio (320 samples
// × 2 bytes). Streaming in real-time-paced frames lets the realtime provider's
// VAD behave the way it will with the live ESP32 mic.
const uplinkFrameBytes = (wsUplinkRate / 50) * 2

// driveTurn waits for state:listening, streams the utterance in paced frames,
// ends the turn, and collects the transcripts + response audio until the turn
// completes or the context deadline fires.
func driveTurn(ctx context.Context, session *client.VoiceAgentSession, pcm []byte, verbose bool) (turnEvidence, error) {
	var ev turnEvidence

	if err := waitForListening(ctx, session, &ev, verbose); err != nil {
		return ev, err
	}

	if err := streamPCM(ctx, session, pcm); err != nil {
		return ev, err
	}
	if err := session.SendAudioEnd(ctx); err != nil {
		return ev, fmt.Errorf("audio_end: %w", err)
	}
	if verbose {
		log.Printf("streamed %d bytes, sent audio_end", len(pcm))
	}

	return collectResponse(ctx, session, ev, verbose)
}

// waitForListening reads frames until the server reports state:listening (the
// signal that the session is ready for audio). The mandatory start frame must
// land within 5 s per the wire contract, so this uses a bounded sub-deadline.
func waitForListening(ctx context.Context, session *client.VoiceAgentSession, ev *turnEvidence, verbose bool) error {
	subCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	for {
		msg, err := session.ReadMessage(subCtx)
		if err != nil {
			return fmt.Errorf("waiting for listening: %w", err)
		}
		if msg.Frame == nil {
			continue
		}
		logFrame(verbose, msg.Frame)
		switch msg.Frame.Type {
		case "state":
			if msg.Frame.State == "listening" {
				ev.reachedListening = true
				return nil
			}
		case "error":
			return fmt.Errorf("server error before listening: %s: %s", msg.Frame.Code, msg.Frame.Message)
		case "session_end":
			return fmt.Errorf("session ended before listening: %s", msg.Frame.Reason)
		}
	}
}

// streamPCM writes the utterance as real-time-paced 20 ms frames.
func streamPCM(ctx context.Context, session *client.VoiceAgentSession, pcm []byte) error {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for off := 0; off < len(pcm); off += uplinkFrameBytes {
		end := off + uplinkFrameBytes
		if end > len(pcm) {
			end = len(pcm)
		}
		if err := session.SendAudio(ctx, pcm[off:end]); err != nil {
			return fmt.Errorf("send audio: %w", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
	return nil
}

// collectResponse gathers transcripts and response audio until the turn ends.
// The turn is complete when the server returns to listening after speaking, a
// turn_end event arrives, or the session ends. A short idle timer after the
// last audio chunk guards against providers that never re-emit listening.
func collectResponse(ctx context.Context, session *client.VoiceAgentSession, ev turnEvidence, verbose bool) (turnEvidence, error) {
	var lastState string
	idle := time.NewTimer(6 * time.Second)
	defer idle.Stop()

	type readResult struct {
		msg client.VoiceAgentMessage
		err error
	}
	reads := make(chan readResult, 1)
	readNext := func() { go func() { m, e := session.ReadMessage(ctx); reads <- readResult{m, e} }() }
	readNext()

	for {
		select {
		case <-ctx.Done():
			// Deadline is a soft stop: return what we gathered so report() can
			// judge it rather than discarding a nearly-complete turn.
			return ev, nil
		case <-idle.C:
			if ev.responseBytes > 0 {
				return ev, nil
			}
			return ev, errors.New("no response audio within idle window")
		case r := <-reads:
			if r.err != nil {
				if errors.Is(r.err, io.EOF) || ctx.Err() != nil {
					return ev, nil
				}
				return ev, fmt.Errorf("read response: %w", r.err)
			}
			if len(r.msg.Audio) > 0 {
				ev.responseBytes += len(r.msg.Audio)
				idle.Reset(2 * time.Second)
				readNext()
				continue
			}
			if r.msg.Frame == nil {
				readNext()
				continue
			}
			logFrame(verbose, r.msg.Frame)
			f := r.msg.Frame
			switch f.Type {
			case "input_transcript":
				if strings.TrimSpace(f.Text) != "" {
					ev.inputTranscript = f.Text
				}
			case "output_transcript":
				if strings.TrimSpace(f.Text) != "" {
					ev.outputTranscript += f.Text
				}
			case "state":
				if f.State == "listening" && (lastState == "speaking" || lastState == "processing") && ev.responseBytes > 0 {
					return ev, nil
				}
				lastState = f.State
			case "event":
				if f.EventType == "turn_end" || containsStr(f.EventTypes, "turn_end") {
					if ev.responseBytes > 0 {
						return ev, nil
					}
				}
			case "error":
				return ev, fmt.Errorf("server error during turn: %s: %s", f.Code, f.Message)
			case "session_end":
				return ev, nil
			}
			readNext()
		}
	}
}

func containsStr(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

func logFrame(verbose bool, f *client.VoiceAgentFrame) {
	if !verbose || f == nil {
		return
	}
	log.Printf("frame type=%s state=%s done=%v text=%q", f.Type, f.State, f.Done, truncateFrame(f.Text))
}

func truncateFrame(s string) string {
	if len(s) <= 80 {
		return s
	}
	return s[:80] + "…"
}
