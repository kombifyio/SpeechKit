# Contributing to SpeechKit

SpeechKit is a beta Go framework and self-hostable speech backend for
Dictation, Assist, and Voice Agent workflows. The public repository focuses on
the reusable Go packages, Linux server, CLI, MCP server, examples, docs, and
release artifacts. The Windows desktop client is distributed through GitHub
Releases, not developed directly in this public source tree.

## Before You Start

- Read [README.md](./README.md) for product scope and module boundaries.
- Read [docs/README.md](./docs/README.md) for framework, server, MCP, and API docs.
- Keep imports on the public surface: `<module path>/pkg/speechkit/...` (see
  [Repository identities](#repository-identities) for which module path applies).
- Do not import `internal/*` from downstream applications.

## Repository Identities

One codebase carries three names. Knowing which is which avoids most
"go get cannot find" and "wrong remote" confusion:

| Identity | Value | Used for |
|---|---|---|
| Governed working source | the private working repository (the `kombify-SpeechKit` checkout this file is edited in) | Day-to-day development, CI, Beads planning, release automation. Not the `go get` target. |
| Public mirror | `kombifyio/SpeechKit` | `go get`, pkg.go.dev, GitHub Releases, `ghcr.io/kombifyio/speechkit-server`, JitPack for the Android AARs, public issues. Produced by an allowlist export (`scripts/public/export-public.*`). |
| Go module path (`go.mod`) | `github.com/kombifyio/SpeechKit` | Import path only, not a repository location. Whatever `go.mod` says in the tree you are reading is what you import; the public export rewrites the working-source path to `github.com/kombifyio/SpeechKit`, which is what a consumer sees. |

Rules that follow from this:

- Never change the module path in `go.mod`. It is an import path that every
  consumer, the public export rewrite and the API-diff gate depend on; renaming
  the repository does not rename the module.
- Write import statements with the module path of the tree you are in. The
  export rewrites Go imports, `go.mod`, docs and scripts to the public path in
  one pass, so a public consumer only ever sees `github.com/kombifyio/SpeechKit`.
- Link public-facing documentation to the mirror
  (`https://github.com/kombifyio/SpeechKit`), not to the working repository.
  Exported docs must stay usable without access to the working source.
- Android artifacts follow the same split: JitPack builds from the mirror and
  is the public channel; GitHub Packages on the working repository is the
  internal lane. See [android/README.md](./android/README.md).

## Development Setup

1. Install Go `1.26+`.
2. Install Node.js `24+`.
3. Install Docker if you want to run the self-host server locally.
4. Optional: install `gitleaks` for local secret scanning.
5. Optional but recommended: install [lefthook](https://lefthook.dev/installation/)
   and run `lefthook install` once. The pre-commit hook runs `gofmt -s -l` on
   staged Go files (plus golangci-lint and eslint); `mise run fmt:check` is the
   same gofmt sweep over every tracked Go file and finishes in about a second.

## Repository Layout

| Path | Purpose |
|---|---|
| `pkg/speechkit/` | Public Go framework packages for embedders |
| `cmd/speechkit-server/` | Linux self-host server entry point |
| `cmd/speechkit-cli/` | CLI diagnostics and scaffolding |
| `cmd/speechkit-mcp/` | MCP server for agent docs, validation, and management |
| `internal/` | Implementation packages used by the public binaries |
| `examples/` | Runnable framework and server integration examples |
| `docs/` | Public framework, server, API, and agent documentation |
| `deploy/` | Dockerfile, Compose example, and server config templates |
| `release/latest/windows/` | Metadata mirror for the latest Windows release asset |

## Public Verification

Run these before opening a pull request:

```bash
go test ./pkg/... ./cmd/speechkit-cli/... ./cmd/speechkit-mcp/... ./examples/...
GOOS=linux CGO_ENABLED=0 go test ./cmd/speechkit-server/...
GOOS=linux CGO_ENABLED=0 go build ./cmd/speechkit-server ./cmd/speechkit-mcp ./cmd/speechkit-cli
go vet ./pkg/... ./cmd/speechkit-cli/... ./cmd/speechkit-mcp/... ./examples/...
node scripts/release/check-doc-links.mjs
gitleaks detect --source . --redact
```

The public examples should run without provider credentials unless their README
or source comments explicitly state that a live provider key is required:

```bash
go run ./examples/provider-catalog
go run ./examples/embed-companion
go run ./examples/embed-tts
go run ./examples/embed-event-bus
```

## Contribution Rules

- Keep public API changes additive when possible. SpeechKit is pre-1.0, but
  breaking changes still need a clear changelog entry.
- Keep secrets in environment variables or generated local `.env` files. Never
  commit provider keys, bearer tokens, private hostnames, or personal paths.
- Keep docs links resolvable in a fresh clone of the public repo.
- Keep examples small and runnable from the repository root.
- Keep server/browser auth guidance conservative: public deployments must not
  use `auth_mode = "none"`.
- Do not add private upstream files such as local planning docs, generated
  desktop build outputs, or internal-only workflow helpers to the public surface.

## Changelog

User-facing changes belong in [CHANGELOG.md](./CHANGELOG.md). Write entries for
framework users and server operators, not maintainers. The release lint checks
the `[Unreleased]` section and rendered release notes:

```bash
node scripts/release/lint-changelog.mjs --unreleased
node scripts/release/lint-version-sync.mjs
```

## Pull Requests

Include:

- the framework/server surface you changed
- the commands you ran
- any provider credentials or live services intentionally skipped
- screenshots only when changing visible docs or generated output

Small, focused pull requests are easiest to review.
