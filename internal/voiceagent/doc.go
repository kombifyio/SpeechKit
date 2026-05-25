// Package voiceagent is the Voice Agent kernel — realtime
// audio-to-audio session manager backed by Gemini Live, with
// Persona/Role/Sequence resolution from internal/voicebehavior. The
// local desktop provider (cmd/speechkit) and the server adapter
// (internal/server/voiceagent) both depend on this package; the
// kernel itself stays free of OS or HTTP plumbing.
//
// Audit 2026-05-24 maintainability sweep.
package voiceagent
