// Package audio is the platform-neutral audio I/O kernel. Pure-Go
// pieces (sample-rate conversion, ring buffers, WAV framing) live in
// this package directly; OS-specific capture/playback backends
// (malgo/WASAPI on Windows, oto/v3 on Linux, etc.) live in build-tag
// gated files alongside.
//
// Audit 2026-05-24 maintainability sweep.
package audio
