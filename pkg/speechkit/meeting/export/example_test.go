package export_test

import (
	"context"
	"fmt"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/meeting/export"
)

// stdoutTarget is a host's own target: it prints the Markdown rendering.
type stdoutTarget struct{}

func (stdoutTarget) ID() string { return "stdout" }

func (stdoutTarget) Push(_ context.Context, doc export.Document, opts export.Options) (export.Receipt, error) {
	fingerprint := doc.Fingerprint()
	if opts.Previous.Fingerprint == fingerprint {
		return export.Receipt{TargetID: "stdout", ExternalID: opts.Previous.ExternalID, Fingerprint: fingerprint, Unchanged: true}, nil
	}
	fmt.Print(doc.Outline().Markdown())
	return export.Receipt{TargetID: "stdout", ExternalID: "1", Fingerprint: fingerprint}, nil
}

func Example() {
	registry := export.NewRegistry()
	if err := registry.Register(stdoutTarget{}); err != nil {
		panic(err)
	}
	doc := export.Document{
		Title:     "Weekly sync",
		StartedAt: time.Date(2026, 10, 12, 9, 30, 0, 0, time.UTC),
		Locale:    "en",
		Sections: []export.Section{{
			Title: "Decisions",
			Items: []export.Item{{Text: "Release on Friday", CitationIDs: []int64{7}}},
		}},
		Notes:     []export.Note{{Text: "check the installer", Offset: 90 * time.Second, Stamped: true}},
		Citations: []export.Citation{{SegmentID: 7, Offset: 3 * time.Minute, Speaker: "Ana", Text: "Friday works."}},
	}
	receipt, _ := registry.Push(context.Background(), "stdout", doc, export.Options{})
	again, _ := registry.Push(context.Background(), "stdout", doc, export.Options{Previous: receipt})
	fmt.Println("second push unchanged:", again.Unchanged)
	// Output:
	// # Weekly sync
	//
	// _2026-10-12 09:30 · en_
	//
	// ## Decisions
	//
	// - Release on Friday [1]
	//
	// ## My notes
	//
	// - [01:30] check the installer
	//
	// ## Transcript citations
	//
	// 1. 03:00 Ana: Friday works.
	// second push unchanged: true
}
