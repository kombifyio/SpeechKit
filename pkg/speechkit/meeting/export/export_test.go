package export_test

import (
	"context"
	"encoding/xml"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/meeting/export"
)

func sampleDocument() export.Document {
	return export.Document{
		Title:     `Q4 planning <script>alert(1)</script> & "budget"`,
		StartedAt: time.Date(2026, 10, 12, 9, 30, 0, 0, time.UTC),
		Locale:    "de-DE",
		Brief:     []string{"Budget approved."},
		Sections: []export.Section{{
			Title: "Decisions",
			Items: []export.Item{{Text: "Ship in November", Owner: "Ana", CitationIDs: []int64{42}}},
		}},
		Notes: []export.Note{
			{Text: "ask <Tim> about R&D\x01 budget", Offset: 4*time.Minute + 2*time.Second, Stamped: true},
			{Text: "line one\nline two"},
		},
		Citations:   []export.Citation{{SegmentID: 42, Offset: 12 * time.Minute, Speaker: "Ana", Text: "We ship in November."}},
		Screenshots: []export.Screenshot{{ID: "shot-1", Offset: 5 * time.Minute, Caption: "roadmap"}},
	}
}

// textOf parses the page strictly as XML and returns its character data,
// failing on anything that is not well-formed or outside the OneNote subset.
func textOf(t *testing.T, page string) (title, body string) {
	t.Helper()
	allowed := map[string]bool{"html": true, "head": true, "title": true, "meta": true, "body": true,
		"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true, "p": true, "ul": true,
		"ol": true, "li": true, "table": true, "tr": true, "td": true, "pre": true, "b": true, "i": true,
		"img": true, "object": true}
	decoder := xml.NewDecoder(strings.NewReader(page))
	decoder.Strict = true
	var stack []string
	var text strings.Builder
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("page is not well-formed XML: %v\n%s", err, page)
		}
		switch tok := token.(type) {
		case xml.StartElement:
			if !allowed[tok.Name.Local] {
				t.Fatalf("element <%s> is outside the OneNote subset", tok.Name.Local)
			}
			if tok.Name.Local == "img" {
				for _, attr := range tok.Attr {
					if attr.Name.Local == "src" && !strings.HasPrefix(attr.Value, "name:") {
						t.Fatalf("image src %q does not reference a part", attr.Value)
					}
				}
			}
			stack = append(stack, tok.Name.Local)
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) > 0 && stack[len(stack)-1] == "title" {
				title += string(tok)
			} else {
				text.Write(tok)
			}
		}
	}
	return title, text.String()
}

func TestXHTMLIsAWellFormedOneNotePageWithNotesVerbatimAndCitations(t *testing.T) {
	doc := sampleDocument()
	title, body := textOf(t, doc.Outline().XHTML())

	if title != doc.Title {
		t.Errorf("title = %q, want %q", title, doc.Title)
	}
	for _, want := range []string{
		"[04:02] ask <Tim> about R&D budget", // verbatim apart from the control character XML cannot carry
		"line one", "line two",
		"Ship in November (Ana) [1]",
		"12:00 Ana: We ship in November.",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page text lacks %q:\n%s", want, body)
		}
	}
}

func TestMarkdownCannotLoadAnything(t *testing.T) {
	doc := export.Document{
		Title: "Sync",
		Notes: []export.Note{{Text: `see ![x](https://tracker.example/p.png) and <img src="https://tracker.example">`}},
	}
	markdown := doc.Outline().Markdown()
	if strings.Contains(markdown, "![") || strings.Contains(markdown, "<img") {
		t.Fatalf("markdown still loads remote content:\n%s", markdown)
	}
}

func TestFingerprintFollowsContent(t *testing.T) {
	doc := sampleDocument()
	same := sampleDocument()
	changed := sampleDocument()
	changed.Notes[0].Text += "!"
	if doc.Fingerprint() != same.Fingerprint() {
		t.Error("equal documents have different fingerprints")
	}
	if doc.Fingerprint() == changed.Fingerprint() {
		t.Error("an edited note kept the fingerprint")
	}
}

func TestRegistryAnswersUnavailableForAnUnknownTarget(t *testing.T) {
	registry := export.NewRegistry()
	_, err := registry.Push(context.Background(), "onenote", sampleDocument(), export.Options{})
	if !errors.Is(err, export.ErrTargetUnavailable) {
		t.Fatalf("err = %v, want ErrTargetUnavailable", err)
	}
}
