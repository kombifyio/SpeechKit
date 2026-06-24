# SpeechKit

[![Go Reference](https://pkg.go.dev/badge/github.com/kombifyio/SpeechKit.svg)](https://pkg.go.dev/github.com/kombifyio/SpeechKit)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.26%2B-00ADD8.svg)](go.mod)

> **🚧 Beta.** SpeechKit is in active beta. Public APIs, config keys, and
> defaults can still change between minor releases. Use it in production
> only with version pins. Pre-1.0 releases use the
> [`v0.MAJOR.MINOR`](CHANGELOG.md) scheme; breaking changes are called
> out in each release entry.

SpeechKit is a Windows-first voice framework for products that need dictation,
voice commands, and realtime voice dialogue without coupling every use case to
one desktop app or one hosted API.

The framework currently has four modules:

| Module | What it is | Use it when |
| --- | --- | --- |
| Local-first Go backend | Embeddable Go runtime in `pkg/speechkit` with mode contracts, provider profiles, routing policy, readiness metadata, and reusable Dictation, Assist, and Voice Agent services. | You want to integrate SpeechKit into your own Go product, internal tool, prototype, or automation host. |
| Self-host Server | Linux server runtime in `cmd/speechkit-server` that wraps the same backend behind HTTP and WebSocket APIs. | You need a durable process for your own clients, teams, product backends, browsers, or centrally managed model/provider configuration. |
| Agent tools | `speechkit-mcp` and `speechkitctl` expose docs, validation, scaffolding, diagnostics, and authenticated server management to coding agents and operators. | You want an agent to inspect the framework, generate starters, validate payloads, or operate a self-hosted server. |
| Windows Client | Public installer and portable release assets for local use, provider testing, and server-connected workflows. | You want to use SpeechKit on a Windows machine, validate providers and models, or connect a workstation to a SpeechKit Server. |

Desktop device support is Windows 10/11 x64 only today. The Windows Client uses
WASAPI for local capture/playback. Linux is supported as a server runtime, not
as a desktop capture client; macOS and Linux desktop packages are not currently
supported.

The runtime modules share the same three strict modes:

| Mode | Purpose | Boundary |
| --- | --- | --- |
| Dictation | Turn speech into text. | STT only. No LLM rewriting, no utilities, no codewords. |
| Assist | Turn speech or text into one useful result. | Codeword, utility, or LLM output with optional TTS and explicit UI surface metadata. |
| Voice Agent | Run realtime audio-to-audio dialogue. | Live conversation for brainstorming, support, and fast follow-ups. |

Hands-Free is not a fourth mode. It is an activation and voice-output layer for
the three modes: wake activation, microphone capture, auto-end policy, and
optional speaker output. Assist uses it for Siri/Alexa-style Voice Companion
requests, Voice Agent uses it for continuous dialogue, and Dictation uses it
only as UI-assisted activation with a visible text target or commit surface.

Speaker diarization, speaker identification, and speaker attribution are also
add-on capabilities over the three modes. Provider support and auth status are
tracked in the [voice capability matrix](./docs/capabilities/voice-capability-matrix.json).

Words and Replacements are the first-class customization axis over the same
three modes. Words teach SpeechKit terms to recognize; Replacements define
deterministic text, command, snippet, synonym, and template transformations.
Native Templates are versioned curated packs of the same Words/Replacements
data, for example the default punctuation template and the opt-in developer
template. They replace the narrow dictionary concept without creating another
mode. See the
[Words And Replacements standard](./docs/words-and-replacements-standard.md).

## Why SpeechKit

### Local-first Go backend

Use the backend when you want voice features inside another application without
adopting the Windows client. The public `pkg/speechkit` boundary gives host
apps stable mode contracts, service interfaces, provider catalogs, and
readiness data they can turn into their own setup UI.

Key advantages:

- One framework kernel for Dictation, Assist, and Voice Agent instead of three
  unrelated voice pipelines.
- Local-first provider support with room for managed local runtimes,
  user-managed local services, cloud providers, and direct vendor APIs.
- Host policy controls for enabled modes, fixed profiles, fallbacks, and clean
  vs intelligence behavior.
- Machine-readable readiness checks for credentials, local runtimes, model
  artifacts, and mode capability.

Start with [Framework API](./docs/speechkit-framework-api.md),
[Voice Companion](./docs/voice-companion.md), or the examples in
[examples/](./examples/README.md).

### Self-host Server

Use the server when SpeechKit should run as a long-lived Linux service you
operate. It
adapts the same framework kernel to a containerized API surface so other
clients can call Dictation, Assist, and Voice Agent without embedding Go code.

Key advantages:

- One server image, one URL, and one deployment contract for all three modes.
- HTTP endpoints for Dictation and Assist plus WebSocket sessions for realtime
  Voice Agent.
- Built-in health/readiness routes, bearer or edge-auth modes, CORS/origin
  controls, and OpenAPI contracts.
- Centralized provider, model, and secret configuration for teams or hosted
  deployments.

Start with [docs/server/README.md](./docs/server/README.md) and the server
OpenAPI file at [docs/server/openapi.v1.yaml](./docs/server/openapi.v1.yaml).

### Agent-native integration

Use the MCP server and CLI when a coding agent should work with SpeechKit
directly. Docs and test modes work without a running server. Management mode
wraps a self-hosted SpeechKit Server and uses the same bearer or edge-auth
rules as the HTTP API.

Start with [docs/mcp/README.md](./docs/mcp/README.md),
[docs/agent/mcp/speechkit-mcp.md](./docs/agent/mcp/speechkit-mcp.md), and the
agent entrypoint at [docs/agent/llms.txt](./docs/agent/llms.txt).

### Windows Client

Use the Windows client when you want a ready-to-run desktop experience or a
reference host for testing providers, models, and server connections. The app
can run local-first on the machine or delegate selected work to a SpeechKit
Server.

Key advantages:

- Global hotkeys for Dictation, Assist, and Voice Agent.
- Hands-Free settings for wake activation, target mode, auto-end behavior, and
  voice output.
- Local audio capture, VAD, overlays, settings, provider setup, and optional
  audio playback in one Wails app.
- Provider/model test bench for local, cloud, and direct integrations.
- Server connection support with configurable bearer-token environment variable,
  request timeout, and local fallback behavior.

Download public builds from
[GitHub Releases](https://github.com/kombifyio/SpeechKit/releases). A fresh
repository clone also carries the current installer metadata in
`release/latest/windows/`, including canonical download URLs and SHA-256
hashes. The GitHub Release assets remain canonical.

Default hotkeys:

- Dictation: `Ctrl+Win`
- Assist: `Win+Alt`
- Voice Agent: `Ctrl+Shift`

## Quick Start

Embed the Go backend:

```bash
go get github.com/kombifyio/SpeechKit
```

Import only the components your host needs. A dictation-only app can use
`pkg/speechkit/dictation`; an activation-only integration can use
`pkg/speechkit/wakeword`; spoken output can use `pkg/speechkit/tts`; one-shot
Voice Companion hosts use `pkg/speechkit/companion` plus Assist/TTS adapters;
speaker-aware apps can use `pkg/speechkit/speaker`; server-connected apps use
`pkg/speechkit/client`. You do not need to load the Windows client or the whole
framework for a single component.

To drive the framework from a `config.toml` instead of building settings by
hand, `pkg/speechkit/hostconfig` turns a config file into the public
`ModeSettings` and a starting `RuntimePolicy` in one call:

```go
settings, policy, err := hostconfig.Load("config.toml")
```

Real providers run **in-process — no SpeechKit server required**: realtime
Voice Agents via `pkg/speechkit/voiceagent/live` (Gemini Live, OpenAI Realtime,
Deepgram), speech-to-text via `pkg/speechkit/stt` (whisper.cpp, OpenAI, Groq,
Google, Deepgram, AssemblyAI, Hugging Face, OpenRouter), text-to-speech via
`pkg/speechkit/tts` (OpenAI, Google, Deepgram, Hugging Face, Piper), and a
turn-based cascaded Voice Agent via `pkg/speechkit/voiceagent/cascaded`. Two
runnable references:

```bash
# in-process Voice Agent (Gemini Live), no server:
GOOGLE_AI_API_KEY=... go run ./examples/voice-agent/in-process
# in-process Assist (host-owned LLM + optional public TTS), no server:
GOOGLE_AI_API_KEY=... go run ./examples/assist/in-process
```

For a single-prompt Go starter:

```bash
speechkit-cli init --template go-assist-voice-companion ./my-companion
speechkit-cli init --template go-voice-agent-companion ./my-agent
speechkit-cli init --template go-dictation-handsfree-ui ./my-dictation-ui
```

Run the self-host server image:

```bash
docker pull ghcr.io/kombifyio/speechkit-server:latest
```

Use the agent tools:

```bash
go run ./cmd/speechkit-mcp --mode=docs,test
go run ./cmd/speechkit-cli status --server http://localhost:8080 --token "$SPEECHKIT_SERVER_TOKEN"
```

## Documentation

This README is the short orientation page. Use the detailed docs when you need
contracts, deployment steps, or release rules:

- [Docs index](./docs/README.md)
- [Framework API](./docs/speechkit-framework-api.md)
- [Words And Replacements standard](./docs/words-and-replacements-standard.md)
- [Voice Capability Matrix](./docs/capabilities/voice-capability-matrix.json)
- [Local OpenAPI](./docs/api/openapi.v1.yaml)
- [SpeechKit Server docs](./docs/server/README.md)
- [SpeechKit Server OpenAPI](./docs/server/openapi.v1.yaml)
- [SpeechKit MCP docs](./docs/mcp/README.md)
- [Agent entrypoint](./docs/agent/llms.txt)
- [Changelog](./CHANGELOG.md)

## Build

Public source verification:

```bash
go test ./pkg/... ./cmd/speechkit-cli/... ./cmd/speechkit-mcp/... ./examples/...
GOOS=linux CGO_ENABLED=0 go test ./cmd/speechkit-server/...
GOOS=linux CGO_ENABLED=0 go build ./cmd/speechkit-server ./cmd/speechkit-mcp ./cmd/speechkit-cli
```

Windows client builds are shipped as release assets from the maintained
release pipeline. The installer is the recommended distribution format for
end users. For clone-and-install testing on Windows, use
`release/latest/windows/INSTALLER-MANIFEST.json` to resolve the current
`SpeechKit-Setup.exe` download URL and verify it against `SHA256SUMS.txt`.

## Repository Layout

```text
pkg/speechkit/          Local-first Go backend
cmd/speechkit-server/   Self-host Server entry point
cmd/speechkit-mcp/      MCP server for agent docs, validation, and management
cmd/speechkit-cli/      CLI diagnostics, scaffolding, and quick actions
internal/               Implementation packages for the public binaries
docs/                   Detailed documentation
deploy/                 Docker and server config
scripts/                Public install and release-note helpers
```

## Trust

Public releases include checksums and an unsigned Windows notice while the
no-cost unsigned release path is active. Download only from the official
[kombifyio/SpeechKit releases](https://github.com/kombifyio/SpeechKit/releases).

## License

Apache-2.0. See [LICENSE](./LICENSE).
