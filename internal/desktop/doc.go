// Package desktop hosts the Device-Target runtime helpers (mode
// dispatcher, control-plane, update checker, settings persistence)
// that the Windows Wails reference UI under cmd/speechkit/ wires
// together. Pure Go; Windows-specific surface lives in sibling
// packages (hotkey, tray, winapi, output).
//
// Audit 2026-05-24 maintainability sweep.
package desktop
