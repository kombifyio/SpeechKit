package flows

import (
	"testing"

	"github.com/kombifyio/SpeechKit/internal/meeting"
)

// Models return JSON wrapped in whatever they feel like: a code fence, a
// sentence of preamble, or both. Refusing those answers would fail a meeting
// write-up over formatting.
func TestDecodeModelJSONReadsAnswersModelsActuallyGive(t *testing.T) {
	cases := map[string]string{
		"bare":               `{"sections":[{"slug":"summary","title":"Summary","bullets":[{"text":"We shipped."}]}]}`,
		"fenced":             "```json\n{\"sections\":[{\"slug\":\"summary\",\"title\":\"Summary\",\"bullets\":[{\"text\":\"We shipped.\"}]}]}\n```",
		"with preamble":      "Here are the notes:\n{\"sections\":[{\"slug\":\"summary\",\"title\":\"Summary\",\"bullets\":[{\"text\":\"We shipped.\"}]}]}",
		"with trailing text": `{"sections":[{"slug":"summary","title":"Summary","bullets":[{"text":"We shipped."}]}]}  Hope that helps!`,
	}

	for name, answer := range cases {
		t.Run(name, func(t *testing.T) {
			var document meeting.NotesDocument
			if err := decodeModelJSON(answer, &document); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if !document.HasContent() {
				t.Fatal("decoded an empty document")
			}
		})
	}
}

func TestDecodeModelJSONRejectsAnAnswerWithNoJSON(t *testing.T) {
	var document meeting.NotesDocument
	if err := decodeModelJSON("I could not hear the meeting.", &document); err == nil {
		t.Fatal("prose was accepted as JSON")
	}
}

// A model that will not produce JSON should still leave the user with notes,
// clearly marked as carrying no provenance rather than faked into structure.
func TestMeetingNotesFromProseKeepsTheAnswer(t *testing.T) {
	template := meeting.TemplateBySlug(meeting.TemplateDefaultMeeting)

	document := meetingNotesFromProse(template, "- We agreed on the launch date\n- Legal still needs to review")

	if len(document.Sections) != 1 || len(document.Sections[0].Bullets) != 2 {
		t.Fatalf("prose salvage produced %+v", document)
	}
	if document.Sections[0].Bullets[0].Text != "We agreed on the launch date" {
		t.Fatalf("bullet markers were left in: %q", document.Sections[0].Bullets[0].Text)
	}
	for _, bullet := range document.Sections[0].Bullets {
		if len(bullet.SourceSegmentIDs) != 0 {
			t.Fatal("prose salvage invented provenance it does not have")
		}
	}
}

func TestMeetingNotesTranscriptBudgetLeavesRoomForTheAnswer(t *testing.T) {
	if budget := meetingNotesTranscriptBudget(0); budget != 0 {
		t.Fatalf("an unknown context window should mean one pass, got budget %d", budget)
	}
	budget := meetingNotesTranscriptBudget(4096)
	if budget >= 4096 {
		t.Fatalf("budget %d leaves no room for the template, notes and answer", budget)
	}
}
