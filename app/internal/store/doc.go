// Package store is the durable backend for transcriptions, quick
// notes, voice-agent session summaries, persona catalog (M5b), and
// wake-word activation audio. SQLite is the default for the Wails
// desktop bundle; PostgreSQL is used by the Linux Server-Target
// behind the Render private network. Same Store interface; the
// backend is picked from StoreConfig.Backend.
//
// Audit 2026-05-24 maintainability sweep.
package store
