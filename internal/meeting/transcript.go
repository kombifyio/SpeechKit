package meeting

import (
	"fmt"
	"strings"
)

// RenderTranscript writes the transcript the way the write-up prompt reads it:
// one line per segment, each labelled with the segment id a bullet cites, who
// spoke, and when. The id is what makes a claim in the notes checkable against
// what was actually said.
func RenderTranscript(lines []TranscriptLine) string {
	var out strings.Builder
	for _, line := range lines {
		text := strings.TrimSpace(line.Text)
		if text == "" {
			continue
		}
		speaker := speakerLabel(line)
		if speaker == "" {
			fmt.Fprintf(&out, "[%d] %s\n", line.SegmentID, text)
			continue
		}
		fmt.Fprintf(&out, "[%d] %s (%s): %s\n", line.SegmentID, speaker, FormatOffset(line.StartMs), text)
	}
	return strings.TrimSpace(out.String())
}

// speakerLabel names who a line came from in words a model can reason about.
// The channel split says which side spoke without any acoustic diarization; a
// meeting that later resolves individual speakers carries them here instead.
func speakerLabel(line TranscriptLine) string {
	switch speaker := strings.TrimSpace(line.Speaker); speaker {
	case "":
		switch line.Channel {
		case ChannelMicrophone:
			return "Me"
		case ChannelSystem:
			return "Them"
		default:
			return ""
		}
	case "me":
		return "Me"
	case "them":
		return "Them"
	default:
		return speaker
	}
}

// FormatOffset renders a point in the meeting as hh:mm:ss.
func FormatOffset(ms int64) string {
	if ms < 0 {
		ms = 0
	}
	total := ms / 1000
	return fmt.Sprintf("%02d:%02d:%02d", total/3600, (total%3600)/60, total%60)
}

// ChunkTranscript splits a transcript into pieces that fit a model's context.
//
// A budget of zero means the model can hold the whole meeting, which is the
// case for every cloud model and the reason the write-up normally happens in a
// single pass. A small local model gets the meeting in parts instead, split on
// segment boundaries so no utterance is cut in half.
func ChunkTranscript(lines []TranscriptLine, budgetTokens int) [][]TranscriptLine {
	if len(lines) == 0 {
		return nil
	}
	if budgetTokens <= 0 || EstimateTokens(lines) <= budgetTokens {
		return [][]TranscriptLine{lines}
	}

	chunks := make([][]TranscriptLine, 0, 4)
	current := make([]TranscriptLine, 0, 16)
	used := 0
	for _, line := range lines {
		cost := estimateLineTokens(line)
		if used+cost > budgetTokens && len(current) > 0 {
			chunks = append(chunks, current)
			current = make([]TranscriptLine, 0, 16)
			used = 0
		}
		current = append(current, line)
		used += cost
	}
	if len(current) > 0 {
		chunks = append(chunks, current)
	}
	return chunks
}

// EstimateTokens approximates how much of a context window a transcript takes.
// It is deliberately a rough character ratio: the point is to decide between
// one pass and several, and being a little pessimistic costs nothing while
// being wrong in the other direction truncates a meeting.
func EstimateTokens(lines []TranscriptLine) int {
	total := 0
	for _, line := range lines {
		total += estimateLineTokens(line)
	}
	return total
}

func estimateLineTokens(line TranscriptLine) int {
	// Roughly 3.5 characters per token, plus the per-line label overhead.
	return len(line.Text)*2/7 + 12
}
