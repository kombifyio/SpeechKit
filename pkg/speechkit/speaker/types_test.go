package speaker

import "testing"

func TestOptionsWantsDiarizationFromSpeakerBounds(t *testing.T) {
	opts := Options{MinSpeakersExpected: 2, MaxSpeakersExpected: 4}
	if !opts.WantsDiarization() {
		t.Fatal("expected speaker bounds to request diarization")
	}
}

func TestOptionsNormalizedDefaultsSpeakerTypeForIdentification(t *testing.T) {
	opts := Options{KnownValues: []string{"Alice", " Alice ", ""}}.Normalized()
	if opts.SpeakerType != SpeakerTypeName {
		t.Fatalf("SpeakerType = %q, want %q", opts.SpeakerType, SpeakerTypeName)
	}
	if len(opts.KnownValues) != 1 || opts.KnownValues[0] != "Alice" {
		t.Fatalf("KnownValues = %#v", opts.KnownValues)
	}
}

func TestBuildSegmentsFromWordsGroupsContiguousSpeakers(t *testing.T) {
	words := []SpeakerWord{
		{Text: "hello", StartMs: 0, EndMs: 100, SpeakerLabel: "speaker_0", SpeakerConfidence: 0.8},
		{Text: "there", StartMs: 120, EndMs: 200, SpeakerLabel: "speaker_0", SpeakerConfidence: 0.6},
		{Text: "yes", StartMs: 220, EndMs: 260, SpeakerLabel: "speaker_1", SpeakerConfidence: 0.9},
	}

	segments := BuildSegmentsFromWords(words)
	if len(segments) != 2 {
		t.Fatalf("segments = %d, want 2", len(segments))
	}
	if segments[0].Text != "hello there" || segments[0].SpeakerLabel != "speaker_0" {
		t.Fatalf("first segment = %+v", segments[0])
	}
	if segments[1].Text != "yes" || segments[1].SpeakerLabel != "speaker_1" {
		t.Fatalf("second segment = %+v", segments[1])
	}
}

func TestNormalizeSpeakerLabelTreatsUnknownAsUnattributed(t *testing.T) {
	for _, raw := range []any{"UNKNOWN", "unknown", "speaker_UNKNOWN", "speaker unknown", "unattributed"} {
		if got := NormalizeSpeakerLabel(raw); got != "" {
			t.Fatalf("NormalizeSpeakerLabel(%v) = %q, want empty", raw, got)
		}
	}
}

func TestFrameFromWordsBuildsSegmentAndSpeakers(t *testing.T) {
	frame := FrameFromWords("deepgram", "nova-3", 7, "hello", true, []SpeakerWord{
		{Text: "hello", StartMs: 0, EndMs: 100, SpeakerLabel: "speaker_0", SpeakerConfidence: 0.8},
	}, 42)

	if frame.Provider != "deepgram" || frame.Model != "nova-3" || frame.Sequence != 7 || !frame.IsFinal {
		t.Fatalf("frame metadata = %+v", frame)
	}
	if frame.Segment == nil || frame.Segment.SpeakerLabel != "speaker_0" {
		t.Fatalf("frame segment = %+v", frame.Segment)
	}
	if len(frame.Speakers) != 1 || frame.Speakers[0].Label != "speaker_0" {
		t.Fatalf("frame speakers = %+v", frame.Speakers)
	}
}

func TestSpeakersFromSegmentsKeepsKnownAttribution(t *testing.T) {
	segments := []SpeakerSegment{
		{SpeakerLabel: "speaker_A", DisplayName: "Alice", AttributionConfidence: 0.7},
		{SpeakerLabel: "speaker_A", DisplayName: "Alice", AttributionConfidence: 0.9},
		{SpeakerLabel: "speaker_B"},
	}

	speakers := SpeakersFromSegments(segments)
	if len(speakers) != 2 {
		t.Fatalf("speakers = %d, want 2", len(speakers))
	}
	if speakers[0].DisplayName != "Alice" {
		t.Fatalf("first speaker = %+v", speakers[0])
	}
	if speakers[1].Label != "speaker_B" {
		t.Fatalf("second speaker = %+v", speakers[1])
	}
}
