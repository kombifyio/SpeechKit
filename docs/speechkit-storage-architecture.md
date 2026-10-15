# SpeechKit Storage Architecture

**Status:** Storage 3.0 local-first OSS contract, v0.34.1 readiness baseline, May 15, 2026

## Current Contract

SpeechKit ships with a local-first storage layer that is usable without accounts, tenants, or cloud infrastructure:

- `sqlite` is the default production-ready local backend.
- `postgres` is supported when a DSN is configured.
- Host applications can register additional backends without changing the OSS core.
- A hosted backend is always an external, registered backend, never a dependency of the default framework.

The public framework contract lives in `pkg/speechkit/storage`. It defines `Scope`, context helpers, scope policies, backend capabilities, and backend registration primitives. The internal app store keeps its existing facade, but all user-owned records resolve a scope from `context.Context`.

For `v0.34.1`, this is the product boundary: SQLite, scope resolution, scoped
history/settings-adjacent records, audio metadata links, and per-scope stats are
release surface. Any additional backend is registered by the host and must not
be required by local usage.

## Scope Model

Storage 3.0 standardizes multi-user and multi-tenant support without forcing a SaaS product model into local apps.

- No scope in context maps to the local default scope: `install:local`.
- Host apps can provide `InstallID`, `DeviceID`, `UserID`, `TenantID`, and labels.
- SQLite and Postgres persist scopes in `storage_scopes`.
- User data tables store `scope_id` and filter all list, detail, stats, and dictionary queries by that scope.
- Backends can require a user or tenant with `ScopePolicy`, while the OSS desktop default stays no-config.

Persona, role, and sequence catalog tables remain global/admin-authored in this step. Persona multi-tenancy needs a separate product model and is intentionally not mixed into Storage 3.0.

## Data Model

Scoped tables:

- `transcriptions`
- `quick_notes`
- `user_dictionary_entries`
- `voice_agent_sessions`
- `audio_assets`
- `store_stats`

Words And Replacements replaced the narrow dictionary data model in v0.44.
`user_dictionary_entries` remains only as an old-data migration table
where needed. The migration projects each dictionary row into a `Word` plus a
`Replacement{substitution}` while preserving scoped data ownership. See
[words-and-replacements-standard.md](words-and-replacements-standard.md).

Audio is represented through `audio_assets` plus owner link tables:

- `transcription_audio_assets`
- `quick_note_audio_assets`

`Transcription.AudioPath` and `QuickNote.AudioPath` remain Go compatibility fields, but new writes derive them from the linked `audio_assets` row instead of persisting path data in the owner table.

Voice Agent sessions use light list rows and detail rows:

- `/api/v1/voice-sessions` returns session metadata and scalar summary fields.
- `/api/v1/voice-sessions/{id}` returns transcript, turns, raw summary, and summary items.

## Performance Notes

Storage 3.0 removes the hot-path OR/function language filters by persisting `language_base` on history rows. Scoped list indexes cover the default history and quick-note reads.

Stats are maintained per scope in `store_stats`; `Stats()` reads that aggregate instead of repeatedly scanning all history rows. Startup migrations can recalculate the table from the base records.

## Backend Guidance

SQLite remains the simplest OSS path and should be the documented default. Postgres is the BYO database path for hosts that need shared infrastructure or stronger operational controls.

Additional cloud backends should:

- honor the public `storage.Scope` contract
- declare scope requirements through `ScopePolicy`
- keep tenant/user enforcement in the backend layer
- avoid adding private cloud assumptions to desktop-local code paths

## Export And Import

Exports should include scope metadata alongside scoped records. Imports can either preserve the source scope, map records to the local default scope, or be rejected by the host backend policy when required scope data is missing.

## Data location (desktop)

The reference desktop client keeps user content (the SQLite database with its
WAL and SHM files, `audio/`, `snapshots/`, `wakeword-activations/`) under one
data root and everything else (configuration, install state, secrets) in the
roaming profile directory.

| Install | Data root |
|---|---|
| Windows installed | `%LOCALAPPDATA%\SpeechKit` (earlier releases: `%APPDATA%\SpeechKit`) |
| Windows portable | `<exe>\data` |
| macOS | `~/Library/Application Support/SpeechKit` |
| `[store] data_root` set | that folder; a network path (UNC or mapped drive) is refused and the default is used |

Roaming profiles, FSLogix and UE-V copy Roaming AppData to file servers, which
is why content moved out of it. `sqlite_path`, when set, still names the
database file and wins for it; audio and snapshots stay beside the database.

Migration (`app/internal/datalocation`) runs at start on Windows installs. When
the old roaming root holds content, the database set is copied through a
staging directory and committed by renaming the database file last, the
directories are renamed (same volume) or copied through a `.part` sibling, and
the old copies are removed only after everything is in place. It never runs
twice at once (lock file in the new root), skips a database another process
holds open (the old root stays in use, the reason is logged as
`desktop.startup.data_root`), rolls back on any failure, and leaves
`data-moved.txt` in the old root. Stored absolute paths under the old root
(`sqlite_path`, `[feedback] db_path`) are rewritten in `config.toml`.

At start the client reports whether the volume under the data root is
BitLocker protected as `bitlocker` (`protected`, `unprotected`, `unknown`) in
`GET /features` and in the `desktop.startup.bitlocker` log line. It is
read through the `System.Volume.BitLockerProtection` shell property, which a
standard user may read; nothing is enforced yet. macOS reports `unknown`.
