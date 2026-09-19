package config

import (
	"testing"
	"time"
)

// A permission keeps covering what it covered when it was given: an early
// grant for transcripts does not silently extend to Copilot recaps.
func TestMicrosoft365GrantCoversWhatItsVersionCovered(t *testing.T) {
	early := Microsoft365Config{ImportTeamsTranscripts: true, ImportCopilotRecaps: true, TranscriptGrantVersion: 1, TranscriptGrantGrantedAt: "2026-09-17T08:00:00Z"}
	if !early.TranscriptImportActive() || early.RecapImportActive() {
		t.Fatalf("version 1: transcripts %v, recaps %v; want transcripts only", early.TranscriptImportActive(), early.RecapImportActive())
	}

	early.GrantTranscripts(time.Now())
	if !early.TranscriptImportActive() || !early.RecapImportActive() {
		t.Fatal("the current grant covers transcripts and recaps")
	}

	early.RevokeTranscripts()
	if early.ImportTeamsTranscripts || early.ImportCopilotRecaps || early.HasTranscriptGrant() || early.HasRecapGrant() {
		t.Fatalf("revoking must turn both imports off: %+v", early)
	}
}
