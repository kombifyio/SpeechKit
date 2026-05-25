// Package server is the umbrella for the Linux Server-Target HTTP +
// WebSocket adapter. Sub-packages own bootstrap (core), per-mode
// handlers (dictation, assist, voiceagent, personas, transcripts,
// vocabulary), the middleware chain (middleware), wake-word training
// upload (wakewordtraining), and onboarding asset embeds (onboarding).
// All of internal/server is //go:build linux.
//
// Audit 2026-05-24 maintainability sweep.
package server
