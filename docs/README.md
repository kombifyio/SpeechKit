# SpeechKit Docs

Start here when embedding SpeechKit as a Go framework, running the self-hosted
server, or wiring agent tooling against the public API.

## Framework

- [SDK in 10 minutes](./sdk/README.md) — `go get` to first transcript, the four contracts every host wires, where to go next.
- [Custom provider](./sdk/custom-provider.md) — add an STT/TTS/live backend: SPI, conformance suite, catalog profile, routing.
- [Framework API](./speechkit-framework-api.md) — local-first Go backend API, mode contracts, provider catalog, readiness, and local control API.
- [SDK surface boundary](./architecture/sdk-surface-boundary.md) — public `pkg/speechkit/...` package boundary for embedders.
- [Android SDK surface boundary](./architecture/android-sdk-surface-boundary.md) — which `android/` modules a host may depend on.
- [Android context actions](./client/android-context-actions.md) — screen capture, shared text/image/audio, preview and execution policy.
- [Voice Companion](./voice-companion.md) — Hands-Free target model, Assist companion architecture, and embed targets.
- [Wake word](./wakeword.md) — wake activation contracts, model policy, and SDK notes.
- [Words And Replacements](./words-and-replacements-standard.md) — customization primitives for recognition knowledge and deterministic transformations.
- [Storage architecture](./speechkit-storage-architecture.md) — public storage scope and backend contracts.

## Contracts

- [Local Windows Client OpenAPI](./api/openapi.v1.yaml)
- [SpeechKit Server OpenAPI](./server/openapi.v1.yaml)
- [SpeechKit Server AsyncAPI](./server/asyncapi.v1.yaml)
- [Voice capability matrix](./capabilities/voice-capability-matrix.json)
- [Provider option matrix](./capabilities/provider-option-matrix.json)

## Server And Agents

- [SpeechKit Server](./server/README.md) — self-host runtime, modes, auth, and deployment setup.
- [Server deployment](./server/DEPLOY.md) — Docker Compose and generic OCI deployment notes.
- [SpeechKit MCP](./mcp/README.md) — MCP server for coding agents, docs mode, management mode, and validation tools.
- [MCP distribution](./mcp/distribution.md)
- [Agent entrypoint](./agent/llms.txt)
- [Agent full context](./agent/llms-full.txt)
- [Agent snippets](./agent/llms-snippets.txt)

## Project

- [Hybrid features and evidence](./hackathon/hybrid-features.md)
- [Hybrid build and acceptance guide](./hackathon/build-hybrid-ai-2026.md)
- [Devpost project draft](./hackathon/devpost-submission.md)
- [Repository README](../README.md)
- [Examples](../examples/README.md)
- [Changelog](../CHANGELOG.md)
- [Contributing](../CONTRIBUTING.md)
