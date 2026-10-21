// Package export is the notes export SPI of Meeting Mode: one Document that
// describes a finished meeting, a structural Outline built from it, Markdown
// and XHTML renderers of that outline, and the Target interface every sync
// destination implements (a Markdown file, OneNote, Notion or a host's own).
//
// The package knows nothing about any destination. A target receives the
// Document and the binary Parts it references, and answers with a Receipt
// that names what it wrote and the content fingerprint it wrote, so a host
// can skip a re-push of unchanged content without asking the destination.
//
// Everything in a Document is untrusted text: a model wrote the review after
// listening to the meeting, and a title can come from a window or a calendar
// subject. The renderers escape it; neither output can load anything.
//
// Stability: Beta — API-checked; may change with a changelog callout.
package export

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

// Sentinel errors a Target wraps so a host can react without knowing the
// destination. Test with errors.Is.
var (
	// ErrTargetUnavailable: the target is not registered, not connected, or
	// not allowed in this deployment.
	ErrTargetUnavailable = errors.New("export: target unavailable")
	// ErrDestinationGone: the destination (a section, a page, a folder) no
	// longer exists. The host decides whether to pick a new one.
	ErrDestinationGone = errors.New("export: destination gone")
	// ErrDestinationFull: the destination refuses more content.
	ErrDestinationFull = errors.New("export: destination full")
	// ErrTooLarge: the document or one of its parts exceeds what the
	// destination accepts, even after fitting.
	ErrTooLarge = errors.New("export: too large")
	// ErrDestinationNotAllowed: the destination lies outside what this
	// deployment lets the target write to (a policy-pinned notebook scope).
	// A target answers it before it sends anything.
	ErrDestinationNotAllowed = errors.New("export: destination not allowed")
)

// Document is one finished meeting as an export sees it.
type Document struct {
	Title     string    `json:"title"`
	StartedAt time.Time `json:"startedAt"`
	// Locale is the BCP-47 tag of the review's language.
	Locale string `json:"locale,omitempty"`
	// Brief is the executive brief: a handful of sentences.
	Brief []string `json:"brief,omitempty"`
	// Sections are the review's headings in order.
	Sections []Section `json:"sections,omitempty"`
	// Notes are what the user typed, reproduced verbatim.
	Notes []Note `json:"notes,omitempty"`
	// Citations are the transcript lines the review cites.
	Citations []Citation `json:"citations,omitempty"`
	// Screenshots reference binary parts by id; the bytes travel in
	// Options.Parts.
	Screenshots []Screenshot `json:"screenshots,omitempty"`
	// Labels are the headings the renderers add. Zero fields fall back to
	// English, so a host passes the catalog strings for Locale.
	Labels Labels `json:"labels,omitzero"`
}

// Section is one heading of the review.
type Section struct {
	Title string `json:"title"`
	Items []Item `json:"items,omitempty"`
}

// Item is one bullet of a review section.
type Item struct {
	Text string `json:"text"`
	// Owner and Due are set for action items where the meeting named them.
	Owner string `json:"owner,omitempty"`
	Due   string `json:"due,omitempty"`
	// CitationIDs are the SegmentIDs of the Document's Citations this bullet
	// came from.
	CitationIDs []int64 `json:"citationIds,omitempty"`
}

// Note is one note the user wrote. Stamped is false for a note taken before
// stamps existed; it keeps no invented time.
type Note struct {
	Text    string        `json:"text"`
	Offset  time.Duration `json:"offset,omitempty"`
	Stamped bool          `json:"stamped,omitempty"`
}

// Citation is one transcript line, with its offset from the meeting start.
type Citation struct {
	SegmentID int64         `json:"segmentId"`
	Offset    time.Duration `json:"offset"`
	Speaker   string        `json:"speaker,omitempty"`
	Text      string        `json:"text"`
}

// Screenshot references one image part by id.
type Screenshot struct {
	ID      string        `json:"id"`
	Offset  time.Duration `json:"offset"`
	Caption string        `json:"caption,omitempty"`
}

