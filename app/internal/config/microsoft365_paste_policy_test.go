package config

import (
	"slices"
	"testing"
)

// A machine pin that turns the transcript paste off wins over config.toml
// and locks the switch; a per-user value cannot turn it back on.
func TestTranscriptPastePinnedOffCannotBeTurnedOnByTheUser(t *testing.T) {
	off, on := false, true
	cfg := defaults()
	cfg.Microsoft365.AllowTranscriptPaste = true

	AttachPolicy(cfg, mergePolicyHives(PolicyValues{Microsoft365AllowTranscriptPaste: &off}, PolicyValues{}, PolicyValues{Microsoft365AllowTranscriptPaste: &on}))

	if cfg.Microsoft365.AllowTranscriptPaste || !slices.Contains(cfg.PolicyLockedFields(), "microsoft365.allow_transcript_paste") {
		t.Fatalf("paste = %v, locked %v; want it off and locked", cfg.Microsoft365.AllowTranscriptPaste, cfg.PolicyLockedFields())
	}
}
