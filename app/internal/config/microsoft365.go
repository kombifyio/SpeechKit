package config

import (
	"strings"
	"time"
)

// Microsoft365GrantVersion is the version of the permission to copy meeting
// content out of Microsoft 365 onto this device. Version 1 covered Teams
// transcripts; version 2 adds Microsoft 365 Copilot meeting recaps. A grant
// keeps covering what its version covered when it was given.
const Microsoft365GrantVersion = 2

const (
	microsoft365TranscriptGrantMinVersion = 1
	microsoft365RecapGrantMinVersion      = 2
)

// Microsoft365Config connects meetings to Microsoft 365.
type Microsoft365Config struct {
	// ImportTeamsTranscripts brings the transcript Microsoft Teams produced
	// for a scheduled Teams meeting onto this device after the meeting, and
	// writes the meeting up from it instead of the local capture.
	ImportTeamsTranscripts bool `toml:"import_teams_transcripts"`
	// ImportCopilotRecaps brings the meeting notes and action items Microsoft
	// 365 Copilot wrote for a Teams meeting onto this device, next to
	// SpeechKit's own review. Every user needs a Microsoft 365 Copilot
	// licence.
	ImportCopilotRecaps bool `toml:"import_copilot_recaps"`
	// TenantID pins the Microsoft sign-in authority. Empty signs in to the
	// account's home tenant, which is where its Teams meetings live.
	TenantID string `toml:"tenant_id,omitempty"`
	// TranscriptGrant* record the user's permission to copy meeting content
	// out of Microsoft 365 onto this device, where Microsoft 365 retention,
	// eDiscovery and sensitivity labels no longer cover it. The version says
	// what the permission covered when it was given.
	TranscriptGrantVersion   int    `toml:"transcript_grant_version,omitempty"`
	TranscriptGrantGrantedAt string `toml:"transcript_grant_granted_at,omitempty"`
}

func (c Microsoft365Config) granted(minVersion int) bool {
	return c.TranscriptGrantVersion >= minVersion && strings.TrimSpace(c.TranscriptGrantGrantedAt) != ""
}

// HasTranscriptGrant reports whether the user allowed copying Teams
// transcripts onto this device.
func (c Microsoft365Config) HasTranscriptGrant() bool {
	return c.granted(microsoft365TranscriptGrantMinVersion)
}

// HasRecapGrant reports whether the user allowed copying Microsoft 365
// Copilot meeting recaps onto this device.
func (c Microsoft365Config) HasRecapGrant() bool {
	return c.granted(microsoft365RecapGrantMinVersion)
}

// TranscriptImportActive reports whether Teams transcripts are imported: the
// user turned the import on and gave the permission.
func (c Microsoft365Config) TranscriptImportActive() bool {
	return c.ImportTeamsTranscripts && c.HasTranscriptGrant()
}

// RecapImportActive reports whether Copilot meeting recaps are imported.
func (c Microsoft365Config) RecapImportActive() bool {
	return c.ImportCopilotRecaps && c.HasRecapGrant()
}

// GrantTranscripts records the permission in its current version, which
// covers transcripts and Copilot recaps.
func (c *Microsoft365Config) GrantTranscripts(now time.Time) {
	c.TranscriptGrantVersion = Microsoft365GrantVersion
	c.TranscriptGrantGrantedAt = now.UTC().Format(time.RFC3339)
}

// RevokeTranscripts withdraws the permission and turns both imports off.
func (c *Microsoft365Config) RevokeTranscripts() {
	c.ImportTeamsTranscripts = false
	c.ImportCopilotRecaps = false
	c.TranscriptGrantVersion = 0
	c.TranscriptGrantGrantedAt = ""
}
