// Package voiceagentprofile re-exports the voicebehavior Profile
// DTO with JSON tags suitable for HTTP envelope serialisation. The
// audit 2026-05-24 sprint-5 work plans to merge this package back
// into internal/voicebehavior once the JSON-tag shape is reconciled;
// today the re-export keeps the persona catalog free of HTTP-
// specific tags.
//
// Audit 2026-05-24 maintainability sweep.
package voiceagentprofile
