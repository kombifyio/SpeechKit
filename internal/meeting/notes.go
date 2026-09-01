package meeting

import (
	"fmt"
	"strings"
)

// The note enhancement turns three things into one document: the transcript of
// what was said, the notes the user typed while it happened, and a template
// that says what this kind of meeting needs written down.
//
// The user's notes are not raw material to be rewritten. They are anchors: they
// say what mattered to the person who was there, and the transcript is searched
// for the context around them. A bullet that came from a note keeps the user's
// own words, and every generated bullet points back at the transcript it came
// from, so a reader can check any claim against what was actually said.

// Template describes what a kind of meeting needs written down.
type Template struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
	// Prompt frames the meeting for the model: what this kind of conversation
	// is and what a good write-up of it does.
	Prompt   string            `json:"prompt"`
	Sections []TemplateSection `json:"sections"`
}

// TemplateSection is one heading of the finished notes, with its own guidance.
type TemplateSection struct {
	Slug     string `json:"slug"`
	Title    string `json:"title"`
	Guidance string `json:"guidance"`
}

// NotesDocument is the enhanced write-up of one meeting.
type NotesDocument struct {
	TemplateSlug   string         `json:"templateSlug"`
	Locale         string         `json:"locale,omitempty"`
	ExecutiveBrief []string       `json:"executiveBrief,omitempty"`
	Sections       []NotesSection `json:"sections"`
}

func (d NotesDocument) Finalize(locale string) NotesDocument {
	d.Locale = strings.TrimSpace(locale)
	if len(d.ExecutiveBrief) > 5 {
		d.ExecutiveBrief = d.ExecutiveBrief[:5]
	}
	brief := d.ExecutiveBrief[:0]
	for _, sentence := range d.ExecutiveBrief {
		if sentence = strings.TrimSpace(sentence); sentence != "" {
			brief = append(brief, sentence)
		}
	}
	d.ExecutiveBrief = brief
	if len(d.ExecutiveBrief) == 0 {
		for _, section := range d.Sections {
			for _, bullet := range section.Bullets {
				if text := strings.TrimSpace(bullet.Text); text != "" {
					d.ExecutiveBrief = append(d.ExecutiveBrief, text)
					if len(d.ExecutiveBrief) == 5 {
						return d
					}
				}
			}
		}
	}
	return d
}

// NotesSection is one heading of the write-up.
type NotesSection struct {
	Slug    string        `json:"slug"`
	Title   string        `json:"title"`
	Bullets []NotesBullet `json:"bullets"`
}

// NotesBullet is one line of the write-up.
type NotesBullet struct {
	Text string `json:"text"`
	// SourceSegmentIDs are the transcript segments this bullet came from. A
	// reader can follow them back to what was actually said; a bullet with none
	// is one the model could not ground.
	SourceSegmentIDs []int64 `json:"sourceSegmentIds,omitempty"`
	// AnchorID names the user's own note this bullet came from. Bullets with an
	// anchor are rendered from the stored note rather than from the model's
	// paraphrase, so the user's words survive verbatim.
	AnchorID string `json:"anchorId,omitempty"`
	// Owner and Due are filled for action items where the conversation named
	// them; both stay empty rather than being guessed.
	Owner string `json:"owner,omitempty"`
	Due   string `json:"due,omitempty"`
}

// HasContent reports whether the document says anything at all.
func (d NotesDocument) HasContent() bool {
	for _, section := range d.Sections {
		if len(section.Bullets) > 0 {
			return true
		}
	}
	return false
}

// Markdown renders the document for copying, exporting and for the fallback
// path where a model could not produce structure.
func (d NotesDocument) Markdown() string {
	var out strings.Builder
	for _, section := range d.Sections {
		if len(section.Bullets) == 0 {
			continue
		}
		if out.Len() > 0 {
			out.WriteString("\n")
		}
		fmt.Fprintf(&out, "## %s\n\n", strings.TrimSpace(section.Title))
		for _, bullet := range section.Bullets {
			text := strings.TrimSpace(bullet.Text)
			if text == "" {
				continue
			}
			out.WriteString("- " + text)
			if owner := strings.TrimSpace(bullet.Owner); owner != "" {
				out.WriteString(" (" + owner)
				if due := strings.TrimSpace(bullet.Due); due != "" {
					out.WriteString(", " + due)
				}
				out.WriteString(")")
			} else if due := strings.TrimSpace(bullet.Due); due != "" {
				out.WriteString(" (" + due + ")")
			}
			out.WriteString("\n")
		}
	}
	return strings.TrimSpace(out.String())
}

