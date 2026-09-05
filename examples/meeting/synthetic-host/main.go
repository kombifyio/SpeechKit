// Example: embed the public Meeting runtime in a Go host with synthetic input.
//
// This does not capture audio, call STT, or use cloud services. The host
// adapter below emits public capture events and commits fixed transcript lines
// so the example can run anywhere while showing the same public API boundaries
// a real host would use.
//
// Run from the repository root:
//
//	go run ./examples/meeting/synthetic-host
package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/audio/capture"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/meeting"
)

type syntheticHost struct {
	runtime *meeting.Runtime

	mu         sync.Mutex
	transcript []meeting.TranscriptLine
	events     []string
	started    map[string]bool
}

func newSyntheticHost() *syntheticHost {
	host := &syntheticHost{started: map[string]bool{}}
	startedAt := time.Date(2026, 1, 2, 15, 4, 0, 0, time.Local)
	host.runtime = meeting.New(meeting.Options{
		NewPipeline:  host.newPipeline,
		Now:          func() time.Time { return startedAt },
		DrainTimeout: 50 * time.Millisecond,
		DrainPoll:    time.Millisecond,
		Log: func(message, kind string) {
			fmt.Printf("runtime log: %s: %s\n", kind, message)
		},
	})
	return host
}

func (h *syntheticHost) newPipeline(channel string) (meeting.Pipeline, error) {
	switch channel {
	case meeting.ChannelMicrophone, meeting.ChannelSystem:
		return &syntheticPipeline{
			channel: channel,
			events:  make(chan capture.Event, 4),
			host:    h,
		}, nil
	default:
		return nil, fmt.Errorf("synthetic host does not provide channel %q", channel)
	}
}

func (h *syntheticHost) commitTranscript(opts speechkit.RecordingStartOptions) error {
	var line meeting.TranscriptLine
	switch opts.CaptureChannel {
	case meeting.ChannelMicrophone:
		line = meeting.TranscriptLine{
			SegmentID: 1,
			Channel:   opts.CaptureChannel,
			StartMs:   1000,
			Text:      "I will send the architecture recap after the call.",
		}
	case meeting.ChannelSystem:
		line = meeting.TranscriptLine{
			SegmentID: 2,
			Channel:   opts.CaptureChannel,
			StartMs:   4000,
			Text:      "Please keep REST, SSE, and TypeScript as separate follow-up work.",
		}
	default:
		return fmt.Errorf("unexpected synthetic channel %q", opts.CaptureChannel)
	}

	h.runtime.NoteSegmentSubmitted(opts.RecordingSessionID)
	h.mu.Lock()
	if !h.started[opts.CaptureChannel] {
		h.started[opts.CaptureChannel] = true
		h.transcript = append(h.transcript, line)
	}
	h.mu.Unlock()
	h.runtime.NoteSegmentCommitted(opts.RecordingSessionID)
	return nil
}

func (h *syntheticHost) recordEvent(channel string, event capture.Event) {
	h.mu.Lock()
	h.events = append(h.events, fmt.Sprintf("%s:%s", channel, event.Type))
	h.mu.Unlock()
}

func (h *syntheticHost) transcriptLines() []meeting.TranscriptLine {
	h.mu.Lock()
	defer h.mu.Unlock()
	lines := append([]meeting.TranscriptLine(nil), h.transcript...)
	sort.Slice(lines, func(i, j int) bool {
		return lines[i].StartMs < lines[j].StartMs
	})
	return lines
}

func (h *syntheticHost) eventLog() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return strings.Join(h.events, ", ")
}

type syntheticPipeline struct {
	channel string
	events  chan capture.Event
	host    *syntheticHost
}

func (p *syntheticPipeline) Channel() string { return p.channel }

func (p *syntheticPipeline) Start(opts speechkit.RecordingStartOptions) error {
	event := capture.Event{
		Type:    capture.EventStarted,
		Backend: capture.BackendAuto,
		Message: "synthetic adapter started",
	}
	p.host.recordEvent(p.channel, event)
	p.events <- event
	return p.host.commitTranscript(opts)
}

func (p *syntheticPipeline) Stop(speechkit.RecordingStopOptions) error {
	event := capture.Event{
		Type:    capture.EventStopped,
		Backend: capture.BackendAuto,
		Message: "synthetic adapter stopped",
	}
	p.host.recordEvent(p.channel, event)
	p.events <- event
	return nil
}

func (p *syntheticPipeline) Events() <-chan capture.Event { return p.events }

func (p *syntheticPipeline) Close() error {
	close(p.events)
	return nil
}

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	host := newSyntheticHost()

	start, err := host.runtime.Start(ctx, meeting.StartOptions{
		SessionID: 1001,
		Title:     "Synthetic planning review",
		Language:  "en",
		Channels:  []string{meeting.ChannelMicrophone, meeting.ChannelSystem},
	})
	if err != nil {
		return fmt.Errorf("start meeting: %w", err)
	}

	fmt.Println("mode: synthetic host adapter; no microphone, STT provider, or cloud call")
	fmt.Printf("start state: %s (%s)\n", start.State, channelStates(start.Channels))

	stop, err := host.runtime.Stop(ctx)
	if err != nil {
		return fmt.Errorf("stop meeting: %w", err)
	}
	fmt.Printf("stop state: %s\n", stop.State)
	fmt.Printf("capture events: %s\n\n", host.eventLog())

	lines := meeting.SuppressEcho(host.transcriptLines())
	fmt.Println("transcript:")
	fmt.Println(meeting.RenderTranscript(lines))
	fmt.Println()

	anchors := []meeting.Anchor{{
		ID:   "host-note-1",
		Text: "Host note: keep REST/SSE/TypeScript out of this Go promotion slice.",
		TsMs: 4500,
	}}
	document := meeting.NotesDocument{
		TemplateSlug: meeting.TemplateDefaultMeeting,
		Locale:       "en",
		ExecutiveBrief: []string{
			"Synthetic transcript lines were committed through a host-owned adapter.",
			"The public Meeting runtime owned lifecycle and channel state.",
		},
		Sections: []meeting.NotesSection{
			{
				Slug:  "summary",
				Title: "Summary",
				Bullets: []meeting.NotesBullet{
					{Text: "The host can embed Meeting runtime without importing internal packages.", SourceSegmentIDs: []int64{1}},
					{Text: "placeholder model wording", SourceSegmentIDs: []int64{2}, AnchorID: "host-note-1"},
				},
			},
		},
	}.ApplyAnchors(anchors)

	fmt.Println("notes:")
	fmt.Print(document.MarkdownDocument("Synthetic planning review", start.StartedAt))
	return nil
}

func channelStates(channels []meeting.ChannelSnapshot) string {
	parts := make([]string, 0, len(channels))
	for _, channel := range channels {
		parts = append(parts, fmt.Sprintf("%s=%s", channel.Channel, channel.State))
	}
	return strings.Join(parts, ", ")
}
