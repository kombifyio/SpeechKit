// Package config defines SpeechKit's TOML configuration schema and
// the load/merge/validate helpers around it. The Config struct
// composes per-mode sub-structs (dictation, assist, voiceagent,
// wakeword) plus the Server-Target ServerConfig.
//
// Audit 2026-05-24 maintainability sweep.
package config
