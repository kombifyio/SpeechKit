package assemblyai

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
)

type assemblyAIStreamingTurn struct {
	Type                string                    `json:"type"`
	Transcript          string                    `json:"transcript"`
	EndOfTurn           bool                      `json:"end_of_turn"`
	TurnIsFormatted     bool                      `json:"turn_is_formatted"`
	TurnOrder           int64                     `json:"turn_order"`
	EndOfTurnConfidence float64                   `json:"end_of_turn_confidence"`
	Speaker             string                    `json:"speaker"`
	SpeakerLabel        string                    `json:"speaker_label"`
	Words               []assemblyAIStreamingWord `json:"words"`
	Data                json.RawMessage           `json:"data"`
}

func assemblyAILLMGatewayContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var payload struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	if len(payload.Choices) == 0 {
		return ""
	}
	return strings.TrimSpace(payload.Choices[0].Message.Content)
}

// dictationEvent maps a Turn event onto the provider-neutral dictation event.
// With format_turns=true the formatted turn (end_of_turn && turn_is_formatted)
// is the single final per turn; the unformatted end-of-turn event stays a
// draft so hosts never commit the same turn twice.
//
// diarize mirrors the speaker_labels query param: the provider only sends
// labels when asked, and a caller who did not ask must not receive them.
func (t assemblyAIStreamingTurn) dictationEvent(provider, model, language string, sessionID uint64, sequence int64, diarize bool) speechkit.DictationStreamEvent {
	event := speechkit.DictationStreamEvent{
		Sequence:       sequence,
		SessionID:      sessionID,
		SegmentID:      uint64(t.TurnOrder) + 1, // #nosec G115 -- provider turn_order is a small non-negative counter.
		ProviderItemID: fmt.Sprintf("assemblyai:%d", t.TurnOrder),
		Text:           strings.TrimSpace(t.Transcript),
		IsFinal:        t.EndOfTurn && t.TurnIsFormatted,
		Language:       language,
		Provider:       provider,
		Model:          model,
	}
	if len(t.Words) > 0 {
		event.Words = make([]speechkit.WordConfidence, 0, len(t.Words))
		total := 0.0
		for _, word := range t.Words {
			event.Words = append(event.Words, speechkit.WordConfidence{
				Text:       word.Text,
				Confidence: word.Confidence,
				StartMs:    word.Start,
				EndMs:      word.End,
			})
			total += word.Confidence
		}
		event.Confidence = total / float64(len(t.Words))
	}
	if diarize {
		event.Speakers = t.dictationSpeakers(provider, model, language)
	}
	return event
}

// dictationSpeakers builds the provider-neutral diarization result from a v3
// Turn's speaker_labels output. Dictation carries no KnownSpeakers, so this is
// label-level only — mapping labels onto people (PersonID/DisplayName/Role)
// stays in the speaker-stream path via assemblyAIStreamingIdentity.
func (t assemblyAIStreamingTurn) dictationSpeakers(provider, model, language string) *speaker.DiarizationResult {
	turnSpeaker := stt.FirstNonEmptyTrimmed(t.SpeakerLabel, t.Speaker)
	words := make([]speaker.SpeakerWord, 0, len(t.Words))
	for _, word := range t.Words {
		words = append(words, speaker.SpeakerWord{
			Text:              word.Text,
			StartMs:           word.Start,
			EndMs:             word.End,
			Confidence:        word.Confidence,
			SpeakerLabel:      speaker.NormalizeSpeakerLabel(stt.FirstNonEmptyTrimmed(word.SpeakerLabel, word.Speaker, turnSpeaker)),
			SpeakerConfidence: word.SpeakerConfidence,
		})
	}
	segments := speaker.BuildSegmentsFromWords(words)
	if len(segments) == 0 && speaker.NormalizeSpeakerLabel(turnSpeaker) == "" {
		// Asked for labels, got none on this frame — report nothing rather
		// than an empty result that reads as "one anonymous speaker".
		return nil
	}
	return &speaker.DiarizationResult{
		Provider: provider,
		Model:    model,
		Level:    speaker.IdentificationDiarization,
		Text:     strings.TrimSpace(t.Transcript),
		Language: language,
		Speakers: speaker.SpeakersFromSegments(segments),
		Segments: segments,
		Words:    words,
	}
}

type assemblyAIStreamingWord struct {
	Text              string  `json:"text"`
	Start             int64   `json:"start"`
	End               int64   `json:"end"`
	Confidence        float64 `json:"confidence"`
	Speaker           string  `json:"speaker"`
	SpeakerLabel      string  `json:"speaker_label"`
	SpeakerConfidence float64 `json:"speaker_confidence"`
}

func (t assemblyAIStreamingTurn) speakerFrame(provider, model string, sequence, latencyMs int64, opts speaker.Options) speaker.SpeakerFrame {
	turnSpeaker := stt.FirstNonEmptyTrimmed(t.SpeakerLabel, t.Speaker)
	words := make([]speaker.SpeakerWord, 0, len(t.Words))
	for _, word := range t.Words {
		raw := stt.FirstNonEmptyTrimmed(word.SpeakerLabel, word.Speaker, turnSpeaker)
		label, personID, displayName, role := assemblyAIStreamingIdentity(raw, opts)
		words = append(words, speaker.SpeakerWord{
			Text:                  word.Text,
			StartMs:               word.Start,
			EndMs:                 word.End,
			Confidence:            word.Confidence,
			SpeakerLabel:          label,
			SpeakerConfidence:     word.SpeakerConfidence,
			PersonID:              personID,
			DisplayName:           displayName,
			Role:                  role,
			AttributionConfidence: assemblyAIAttributionConfidence(displayName, role, word.SpeakerConfidence),
		})
	}
	frame := speaker.FrameFromWords(provider, model, sequence, t.Transcript, t.EndOfTurn || t.TurnIsFormatted, words, latencyMs)
	if frame.Segment == nil {
		label, personID, displayName, role := assemblyAIStreamingIdentity(turnSpeaker, opts)
		if label != "" || personID != "" || displayName != "" || role != "" || strings.TrimSpace(t.Transcript) != "" {
			frame.Segment = &speaker.SpeakerSegment{
				Text:         strings.TrimSpace(t.Transcript),
				SpeakerLabel: label,
				PersonID:     personID,
				DisplayName:  displayName,
				Role:         role,
			}
			frame.Speakers = speaker.SpeakersFromSegments([]speaker.SpeakerSegment{*frame.Segment})
		}
	}
	return frame
}

func assemblyAIStreamingIdentity(raw string, opts speaker.Options) (label, personID, displayName, role string) {
	raw = strings.TrimSpace(raw)
	label = speaker.NormalizeSpeakerLabel(raw)
	if label == "" {
		return "", "", "", ""
	}
	if !opts.AllowProviderMapping || len(opts.KnownSpeakers) == 0 {
		return label, "", "", ""
	}
	idx := assemblyAISpeakerOrdinal(raw)
	if idx < 0 || idx >= len(opts.KnownSpeakers) {
		return label, "", "", ""
	}
	known := opts.KnownSpeakers[idx]
	return label, known.ID, known.DisplayName, known.Role
}

func assemblyAISpeakerOrdinal(raw string) int {
	raw = strings.TrimSpace(strings.TrimPrefix(speaker.NormalizeSpeakerLabel(raw), "speaker_"))
	if len(raw) != 1 {
		return -1
	}
	ch := raw[0]
	switch {
	case ch >= 'A' && ch <= 'Z':
		return int(ch - 'A')
	case ch >= 'a' && ch <= 'z':
		return int(ch - 'a')
	default:
		return -1
	}
}
