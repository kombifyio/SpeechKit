//go:build linux

package voiceagent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
)

type continuousDuplexTestProvider struct{ *fakeProvider }

func TestProviderBargeInPreemptsBatchedAudioAndAllowsTheNextReply(t *testing.T) {
	provider := newFakeProvider()
	conn, _ := startReliabilityAdapter(t, provider, &ManagedSession{ID: "provider-barge-in"}, 0)
	sendStart(t, conn, StartFrame{})
	var ready StateFrame
	readJSONFrame(t, conn, &ready)
	// A valid native batch spans several seconds. The provider's next control
	// must be observed while this answer is being paced, without client cancel.
	pcm := make([]byte, 3*outputPCMBytesPerSecond)
	for offset := 0; offset < len(pcm); offset += 2 {
		pcm[offset] = 0xAA
	}
	provider.push(&LiveMessage{Audio: pcm})
	if first := readBinaryFrame(t, conn); len(first) == 0 || first[0] != 0xAA {
		t.Fatalf("initial native audio did not play: %x", first)
	}
	provider.push(&LiveMessage{Interrupted: true})
	deadline, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	for {
		typ, data, err := conn.Read(deadline)
		if err != nil {
			t.Fatalf("provider interruption waited for the paced audio tail: %v", err)
		}
		if typ == websocket.MessageBinary {
			continue // fragments already written before the acknowledgement
		}
		var interrupted InterruptedFrame
		if err := json.Unmarshal(data, &interrupted); err != nil || interrupted.Type != MsgInterrupted {
			t.Fatalf("provider interruption was not acknowledged: %s", data)
		}
		break
	}
	// The provider can still emit a cancelled tail. Only its actual Done
	// releases that epoch; a later real reply must remain audible.
	provider.push(&LiveMessage{Audio: []byte{0xAA, 0x00}})
	provider.push(&LiveMessage{Done: true})
	provider.push(&LiveMessage{Audio: []byte{0xBB, 0x00}})
	next, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	for {
		typ, data, err := conn.Read(next)
		if err != nil {
			t.Fatalf("the reply after the true provider Done stayed muted: %v", err)
		}
		if typ != websocket.MessageBinary {
			continue
		}
		if len(data) != 2 || data[0] != 0xBB {
			t.Fatalf("cancelled native audio resumed after its acknowledgement: %x", data)
		}
		return
	}
}

func (*continuousDuplexTestProvider) ContinuousDuplex() bool { return true }

func TestContinuousDuplexCancelSettlesByOutputCadenceWithoutDone(t *testing.T) {
	provider := &continuousDuplexTestProvider{newFakeProvider()}
	conn, _ := startReliabilityAdapter(t, provider, &ManagedSession{ID: "duplex"}, 0)
	sendStart(t, conn, StartFrame{})
	var ready StateFrame
	readJSONFrame(t, conn, &ready)
	provider.push(&LiveMessage{Audio: []byte{0xAA, 0x00}})
	if got := readBinaryFrame(t, conn); len(got) != 2 || got[0] != 0xAA {
		t.Fatalf("initial output did not play: %x", got)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"cancel"}`)); err != nil {
		t.Fatal(err)
	}
	var interrupted InterruptedFrame
	readJSONFrame(t, conn, &interrupted)
	if interrupted.Type != MsgInterrupted {
		t.Fatalf("cancel was not acknowledged: %+v", interrupted)
	}
	// A current tail must remain muted. The following input transcript is a
	// receive-order barrier; leaked audio would fail the JSON read.
	provider.push(&LiveMessage{Audio: []byte{0xBB, 0x00}})
	provider.push(&LiveMessage{InputTranscript: "still listening"})
	var input TranscriptFrame
	readJSONFrame(t, conn, &input)
	if input.Type != MsgInputTranscript {
		t.Fatalf("cancellation interrupted input: %+v", input)
	}
	// GPT-Live never emits Done. Its existing quiet-output cadence releases
	// local suppression without manufacturing a turn-end event.
	time.Sleep(live.DefaultSpeakingSettleDelay + 50*time.Millisecond)
	provider.push(&LiveMessage{Audio: []byte{0xCC, 0x00}})
	if got := readBinaryFrame(t, conn); len(got) != 2 || got[0] != 0xCC {
		t.Fatalf("later duplex output remained muted without Done: %x", got)
	}
}

func TestBurstAudioStaysWithinPlaybackBudgetAndCancelCutsTail(t *testing.T) {
	provider := newFakeProvider()
	provider.receiveQueue = make(chan *LiveMessage, 64)
	conn, _ := startReliabilityAdapter(t, provider, &ManagedSession{ID: "paced"}, 0)
	sendStart(t, conn, StartFrame{})
	var ready StateFrame
	readJSONFrame(t, conn, &ready)
	// A synthesized answer arrives as a burst, substantially longer than
	// the bounded client queue. Cancel after observing sustained playback.
	for i := range 40 {
		// Native providers can batch more than 500 ms into one valid frame.
		provider.push(&LiveMessage{Audio: make([]byte, 14_400*(1+i%2))})
	}
	provider.push(&LiveMessage{Done: true})
	provider.push(&LiveMessage{OutputTranscript: "following turn", OutputTranscriptDone: true})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	started := time.Now()
	bytes := 0
	cancelled, acknowledged := false, false
	var cancelledAt time.Time
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if typ == websocket.MessageBinary {
			if acknowledged {
				t.Fatal("cancelled audio resumed after acknowledgement")
			}
			bytes += len(data)
			// Include scheduling tolerance, while remaining far below a
			// two-second client queue; a burst sender fails immediately.
			lead := time.Duration(bytes)*time.Second/48000 - time.Since(started)
			if lead > 600*time.Millisecond {
				t.Fatalf("audio outran playback by %v", lead)
			}
			if !cancelled && bytes >= 48000 {
				cancelledAt = time.Now()
				if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"cancel"}`)); err != nil {
					t.Fatal(err)
				}
				cancelled = true
			}
			continue
		}
		var frame struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(data, &frame); err != nil {
			t.Fatal(err)
		}
		if frame.Type == MsgInterrupted {
			acknowledged = true
			if time.Since(cancelledAt) > 500*time.Millisecond {
				t.Fatal("cancel waited for audio tail")
			}
		}
		if frame.Text == "following turn" {
			if !acknowledged {
				t.Fatal("cancel was not acknowledged")
			}
			return
		}
	}
}
