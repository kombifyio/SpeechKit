// Package output is the Windows text-output adapter. Transcribed
// text + assist results are paste-injected via clipboard + SendInput;
// the package also exposes a fallback keystroke-typing path for
// targets that refuse paste. Pure Win32 / winapi consumer.
//
// Audit 2026-05-24 maintainability sweep.
package output
