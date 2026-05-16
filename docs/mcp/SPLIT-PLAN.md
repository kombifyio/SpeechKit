# `cmd/speechkit-mcp` split plan

**Status:** Phase C execution started for v0.34.1 product readiness. PR-1
utility extraction has begun; the remaining handler/package moves stay
sequenced below.
**Audit basis:** `cmd/speechkit-mcp/main.go` is 700 LOC with 48 top-level
functions all in `package main`, mixing transport bootstrap, three modes
of tool handlers (docs / management / test), and OpenAPI-payload
validation helpers.

## Current map (single file)

| Lines (approx) | Concern | Functions |
|---|---|---|
| 1-68 | Bootstrap & transport | `main`, flag parsing, stdio/HTTP switch, MCP-token guard |
| 69-158 | Server construction, tool registration | `newServer` (in a sibling file), shared types (`queryInput`, `endpointInput`, …) |
| 159-220 | Docs / API tools | `docsSearch`, `apiEndpoint`, `apiOverview`, `getOpenAPISpec`, `getAsyncAPISpec`, `integrationExample`, `architectureOverview` |
| 220-260 | Scaffold tools | `scaffoldTemplates`, `scaffoldIntegration` |
| 261-455 | Management CRUD (mode `management`) | `installPlan`, `status`, `configGet`, `providerList`, `providerReadiness`, `personasList`, `personaGet/Create/Update/Delete`, `rolesList`, `roleGet/Create/Update/Delete`, `sequencesList`, `sequenceGet/Create/Update/Delete` |
| 457-505 | Read-side management | `transcriptsList`, `transcriptGet`, `voiceAgentSessionSummary`, `vocabularyGet`, `vocabularyReplace` |
| 507-530 | Provider exercise tools (mode `test`) | `transcribe`, `ttsSynthesize` |
| 532-597 | OpenAPI validation tools (mode `test`) | `validateConfig`, `validateRequest`, `validateResponse`, `checkCompatibility`, `selfCheckPlan`, `breakingChanges` |
| 598-700 | Helpers | `validateOpenAPIPayload`, `textResult`, `jsonResult`, `stringMapValue`, `shellQuote` |

## Target layout

```text
cmd/speechkit-mcp/
├── main.go                          (~80 LOC: flags, transport, exit)
├── internal/
│   ├── server/
│   │   ├── server.go                (newServer, tool registration table)
│   │   └── types.go                 (all *Input structs used by multiple tools)
│   ├── tools/
│   │   ├── docs/
│   │   │   ├── docs.go              (docsSearch, apiEndpoint, apiOverview, integrationExample, architectureOverview)
│   │   │   └── specs.go             (getOpenAPISpec, getAsyncAPISpec)
│   │   ├── scaffold/
│   │   │   └── scaffold.go          (scaffoldTemplates, scaffoldIntegration)
│   │   ├── management/
│   │   │   ├── status.go            (installPlan, status, configGet)
│   │   │   ├── providers.go         (providerList, providerReadiness)
│   │   │   ├── personas.go          (full persona CRUD)
│   │   │   ├── roles.go             (full role CRUD)
│   │   │   ├── sequences.go         (full sequence CRUD)
│   │   │   ├── transcripts.go       (transcriptsList, transcriptGet, voiceAgentSessionSummary)
│   │   │   └── vocabulary.go        (vocabularyGet, vocabularyReplace)
│   │   └── test/
│   │       ├── exercise.go          (transcribe, ttsSynthesize)
│   │       ├── validation.go        (validateConfig, validateRequest, validateResponse, checkCompatibility)
│   │       └── compat.go            (selfCheckPlan, breakingChanges, validateOpenAPIPayload)
│   └── util/
│       └── result.go                (textResult, jsonResult, stringMapValue, shellQuote)
```

Target sizes: every file ≤ 300 LOC, `main.go` ≤ 100 LOC.

## Sequencing

Land in five PRs, each independently reviewable:

1. **PR-1** — extract `internal/util` (pure helpers, no API behavior). Started
   in v0.34.1 with MCP result formatting and shell quoting.
2. **PR-2** — extract `internal/server` (server bootstrap, tool registration, shared input types). Verify stdio + HTTP transports still work end-to-end.
3. **PR-3** — extract `internal/tools/docs` and `internal/tools/scaffold` (the lowest-risk read-only tools).
4. **PR-4** — extract `internal/tools/management` (the bulk of the surface; touches provider/persona/role/sequence/transcript/vocabulary CRUD).
5. **PR-5** — extract `internal/tools/test` (validation + provider exercise tools).

Each PR keeps the wire surface identical — only file/package locations move.

## Verification

After every PR:

```powershell
go build ./cmd/speechkit-mcp/...
go test ./cmd/speechkit-mcp/...
# Smoke (requires a running SpeechKit server on localhost:8080):
go run ./cmd/speechkit-mcp --mode=docs --transport=http --addr=127.0.0.1:18090
# In another shell, run the public-website MCP smoke against the new binary.
```

The full Schemathesis fuzz from `server-spec-contract.yml` must remain
green after PR-5.

## What does NOT move

- The MCP tool name strings (e.g. `speechkit_docs_search`, `speechkit_persona_create`) must stay byte-identical. They are the public protocol surface and any rename breaks all configured agents.
- The flag names (`--mode`, `--server`, `--token`, `--transport`, `--addr`, `--mcp-token`) and the env-var fallbacks. These are documented in [docs/mcp/README.md](README.md).

## Rollback

Each PR is purely a code-motion change — revertible without data
migration. If a regression is found, revert the offending PR and re-land
after fixing.

## Related

- Phase C item C3 in the post-audit improvement plan ([docs/audits/2026-05-13/improvement-plan.md](../audits/2026-05-13/improvement-plan.md)).
- The Wails-client decomposition (C2) follows the same per-PR-per-subpackage pattern.
