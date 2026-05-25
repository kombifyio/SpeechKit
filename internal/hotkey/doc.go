// Package hotkey is the Windows adapter for the global hotkey bus.
// Dictation, Assist, and Voice Agent each register a binding here;
// the package translates RegisterHotKey/UnregisterHotKey Win32 calls
// into channel events the desktop runtime consumes.
//
// Audit 2026-05-24 maintainability sweep.
package hotkey
