package flows

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/ai/generation"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/meeting"
)

// budgetGenerator answers extraction with a handful of facts per chunk and
// synthesis with a valid document. It records every synthesis request and
// can refuse the first one with a context-limit error, the way llama-server
// does when prompt and answer together exceed its window.
type budgetGenerator struct {
	failFirstSynthesis bool
	synthesis          []generation.Request
	extractions        int
}

func (g *budgetGenerator) Models(context.Context, generation.ModelQuery) (generation.Catalog, error) {
	return generation.Catalog{Models: []generation.Model{{ID: "local", ContextWindowTokens: 4096}}}, nil
}

func (g *budgetGenerator) Generate(_ context.Context, request generation.Request) (generation.Result, error) {
	switch request.Purpose {
	case generation.PurposeMeetingExtraction:
		g.extractions++
		facts := []map[string]any{{"segmentId": 1, "text": "Budget approved for Q4."}, {"segmentId": 2, "text": "Launch moves to October."}}
		body, _ := json.Marshal(map[string]any{"facts": facts})
		return generation.Result{Text: string(body), Provider: "local", Model: "gemma"}, nil
	default:
		g.synthesis = append(g.synthesis, request)
		if g.failFirstSynthesis && len(g.synthesis) == 1 {
			return generation.Result{}, &generation.Error{Kind: generation.ErrorContextLimit, Operation: "generate", Err: errors.New("gemma error (500): provider context limit exceeded")}
		}
		return generation.Result{Text: `{"locale":"de","executiveBrief":["Budget approved."],"sections":[{"slug":"decisions","title":"Decisions","bullets":[{"text":"Ship in October","sourceSegmentIds":[2]}]}]}`, Provider: "local", Model: "gemma"}, nil
	}
}

func longTranscript(lines int) []meeting.TranscriptLine {
	out := make([]meeting.TranscriptLine, 0, lines)
	for index := 0; index < lines; index++ {
		out = append(out, meeting.TranscriptLine{SegmentID: int64(index + 1), Speaker: "Anna", Text: strings.Repeat("Wir besprechen das Budget und die nächsten Schritte für den Launch. ", 12)})
	}
	return out
}

// Meeting 24 (2026-09-03): rolling digests plus a long raw stretch filled the
// 4096 window, the synthesis asked for 1800 answer tokens on top, and the
// server ran out of context mid-answer. The prompt must leave room for the
// answer on the small window.
func TestGenerateMeetingNotesKeepsTheSynthesisPromptInsideTheWindow(t *testing.T) {
	generator := &budgetGenerator{}
	input := MeetingNotesInput{
		Locale: "de", Template: meeting.TemplateBySlug(meeting.TemplateDefaultMeeting),
		Transcript: longTranscript(40), ContextWindowTokens: 4096,
	}

	output, err := generateMeetingNotes(context.Background(), generator, input)
	if err != nil {
		t.Fatalf("generateMeetingNotes: %v", err)
	}
	if !output.Document.HasContent() || output.Passes < 3 {
		t.Fatalf("output = %+v, want a document produced in several passes", output)
	}
	if len(generator.synthesis) != 1 {
		t.Fatalf("synthesis calls = %d, want one", len(generator.synthesis))
	}
	request := generator.synthesis[0]
	wantOutput := meetingNotesOutputTokens(4096)
	if request.MaxOutputTokens != wantOutput {
		t.Fatalf("synthesis answer budget = %d, want %d for a 4096 window", request.MaxOutputTokens, wantOutput)
	}
	promptTokens := len(request.System+request.Prompt)/3 + 50
	if promptTokens+request.MaxOutputTokens > 4096 {
		t.Fatalf("synthesis prompt ≈%d tokens + answer %d exceeds the 4096 window", promptTokens, request.MaxOutputTokens)
	}
}

func TestGenerateMeetingNotesCondensesAgainAfterAContextLimit(t *testing.T) {
	generator := &budgetGenerator{failFirstSynthesis: true}
	input := MeetingNotesInput{
		Locale: "de", Template: meeting.TemplateBySlug(meeting.TemplateDefaultMeeting),
		Transcript: longTranscript(6), ContextWindowTokens: 4096,
	}

	output, err := generateMeetingNotes(context.Background(), generator, input)
	if err != nil {
		t.Fatalf("generateMeetingNotes after a context limit: %v", err)
	}
	if !output.Document.HasContent() {
		t.Fatal("no document after the retry")
	}
	if len(generator.synthesis) != 2 {
		t.Fatalf("synthesis calls = %d, want the failed one and one retry", len(generator.synthesis))
	}
	if generator.synthesis[1].MaxOutputTokens >= generator.synthesis[0].MaxOutputTokens {
		t.Fatalf("retry answer budget %d must be smaller than the first %d", generator.synthesis[1].MaxOutputTokens, generator.synthesis[0].MaxOutputTokens)
	}
	if generator.extractions == 0 {
		t.Fatal("the retry must condense the input before asking again")
	}
}

func TestMeetingNotesBudgetsScaleWithTheWindow(t *testing.T) {
	if got := meetingNotesOutputTokens(4096); got != 1228 {
		t.Fatalf("output tokens for 4096 = %d, want 1228", got)
	}
	if got := meetingNotesOutputTokens(32768); got != 1800 {
		t.Fatalf("output tokens for a roomy model = %d, want the 1800 cap", got)
	}
	if got := meetingNotesOutputTokens(0); got != 1800 {
		t.Fatalf("output tokens for an unknown window = %d, want 1800", got)
	}
	if got := meetingNotesSynthesisBudget(4096, 1228); got != 4096-1228-meetingNotesPromptReserveTokens {
		t.Fatalf("synthesis budget for 4096 = %d", got)
	}
	if got := meetingNotesSynthesisBudget(0, 1800); got != 0 {
		t.Fatalf("unknown window must not condense: %d", got)
	}
}
