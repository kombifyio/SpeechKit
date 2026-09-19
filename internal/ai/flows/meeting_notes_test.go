package flows

import (
	"context"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/ai/generation"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/meeting"
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

type pinnedRecorder struct {
	requests []generation.Request
}

func (r *pinnedRecorder) Generate(_ context.Context, request generation.Request) (generation.Result, error) {
	r.requests = append(r.requests, request)
	if request.Purpose == generation.PurposeMeetingExtraction {
		return generation.Result{Text: `{"facts":[{"segmentId":1,"text":"We shipped."}]}`}, nil
	}
	return generation.Result{Text: `{"sections":[{"slug":"summary","title":"Summary","bullets":[{"text":"We shipped."}]}]}`}, nil
}

func (r *pinnedRecorder) Models(context.Context, generation.ModelQuery) (generation.Catalog, error) {
	return generation.Catalog{Models: []generation.Model{
		{ID: "github_copilot/gpt-5.6-luna", ContextWindowTokens: 128000},
		{ID: "local/gemma", ContextWindowTokens: 1200},
	}}, nil
}

// A write-up pinned to one model sends every pass to that model and sizes its
// chunks for that model's window, not for the first model of the chain.
func TestMeetingNotesPinnedRunStaysOnItsModel(t *testing.T) {
	recorder := &pinnedRecorder{}
	flow := DefineMeetingNotesFlowWithGenerator(recorder)
	transcript := make([]meeting.TranscriptLine, 0, 60)
	for index := range 60 {
		transcript = append(transcript, meeting.TranscriptLine{SegmentID: int64(index + 1), Text: strings.Repeat("Wir besprechen den Launch und die offenen Punkte. ", 4)})
	}

	if _, err := flow.Run(context.Background(), MeetingNotesInput{ModelID: "local/gemma", Transcript: transcript}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	extraction := 0
	for _, request := range recorder.requests {
		if request.ModelID != "local/gemma" {
			t.Fatalf("request for %q left the pinned model", request.ModelID)
		}
		if request.Purpose == generation.PurposeMeetingExtraction {
			extraction++
		}
	}
	if extraction == 0 {
		t.Fatal("the transcript was not condensed for the pinned model's small window")
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
