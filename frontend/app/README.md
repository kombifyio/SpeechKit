# SpeechKit Frontend

This directory contains the React/Vite source for the desktop UI surfaces embedded into the Wails host.

## What Lives Here

- dashboard and settings views
- overlay surfaces and quick capture UI
- provider and settings client code under `src/lib/`
- frontend tests and lint configuration

## Local Commands

```bash
npm ci
npm test
npm run lint
npm run build
```

## Asset Flow

- source lives in `frontend/app/src`
- production build output is written into `internal/frontendassets/dist`
- the Go desktop host serves those generated assets at runtime

Do not hand-edit files in `internal/frontendassets/dist`. Rebuild from this directory instead.

## Working Agreement

- keep UI changes covered by tests when behavior changes
- keep host-specific runtime logic in Go and transport/UI logic in the frontend
- avoid introducing provider-specific credential handling into the framework-facing UI contracts
