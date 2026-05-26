// Package hotkey is the Windows adapter for the global hotkey bus.
// Dictation, Assist, and Voice Agent each register a binding here;
// the package translates RegisterHotKey/UnregisterHotKey Win32 calls
// into channel events the desktop runtime consumes.
//
// Combo syntax (ParseCombo): `+`-separated tokens, e.g. `ctrl+shift`,
// `win+alt`, `ctrl+alt+f12`, `ctrl+space`. Mouse side-buttons X1/X2
// are recognised as `mouse-x1`/`mouse-x2` (aliases `mb4`/`mb5`) and
// picked up by the polling loop via GetAsyncKeyState — no separate
// WH_MOUSE_LL hook is installed. Left, right, and middle mouse
// buttons are intentionally NOT mappable to keep everyday clicks
// usable.
//
// Audit 2026-05-24 maintainability sweep.
package hotkey
