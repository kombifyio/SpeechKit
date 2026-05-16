# SpeechKit Docs

Start with the module you are working on, then jump into the detailed docs.

## Modules

- [../README.md](../README.md) — short module overview.
- [speechkit-framework-api.md](./speechkit-framework-api.md) — local-first Go backend API, mode contracts, provider catalog, and local control API.
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
- [https://speechkit.cc/llms.txt](https://speechkit.cc/llms.txt) — public Markdown entrypoint for agents and crawler-friendly install guidance.

## Operations

- [server/DEPLOY.md](./server/DEPLOY.md) — SpeechKit Server Docker Compose and generic OCI deployments.

## Release And Trust

- [code-signing-policy.md](./code-signing-policy.md) — Windows artifact trust policy.
- [product-readiness-v0.34.1.md](./product-readiness-v0.34.1.md) — v0.34.1 desktop, agent-gate, storage/settings, and OSS-readiness release checklist.