// ApplyAnchors replaces the text of every anchored bullet with the note the
// user actually wrote.
//
// This is enforcement, not trust: a model asked to preserve wording will
// usually comply and occasionally tidy it up, and a tidied-up note is no longer
// the user's note. Anchors that the model dropped entirely are appended to the
// first section, because a note the user took must appear somewhere.
func (d NotesDocument) ApplyAnchors(anchors []Anchor) NotesDocument {
	if len(anchors) == 0 {
		return d
	}
	byID := make(map[string]Anchor, len(anchors))
	for _, anchor := range anchors {
		byID[anchor.ID] = anchor
	}

	used := map[string]bool{}
	sections := make([]NotesSection, 0, len(d.Sections))
	for _, section := range d.Sections {
		bullets := make([]NotesBullet, 0, len(section.Bullets))
		for _, bullet := range section.Bullets {
			if bullet.AnchorID != "" {
				anchor, ok := byID[bullet.AnchorID]
				if !ok {
					// An anchor id the model invented points at no note, so the
					// bullet is ordinary generated text.
					bullet.AnchorID = ""
					bullets = append(bullets, bullet)
					continue
				}
				if used[anchor.ID] {
					continue
				}
				used[anchor.ID] = true
				bullet.Text = anchor.Text
			}
			bullets = append(bullets, bullet)
		}
		section.Bullets = bullets
		sections = append(sections, section)
	}

	missing := make([]NotesBullet, 0)
	for _, anchor := range anchors {
		if !used[anchor.ID] {
			missing = append(missing, NotesBullet{Text: anchor.Text, AnchorID: anchor.ID})
		}
	}
	if len(missing) > 0 {
		if len(sections) == 0 {
			sections = append(sections, NotesSection{Slug: "notes", Title: "Notes"})
		}
		sections[0].Bullets = append(sections[0].Bullets, missing...)
	}
	d.Sections = sections
	return d
}

// Anchor is one note the user wrote, with the point in the meeting it was
// written at.
type Anchor struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	TsMs int64  `json:"tsMs"`
}

// TranscriptLine is one segment of what was said, numbered so bullets can cite
// it.
type TranscriptLine struct {
	SegmentID int64  `json:"segmentId"`
	Speaker   string `json:"speaker"`
	Channel   string `json:"channel"`
	StartMs   int64  `json:"startMs"`
	Text      string `json:"text"`
}

// The templates a meeting can be written up with. They are code rather than
// seeded rows so a stored write-up always names a template that still exists,
// and so improving the wording does not need a data migration. Custom templates
// have a table waiting for them.
const (
	TemplateDefaultMeeting = "default_meeting"
	TemplateOneOnOne       = "one_on_one"
	TemplateSales          = "sales"
	TemplateStandup        = "standup"
)

// SectionActionItems is the slug of the section action items live in. Callers
// that surface tasks separately look for it by name.
const SectionActionItems = "action_items"

var builtInTemplates = []Template{
	{
		Slug: TemplateDefaultMeeting,
		Name: "Meeting",
		Prompt: "A general work meeting. Write it up so someone who missed it knows what was " +
			"discussed, what was settled, and what happens next.",
		Sections: []TemplateSection{
			{Slug: "summary", Title: "Summary", Guidance: "Two or three sentences on what this meeting was about and where it landed."},
			{Slug: "discussion", Title: "Discussion", Guidance: "The points that were actually argued or explored, not a chronological retelling."},
			{Slug: "decisions", Title: "Decisions", Guidance: "What was settled. Only decisions that were genuinely made — leave this empty rather than inferring."},
			{Slug: SectionActionItems, Title: "Action items", Guidance: "What someone committed to do. Name the owner and the deadline only where they were stated."},
			{Slug: "open_questions", Title: "Open questions", Guidance: "What was raised and left unresolved."},
		},
	},
	{
		Slug:   TemplateOneOnOne,
		Name:   "1:1",
		Prompt: "A one-to-one between a person and their manager. Keep it personal and specific; this is a record for the two people in it.",
		Sections: []TemplateSection{
			{Slug: "summary", Title: "Summary", Guidance: "What this conversation was really about."},
			{Slug: "going_well", Title: "Going well", Guidance: "What was reported as working."},
			{Slug: "blockers", Title: "Blockers", Guidance: "What is in the way, in the words it was described with."},
			{Slug: "growth", Title: "Growth and feedback", Guidance: "Feedback given in either direction, and anything about development or career."},
			{Slug: SectionActionItems, Title: "Action items", Guidance: "What each person said they would do."},
		},
	},
	{
		Slug:   TemplateSales,
		Name:   "Sales call",
		Prompt: "A sales conversation with a prospect or customer. Write it up so the next call can start where this one ended.",
		Sections: []TemplateSection{
			{Slug: "summary", Title: "Summary", Guidance: "Who was on the call and where the deal stands after it."},
			{Slug: "needs", Title: "Needs and pain", Guidance: "The problems the customer described, in their own framing."},
			{Slug: "objections", Title: "Objections", Guidance: "What they pushed back on, including on price."},
			{Slug: "next_steps", Title: "Next steps", Guidance: "What was agreed as the next move, and by when."},
			{Slug: SectionActionItems, Title: "Action items", Guidance: "What we committed to, with owners where they were named."},
		},
	},
	{
		Slug:   TemplateStandup,
		Name:   "Stand-up",
		Prompt: "A short team stand-up. Keep it terse — one line per person is usually right.",
		Sections: []TemplateSection{
			{Slug: "updates", Title: "Updates", Guidance: "One line per person: what they did and what they are on next."},
			{Slug: "blockers", Title: "Blockers", Guidance: "Anything reported as blocked, and who can unblock it."},
			{Slug: SectionActionItems, Title: "Action items", Guidance: "Follow-ups that came out of the stand-up."},
		},
	},
}

// Templates returns the built-in templates.
func Templates() []Template {
	out := make([]Template, len(builtInTemplates))
	copy(out, builtInTemplates)
	return out
}

// TemplateBySlug returns the named template, falling back to the general
// meeting template so an unknown or empty slug still produces notes.
func TemplateBySlug(slug string) Template {
	slug = strings.TrimSpace(slug)
	for _, template := range builtInTemplates {
		if template.Slug == slug {
			return template
		}
	}
	return builtInTemplates[0]
}