// Labels are the renderer's own headings.
type Labels struct {
	Brief       string `json:"brief,omitempty"`
	Notes       string `json:"notes,omitempty"`
	Citations   string `json:"citations,omitempty"`
	Screenshots string `json:"screenshots,omitempty"`
	Screenshot  string `json:"screenshot,omitempty"`
}

func (l Labels) withDefaults() Labels {
	pick := func(value, fallback string) string {
		if value != "" {
			return value
		}
		return fallback
	}
	return Labels{
		Brief:       pick(l.Brief, "Executive brief"),
		Notes:       pick(l.Notes, "My notes"),
		Citations:   pick(l.Citations, "Transcript citations"),
		Screenshots: pick(l.Screenshots, "Screenshots"),
		Screenshot:  pick(l.Screenshot, "Screenshot"),
	}
}

// Fingerprint identifies the content of a Document. Two documents with the
// same fingerprint render identically; a target compares it with the
// fingerprint of its last Receipt to skip an unchanged re-push. Part bytes
// are not hashed: a screenshot id names immutable content.
func (d Document) Fingerprint() string {
	encoded, err := json.Marshal(d)
	if err != nil {
		// A Document holds only strings, numbers and times; Marshal cannot
		// fail on it. An empty fingerprint never matches a stored one.
		return ""
	}
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Part is one binary attachment of a Document.
type Part struct {
	ID          string
	ContentType string
	Data        []byte
}

// Parts gives a target the bytes behind a Screenshot id. Load answers an
// error wrapping fs.ErrNotExist (or any error) for a part that is gone; the
// target then skips it and reports it in Receipt.Skipped.
type Parts interface {
	Load(ctx context.Context, id string) (Part, error)
}

// PartsMap is an in-memory Parts.
type PartsMap map[string]Part

// Load implements Parts.
func (m PartsMap) Load(_ context.Context, id string) (Part, error) {
	part, ok := m[id]
	if !ok {
		return Part{}, errPartMissing
	}
	return part, nil
}

var errPartMissing = errors.New("export: part not found")

// Destination is where a target writes: a section, a database, a folder.
// ParentID is its container (a notebook) where the target needs one to
// create a sibling. Container names the space both ids live in where a target
// serves several (a OneNote site); empty is the target's default space.
type Destination struct {
	ID        string `json:"id"`
	ParentID  string `json:"parentId,omitempty"`
	Name      string `json:"name,omitempty"`
	Container string `json:"container,omitempty"`
}

// Options steer one push.
type Options struct {
	Destination Destination
	// Parts holds the screenshot bytes; nil exports text only.
	Parts Parts
	// Previous is the receipt of the last successful push of this document
	// to this target, or zero. Its fingerprint and external id make a
	// re-push idempotent.
	Previous Receipt
	// AllowRollover lets a target create a sibling destination when the
	// configured one is full, instead of answering ErrDestinationFull.
	AllowRollover bool
}

// Receipt records what a push wrote.
type Receipt struct {
	TargetID   string `json:"targetId"`
	ExternalID string `json:"externalId"`
	URL        string `json:"url,omitempty"`
	// Fingerprint is the Document fingerprint the destination now holds.
	// It is empty when a push failed half-way: the external object exists
	// but does not hold the whole document.
	Fingerprint string `json:"fingerprint,omitempty"`
	// Destination is where the push landed; it differs from the requested
	// one after a rollover.
	Destination Destination `json:"destination,omitzero"`
	// Skipped lists screenshot ids that were not written (missing parts or
	// a destination limit).
	Skipped []string `json:"skipped,omitempty"`
	// Unchanged is true when the push sent nothing because the destination
	// already held this fingerprint.
	Unchanged bool      `json:"unchanged,omitempty"`
	PushedAt  time.Time `json:"pushedAt,omitzero"`
}

// Target is one export destination.
type Target interface {
	// ID is the stable identifier the registry and the store use.
	ID() string
	// Push writes the document. When opts.Previous carries the current
	// fingerprint, Push returns it with Unchanged set and sends nothing.
	Push(ctx context.Context, doc Document, opts Options) (Receipt, error)
}
