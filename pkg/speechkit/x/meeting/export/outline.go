package export

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// BlockKind names one structural element of an Outline.
type BlockKind string

// The block kinds an Outline is built from.
const (
	BlockHeading   BlockKind = "heading"
	BlockParagraph BlockKind = "paragraph"
	BlockList      BlockKind = "list"
	BlockImage     BlockKind = "image"
)

// Block is one element of an Outline. Which fields matter depends on Kind:
// a heading has Level (1 to 6) and Text, a paragraph Text and Emphasis, a
// list Ordered and Items, an image ImageID and Alt.
type Block struct {
	Kind     BlockKind `json:"kind"`
	Level    int       `json:"level,omitempty"`
	Text     string    `json:"text,omitempty"`
	Emphasis bool      `json:"emphasis,omitempty"`
	Ordered  bool      `json:"ordered,omitempty"`
	Items    []string  `json:"items,omitempty"`
	ImageID  string    `json:"imageId,omitempty"`
	Alt      string    `json:"alt,omitempty"`
}

// Outline is the structure a Document renders to. It is built, never parsed
// from Markdown, so every renderer sees the same blocks; a target may also
// split it (a slice of Blocks is an Outline of its own).
type Outline struct {
	Title   string    `json:"title"`
	Created time.Time `json:"created,omitzero"`
	Locale  string    `json:"locale,omitempty"`
	Blocks  []Block   `json:"blocks"`
}

// Outline builds the structure of the document: date and language, the
// brief, the review sections with citation markers, the user's notes
// verbatim, the cited transcript lines and the screenshots.
func (d Document) Outline() Outline {
	labels := d.Labels.withDefaults()
	title := strings.TrimSpace(d.Title)
	if title == "" {
		title = "Meeting notes"
	}
	out := Outline{Title: title, Created: d.StartedAt, Locale: strings.TrimSpace(d.Locale)}
	meta := make([]string, 0, 2)
	if !d.StartedAt.IsZero() {
		meta = append(meta, d.StartedAt.Format("2006-01-02 15:04"))
	}
	if out.Locale != "" {
		meta = append(meta, out.Locale)
	}
	if len(meta) > 0 {
		out.Blocks = append(out.Blocks, Block{Kind: BlockParagraph, Text: strings.Join(meta, " · "), Emphasis: true})
	}

	if brief := nonBlank(d.Brief); len(brief) > 0 {
		out.Blocks = append(out.Blocks,
			Block{Kind: BlockHeading, Level: 2, Text: labels.Brief},
			Block{Kind: BlockList, Items: brief})
	}

	citationNumber := make(map[int64]int, len(d.Citations))
	for index, citation := range d.Citations {
		if _, seen := citationNumber[citation.SegmentID]; !seen {
			citationNumber[citation.SegmentID] = index + 1
		}
	}
	for _, section := range d.Sections {
		items := make([]string, 0, len(section.Items))
		for _, item := range section.Items {
			if text := itemText(item, citationNumber); text != "" {
				items = append(items, text)
			}
		}
		if len(items) == 0 {
			continue
		}
		out.Blocks = append(out.Blocks,
			Block{Kind: BlockHeading, Level: 2, Text: strings.TrimSpace(section.Title)},
			Block{Kind: BlockList, Items: items})
	}

	notes := make([]string, 0, len(d.Notes))
	for _, note := range d.Notes {
		if strings.TrimSpace(note.Text) == "" {
			continue
		}
		// The user's words are kept as typed; only the stamp is added.
		if note.Stamped {
			notes = append(notes, "["+FormatOffset(note.Offset)+"] "+note.Text)
		} else {
			notes = append(notes, note.Text)
		}
	}
	if len(notes) > 0 {
		out.Blocks = append(out.Blocks,
			Block{Kind: BlockHeading, Level: 2, Text: labels.Notes},
			Block{Kind: BlockList, Items: notes})
	}

	if len(d.Citations) > 0 {
		lines := make([]string, 0, len(d.Citations))
		for _, citation := range d.Citations {
			line := FormatOffset(citation.Offset)
			if speaker := strings.TrimSpace(citation.Speaker); speaker != "" {
				line += " " + speaker + ":"
			}
			lines = append(lines, line+" "+strings.TrimSpace(citation.Text))
		}
		out.Blocks = append(out.Blocks,
			Block{Kind: BlockHeading, Level: 2, Text: labels.Citations},
			Block{Kind: BlockList, Ordered: true, Items: lines})
	}

	if len(d.Screenshots) > 0 {
		out.Blocks = append(out.Blocks, Block{Kind: BlockHeading, Level: 2, Text: labels.Screenshots})
		for _, shot := range d.Screenshots {
			label := labels.Screenshot + " · " + FormatOffset(shot.Offset)
			if caption := strings.TrimSpace(shot.Caption); caption != "" {
				label += " · " + caption
			}
			out.Blocks = append(out.Blocks,
				Block{Kind: BlockParagraph, Text: label},
				Block{Kind: BlockImage, ImageID: shot.ID, Alt: label})
		}
	}
	return out
}

func itemText(item Item, citationNumber map[int64]int) string {
	text := strings.TrimSpace(item.Text)
	if text == "" {
		return ""
	}
	owner, due := strings.TrimSpace(item.Owner), strings.TrimSpace(item.Due)
	switch {
	case owner != "" && due != "":
		text += " (" + owner + ", " + due + ")"
	case owner != "":
		text += " (" + owner + ")"
	case due != "":
		text += " (" + due + ")"
	}
	refs := make([]string, 0, len(item.CitationIDs))
	for _, id := range item.CitationIDs {
		if number, ok := citationNumber[id]; ok {
			refs = append(refs, fmt.Sprintf("%d", number))
		}
	}
	if len(refs) > 0 {
		text += " [" + strings.Join(refs, ", ") + "]"
	}
	return text
}

func nonBlank(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}

// FormatOffset renders an offset from the meeting start as m:ss, or h:mm:ss
// from the first hour on.
func FormatOffset(offset time.Duration) string {
	if offset < 0 {
		offset = 0
	}
	total := int64(offset / time.Second)
	hours, minutes, seconds := total/3600, (total/60)%60, total%60
	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, seconds)
	}
	return fmt.Sprintf("%02d:%02d", minutes, seconds)
}

// ImageIDs lists the image ids of the outline in order.
func (o Outline) ImageIDs() []string {
	ids := make([]string, 0)
	for _, block := range o.Blocks {
		if block.Kind == BlockImage {
			ids = append(ids, block.ImageID)
		}
	}
	return ids
}

// ImagePartName is the multipart part name the XHTML renderer references an
// image by ("name:<part>"). It is derived from the id, so a target that
// attaches the bytes finds the name without a lookup table, and it contains
// only letters and digits whatever the id holds.
func ImagePartName(id string) string {
	sum := sha256.Sum256([]byte(id))
	return "img" + hex.EncodeToString(sum[:8])
}
