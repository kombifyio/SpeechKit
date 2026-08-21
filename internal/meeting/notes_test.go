package meeting

import (
	"strings"
	"testing"
)

// The user's notes are the one part of the write-up a model must not rewrite.
// A tidied-up note is no longer what the person in the meeting wrote down.
func TestApplyAnchorsRestoresTheUsersOwnWording(t *testing.T) {
	anchors := []Anchor{{ID: "a1", Text: "pricing pushback — legal too", TsMs: 12000}}
	document := NotesDocument{Sections: []NotesSection{{
		Slug:  "discussion",
		Title: "Discussion",
		Bullets: []NotesBullet{
			{Text: "The customer raised concerns regarding pricing and legal review.", AnchorID: "a1"},
			{Text: "Timeline was confirmed.", SourceSegmentIDs: []int64{4}},
		},
	}}}

	applied := document.ApplyAnchors(anchors)

	bullets := applied.Sections[0].Bullets
	if bullets[0].Text != "pricing pushback — legal too" {
		t.Fatalf("the model's paraphrase survived: %q", bullets[0].Text)
	}
	if bullets[1].Text != "Timeline was confirmed." {
		t.Fatalf("a generated bullet was altered: %q", bullets[1].Text)
	}
}

func TestApplyAnchorsKeepsANoteTheModelDropped(t *testing.T) {
	anchors := []Anchor{
		{ID: "a1", Text: "pricing pushback"},
		{ID: "a2", Text: "ask about the migration window"},
	}
	document := NotesDocument{Sections: []NotesSection{{
		Slug:    "discussion",
		Title:   "Discussion",
		Bullets: []NotesBullet{{Text: "pricing", AnchorID: "a1"}},
	}}}

	applied := document.ApplyAnchors(anchors)

	if !strings.Contains(applied.Markdown(), "ask about the migration window") {
		t.Fatalf("a note the user wrote went missing:\n%s", applied.Markdown())
	}
}

func TestApplyAnchorsIgnoresAnInventedAnchorID(t *testing.T) {
	document := NotesDocument{Sections: []NotesSection{{
		Slug:    "discussion",
		Title:   "Discussion",
		Bullets: []NotesBullet{{Text: "Something the model made up an id for.", AnchorID: "nope"}},
	}}}

	applied := document.ApplyAnchors([]Anchor{{ID: "a1", Text: "real note"}})

	bullets := applied.Sections[0].Bullets
	if bullets[0].AnchorID != "" {
		t.Fatalf("an id pointing at no note was kept: %+v", bullets[0])
	}
	if bullets[0].Text != "Something the model made up an id for." {
		t.Fatalf("the bullet text changed: %q", bullets[0].Text)
	}
}

// Without a headset the microphone hears the call through the speakers, so the
// same sentence arrives on both channels — the second copy usually garbled.
func TestSuppressEchoDropsTheSpeakerBleedCopy(t *testing.T) {
	lines := []TranscriptLine{
		{SegmentID: 1, Channel: ChannelSystem, StartMs: 4000, Text: "Release readiness check for the launch."},
		{SegmentID: 2, Channel: ChannelMicrophone, StartMs: 4200, Text: "Release readiness check for the launch"},
	}

	out := SuppressEcho(lines)

	if len(out) != 1 {
		t.Fatalf("expected the echoed copy to be dropped, got %d lines", len(out))
	}
	if out[0].Channel != ChannelSystem {
		t.Fatalf("kept the microphone's echo instead of the call audio: %+v", out[0])
	}
}

func TestSuppressEchoKeepsTwoPeopleSayingDifferentThings(t *testing.T) {
	lines := []TranscriptLine{
		{SegmentID: 1, Channel: ChannelSystem, StartMs: 4000, Text: "Can you walk us through the pricing model?"},
		{SegmentID: 2, Channel: ChannelMicrophone, StartMs: 4500, Text: "Sure, let me share my screen first."},
	}

	if out := SuppressEcho(lines); len(out) != 2 {
		t.Fatalf("a genuine exchange was collapsed into %d line(s)", len(out))
	}
}

func TestSuppressEchoKeepsTheSameWordsFarApart(t *testing.T) {
	lines := []TranscriptLine{
		{SegmentID: 1, Channel: ChannelSystem, StartMs: 4000, Text: "Let us go through the release checklist."},
		{SegmentID: 2, Channel: ChannelMicrophone, StartMs: 900_000, Text: "Let us go through the release checklist."},
	}

	if out := SuppressEcho(lines); len(out) != 2 {
		t.Fatalf("a phrase repeated fifteen minutes later was treated as an echo")
	}
}

func TestChunkTranscriptSplitsOnlyWhenItHasTo(t *testing.T) {
	lines := make([]TranscriptLine, 0, 40)
	for i := 0; i < 40; i++ {
		lines = append(lines, TranscriptLine{SegmentID: int64(i + 1), Text: strings.Repeat("word ", 40)})
	}

	if chunks := ChunkTranscript(lines, 0); len(chunks) != 1 {
		t.Fatalf("a model that can hold the meeting got %d chunks, want one pass", len(chunks))
	}

	chunks := ChunkTranscript(lines, 500)
	if len(chunks) < 2 {
		t.Fatal("a transcript over budget was not split")
	}
	total := 0
	for _, chunk := range chunks {
		total += len(chunk)
	}
	if total != len(lines) {
		t.Fatalf("chunking lost %d line(s) of the meeting", len(lines)-total)
	}
}

func TestRenderTranscriptNumbersLinesSoBulletsCanCiteThem(t *testing.T) {
	rendered := RenderTranscript([]TranscriptLine{
		{SegmentID: 17, Channel: ChannelMicrophone, StartMs: 63000, Text: "I will send the contract."},
	})

	if !strings.Contains(rendered, "[17]") {
		t.Fatalf("the segment id is missing, so a bullet cannot cite it:\n%s", rendered)
	}
	if !strings.Contains(rendered, "Me") || !strings.Contains(rendered, "00:01:03") {
		t.Fatalf("speaker or offset missing:\n%s", rendered)
	}
}
