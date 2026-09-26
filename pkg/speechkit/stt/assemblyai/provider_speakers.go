package assemblyai

import (
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
)

func (r assemblyAITranscriptResponse) diarizationResult(provider, model, language string, opts speaker.Options) *speaker.DiarizationResult {
	segments := make([]speaker.SpeakerSegment, 0, len(r.Utterances))
	var words []speaker.SpeakerWord
	mapping := r.speakerIdentificationMapping()
	for _, utterance := range r.Utterances {
		label, personID, displayName, role := assemblyAISpeakerIdentity(utterance.Speaker, opts, mapping)
		segment := speaker.SpeakerSegment{
			Text:                  strings.TrimSpace(utterance.Text),
			StartMs:               utterance.Start,
			EndMs:                 utterance.End,
			SpeakerLabel:          label,
			SpeakerConfidence:     utterance.Confidence,
			PersonID:              personID,
			DisplayName:           displayName,
			Role:                  role,
			AttributionConfidence: assemblyAIAttributionConfidence(displayName, role, utterance.Confidence),
		}
		for _, word := range utterance.Words {
			wordLabel, wordPersonID, wordDisplayName, wordRole := assemblyAISpeakerIdentity(stt.FirstNonEmptyTrimmed(word.Speaker, utterance.Speaker), opts, mapping)
			sw := speaker.SpeakerWord{
				Text:                  word.Text,
				StartMs:               word.Start,
				EndMs:                 word.End,
				Confidence:            word.Confidence,
				SpeakerLabel:          wordLabel,
				SpeakerConfidence:     utterance.Confidence,
				PersonID:              wordPersonID,
				DisplayName:           wordDisplayName,
				Role:                  wordRole,
				AttributionConfidence: assemblyAIAttributionConfidence(wordDisplayName, wordRole, utterance.Confidence),
			}
			segment.Words = append(segment.Words, sw)
			words = append(words, sw)
		}
		segments = append(segments, segment)
	}
	level := speaker.IdentificationDiarization
	if opts.WantsIdentification() {
		level = speaker.IdentificationProviderID
	}
	return &speaker.DiarizationResult{
		Provider: provider,
		Model:    model,
		Level:    level,
		Text:     strings.TrimSpace(r.Text),
		Language: language,
		Speakers: speaker.SpeakersFromSegments(segments),
		Segments: segments,
		Words:    words,
	}
}

func (r assemblyAITranscriptResponse) speakerIdentificationMapping() map[string]string {
	if r.SpeechUnderstanding == nil {
		return nil
	}
	return r.SpeechUnderstanding.Response.SpeakerIdentification.Mapping
}

func assemblyAISpeakerIdentity(raw string, opts speaker.Options, mapping map[string]string) (label, personID, displayName, role string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", "", ""
	}
	label = speaker.NormalizeSpeakerLabel(raw)
	if mapped := assemblyAIMappedSpeaker(raw, label, mapping); mapped != "" {
		if opts.SpeakerType == speaker.SpeakerTypeRole {
			role = mapped
		} else {
			displayName = mapped
		}
		personID = assemblyAIKnownSpeakerID(opts, displayName, role)
		return label, personID, displayName, role
	}
	if opts.WantsIdentification() && knownValueContains(opts, raw) {
		if opts.SpeakerType == speaker.SpeakerTypeRole {
			role = raw
		} else {
			displayName = raw
		}
		personID = assemblyAIKnownSpeakerID(opts, displayName, role)
		return label, personID, displayName, role
	}
	return label, "", "", ""
}

func assemblyAIAttributionConfidence(displayName, role string, confidence float64) float64 {
	if displayName == "" && role == "" {
		return 0
	}
	return confidence
}

func assemblyAIKnownValues(opts speaker.Options) []string {
	values := append([]string(nil), opts.KnownValues...)
	for _, known := range opts.KnownSpeakers {
		switch opts.SpeakerType {
		case speaker.SpeakerTypeRole:
			values = append(values, known.Role)
		default:
			values = append(values, known.DisplayName)
		}
	}
	return speaker.Options{KnownValues: values}.Normalized().KnownValues
}

func assemblyAIKnownSpeakers(opts speaker.Options) []assemblyAISpeakerMetadata {
	opts = opts.Normalized()
	out := make([]assemblyAISpeakerMetadata, 0, len(opts.KnownSpeakers))
	for _, known := range opts.KnownSpeakers {
		item := assemblyAISpeakerMetadata{
			ID:          known.ID,
			Description: known.Description,
		}
		if opts.SpeakerType == speaker.SpeakerTypeRole {
			item.Role = known.Role
		} else {
			item.Name = known.DisplayName
		}
		if item.ID == "" && item.Name == "" && item.Role == "" && item.Description == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}

func assemblyAIMappedSpeaker(raw, label string, mapping map[string]string) string {
	if len(mapping) == 0 {
		return ""
	}
	candidates := []string{raw, label}
	if strings.HasPrefix(label, "speaker_") {
		candidates = append(candidates, strings.TrimPrefix(label, "speaker_"))
	}
	for _, candidate := range candidates {
		if value := strings.TrimSpace(mapping[candidate]); value != "" {
			return value
		}
	}
	return ""
}

func assemblyAIKnownSpeakerID(opts speaker.Options, displayName, role string) string {
	displayName = strings.TrimSpace(displayName)
	role = strings.TrimSpace(role)
	if displayName == "" && role == "" {
		return ""
	}
	for _, known := range opts.Normalized().KnownSpeakers {
		if displayName != "" && strings.EqualFold(known.DisplayName, displayName) {
			return known.ID
		}
		if role != "" && strings.EqualFold(known.Role, role) {
			return known.ID
		}
	}
	return ""
}

func knownValueContains(opts speaker.Options, value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for _, known := range assemblyAIKnownValues(opts) {
		if strings.EqualFold(known, value) {
			return true
		}
	}
	return false
}
