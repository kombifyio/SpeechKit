# SpeechKit Docs

Start with the module you are working on, then jump into the detailed docs.

## Modules

- [../README.md](../README.md) — short module overview.
- [speechkit-framework-api.md](./speechkit-framework-api.md) — local-first Go backend API, mode contracts, provider catalog, and local control API.
- [architecture/sdk-surface-boundary.md](./architecture/sdk-surface-boundary.md) — public `pkg/speechkit/` boundary for v0.40.1 embedders.
- [words-and-replacements-standard.md](./words-and-replacements-standard.md) — active first-class customization axis: Words for recognition, Replacements for deterministic transformations, and Native Templates as curated pack sources.
- [capabilities/voice-capability-matrix.json](./capabilities/voice-capability-matrix.json) — source of truth for mode/capability/provider support and future Notion sync.
- [voice-companion.md](./voice-companion.md) — hands-free capability model, Assist companion architecture, skills, latency budgets, and embed targets.
- [wakeword.md](./wakeword.md) — wake activation setup, model policy, training data, and SDK promotion notes.
- [server/README.md](./server/README.md) — SpeechKit Server runtime, modes, auth, and deployment setup.
- [mcp/README.md](./mcp/README.md) — SpeechKit MCP server for coding agents, docs mode, management mode, and validation tools.
- [../CHANGELOG.md](../CHANGELOG.md) — release history.

## Architecture

- [architecture/voice-agent-target.md](./architecture/voice-agent-target.md) — Voice Agent (Mode 3) target architecture: confirmed decisions, 6-phase roadmap, critical files across SpeechKit + 11Seconds + kombify-AI, residual risks. The single source of truth for Mode 3 direction.

## Setup

- [LOCAL_TESTING.md](./LOCAL_TESTING.md) — canonical local `mise` preflight
  contract for setup, build, test, release preview, and deploy readiness.
- [setup/LIVE_TEST_VOICE_AGENT.md](./setup/LIVE_TEST_VOICE_AGENT.md) — End-to-end local test of the Voice Agent server + Wails device-target integration.

## Contracts

- [api/openapi.v1.yaml](./api/openapi.v1.yaml) — Local Windows Client control-plane OpenAPI.
- [server/openapi.v1.yaml](./server/openapi.v1.yaml) — SpeechKit Server HTTP and WebSocket OpenAPI.
- [capabilities/voice-capability-matrix.json](./capabilities/voice-capability-matrix.json) — provider capability matrix with auth methods, status, evidence URLs, and required env-var names.
- [https://speechkit.cc/llms.txt](https://speechkit.cc/llms.txt) — public Markdown entrypoint for agents and crawler-friendly install guidance.

## Operations

- [server/DEPLOY.md](./server/DEPLOY.md) — SpeechKit Server Docker Compose and generic OCI deployments.

## Release And Trust

- [code-signing-policy.md](./code-signing-policy.md) — Windows artifact trust policy.
- [release-matrix.md](./release-matrix.md) — release-surface matrix, OSS rollups, and validation expectations.
- [release-notes/v0.40.0.md](./release-notes/v0.40.0.md) — Runtime Modularity release notes.
- [release-notes/v0.40.1.md](./release-notes/v0.40.1.md) — SDK Surface Modularity release notes.
- [product-readiness-v0.34.1.md](./product-readiness-v0.34.1.md) — v0.34.1 desktop, agent-gate, storage/settings, and OSS-readiness release checklist.

## Compliance And Enterprise

- [compliance/ENTERPRISE-DEPLOYMENT.md](./compliance/ENTERPRISE-DEPLOYMENT.md) — single-page deployment reference for Customer IT and Auditor (egress hosts, config switches, audit-log layout, install paths, Profile A vs B).
- [compliance/audit-event-catalog.md](./compliance/audit-event-catalog.md) — audit-log schema v1 + per-event resource fields.
- [compliance/providers/README.md](./compliance/providers/README.md) — DSGVO TOM data sheets per provider.
- [superpowers/specs/2026-05-18-enterprise-hardening-design.md](./superpowers/specs/2026-05-18-enterprise-hardening-design.md) — Enterprise Hardening design spec (4 phases).
- [superpowers/plans/2026-05-18-enterprise-hardening-phase-0.md](./superpowers/plans/2026-05-18-enterprise-hardening-phase-0.md) — Phase 0 implementation plan.
