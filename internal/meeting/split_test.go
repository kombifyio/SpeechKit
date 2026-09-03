package meeting

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSplitLongLinesLeavesShortLinesAloneAndCutsAtSentenceEnds(t *testing.T) {
	sentence := "Wir haben über das Budget gesprochen und die nächsten Schritte geklärt. "
	long := strings.TrimSpace(strings.Repeat(sentence, 60)) // ≈ 4,300 runes
	lines := []TranscriptLine{
		{SegmentID: 1, Speaker: "Anna", Text: "Kurz."},
		{SegmentID: 2, Speaker: "Ben", Channel: ChannelSystem, StartMs: 25009, Text: long},
		{SegmentID: 3, Text: "Danach."},
	}

	out := SplitLongLines(lines, 0)

	if len(out) < 5 {
		t.Fatalf("got %d lines, want the long one split into several parts", len(out))
	}
	if out[0].Text != "Kurz." || out[len(out)-1].Text != "Danach." {
		t.Fatalf("short lines changed: first=%q last=%q", out[0].Text, out[len(out)-1].Text)
	}
	var rebuilt []string
	for _, line := range out[1 : len(out)-1] {
		if line.SegmentID != 2 || line.Speaker != "Ben" || line.Channel != ChannelSystem || line.StartMs != 25009 {
			t.Fatalf("part lost its identity: %+v", line)
		}
		if n := utf8.RuneCountInString(line.Text); n > MaxTranscriptLineRunes {
			t.Fatalf("part has %d runes, over the limit", n)
		}
		if !strings.HasSuffix(line.Text, ".") {
			t.Fatalf("part does not end at a sentence boundary: %q", line.Text[len(line.Text)-40:])
		}
		rebuilt = append(rebuilt, line.Text)
	}
	if strings.Join(rebuilt, " ") != long {
		t.Fatal("parts do not reassemble into the original text")
	}
}

func TestSplitTextFallsBackToSpacesAndHardCuts(t *testing.T) {
	words := strings.TrimSpace(strings.Repeat("wort ", 100)) // no sentence ends
	parts := splitText(words, 50)
	for _, part := range parts {
		if utf8.RuneCountInString(part) > 50 || strings.Contains(part, "  ") {
			t.Fatalf("bad part %q", part)
		}
	}
	if strings.Join(parts, " ") != words {
		t.Fatal("space-split parts do not reassemble")
	}
	solid := strings.Repeat("x", 120)
	hard := splitText(solid, 50)
	if len(hard) != 3 || len(hard[0]) != 50 || len(hard[2]) != 20 {
		t.Fatalf("hard cuts = %v", func() []int {
			out := []int{}
			for _, p := range hard {
				out = append(out, len(p))
			}
			return out
		}())
	}
}
