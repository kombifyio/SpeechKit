# SpeechKit Storage Architecture

**Status:** current OSS-oriented architecture, March 27, 2026

## Current Reality

SpeechKit currently ships with one production-ready storage backend in the repo:

- `sqlite` as the default backend
- local metadata in `%APPDATA%/SpeechKit/feedback.db`
- optional raw WAV storage in `%APPDATA%/SpeechKit/audio/`

This is the only storage path that should be described as shipped and supported in the first OSS release.

## Why This Matters

The repo is being prepared for a framework-style public release. That requires the docs to distinguish clearly between:

- what is implemented now
- what is a planned extension point
- what belongs in private downstream integrations instead of the OSS core

## Current Store Contract

The current store layer is designed to keep extension space open without forcing extra infrastructure into the default runtime:

- `sqlite` is the local, zero-config default
- semantic capabilities are optional
- provider and audio metadata remain part of the stored record model
- host apps can decide retention and raw-audio policy

## Future Work, Not Yet Shipped

The following are valid roadmap items, but they are not part of the first OSS release contract:

- generic PostgreSQL backend
- S3-compatible audio storage
- pgvector or other vector-search backends
- private downstream plugin backends

If these backends are added later, they should extend the existing contracts without breaking the SQLite-first default path.

## OSS Boundary

For the public framework release:

- the default experience must work with local storage only
- no private cloud integration may be required
- no proprietary kombify backend code may be referenced as part of the shipped OSS implementation
- release docs must never claim a backend is available when the code is still a stub

## Release Guidance

When updating storage docs:

- describe `sqlite` as current
- describe `postgres` and richer storage backends as planned or experimental until they are fully implemented and tested
- keep host-managed secrets and provider credentials out of the store contract unless there is a concrete, public implementation
