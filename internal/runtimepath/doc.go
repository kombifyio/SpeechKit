// Package runtimepath resolves exe-relative paths so SpeechKit's
// portable-mode bundle finds its bundled assets, models, and
// per-user data dirs without depending on the OS-level installer
// having registered a fixed location.
//
// Audit 2026-05-24 maintainability sweep.
package runtimepath
