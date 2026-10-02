# SpeechKit

[![Go Reference](https://pkg.go.dev/badge/github.com/kombifyio/SpeechKit.svg)](https://pkg.go.dev/github.com/kombifyio/SpeechKit)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.26%2B-00ADD8.svg)](go.mod)

SpeechKit is an open-source, local-first speech framework. Its platform-neutral
Go kernel handles dictation, one-shot voice assistance and realtime voice
conversations over a pluggable set of speech-to-text, text-to-speech, LLM and
realtime providers. Embed it in your own Go program, run it as a self-hosted
server, or drive it from the included CLI and MCP server.

> **Beta.** Public APIs, config keys and defaults can still change between
> minor releases; pin versions. Breaking changes are called out in
> [CHANGELOG.md](CHANGELOG.md).

## What is in this repository

| Path | What it is |
| --- | --- |
| `pkg/speechkit/` | The Go SDK: mode contracts, provider adapters, routing, wake word, TTS, client |
| `app/cmd/speechkit-server/` | Self-hosted Linux server with HTTP/WebSocket APIs (OpenAPI and AsyncAPI contracts in `docs/server/`) |
| `app/cmd/speechkit-cli/` | CLI for diagnostics, scaffolding and quick actions against a server |
| `app/cmd/speechkit-mcp/` | MCP server so coding agents can read the docs, validate payloads and operate a server |
| `clients/typescript/` | TypeScript server client, realtime voice client and a web-component voice UI kit |
| `android/` | Android library modules, keyboard (IME) and assistant app |
| `examples/` | Small runnable programs, one per integration pattern |
| `deploy/` | Dockerfile, Compose files and server configuration templates |
| `docs/` | Framework, server, MCP and API documentation |

The SDK (`pkg/speechkit/...`) is the root Go module. The reference apps
(`app/cmd`, `app/internal`) are the nested module `.../app`, which requires the
root module; the committed `go.work` joins both, so run app commands from the
repository root with `./app/...` paths. SDK code never imports the app module.

## Modes

| Mode | Purpose | Boundary |
| --- | --- | --- |
| Dictation | Turn speech into text. | STT only. No LLM rewriting. |
| Assist | Turn speech or text into one useful result. | Codeword, utility or LLM output, optional TTS. |
| Voice Agent | Realtime audio-to-audio dialogue. | Live conversation with a realtime provider. |

Hands-Free is an activation layer over the three modes (wake word, capture,
auto-end and spoken output), not a fourth mode. Words and Replacements
customize recognition and apply deterministic text transformations; see the
[Words and Replacements standard](docs/words-and-replacements-standard.md).

## Providers

Local: whisper.cpp, Piper, Ollama-style local LLM endpoints and sherpa-onnx wake
words. Cloud (bring your own key): OpenAI, Groq, Deepgram, AssemblyAI, Hugging
Face, OpenRouter, Azure AI / Foundry, Google Cloud STT/TTS and Gemini Live. A
fresh install runs local-only with no cloud keys. The
[provider option matrix](docs/capabilities/provider-option-matrix.json) and the
[voice capability matrix](docs/capabilities/voice-capability-matrix.json) list
what each provider supports.

## Quick start

Embed the Go kernel:

```bash
go get github.com/kombifyio/SpeechKit
```

Import only what your host needs: `pkg/speechkit/dictation`,
`pkg/speechkit/wakeword`, `pkg/speechkit/tts`, `pkg/speechkit/companion`
(one-shot voice companion hosts), `pkg/speechkit/speaker` and
`pkg/speechkit/client` (server-connected apps). To configure from a file:

```go
settings, policy, err := hostconfig.Load("config.toml")
```

A missing file yields the local-only defaults; a malformed file returns an
error wrapping `hostconfig.ErrMalformedConfig`. `config.example.toml` lists
every key.

Run a provider in-process, no server needed:

```bash
OPENAI_API_KEY=... go run ./examples/voice-agent/in-process
```

Run the self-hosted server:

```bash
docker pull ghcr.io/kombifyio/speechkit-server:latest
```

Compose files and configuration templates are in `deploy/`; the
[server documentation](docs/server/README.md) and
[deployment notes](docs/server/DEPLOY.md) cover modes, authentication and
providers. Public binds must use an authenticated `auth_mode`; never expose
`auth_mode = "none"`.

Agent tooling:

```bash
go run ./app/cmd/speechkit-mcp --mode=docs,test
go run ./app/cmd/speechkit-cli status --server "$SPEECHKIT_SERVER_URL" --token "$SPEECHKIT_SERVER_TOKEN"
```

## Verify a change

```bash
go test ./pkg/... ./app/cmd/speechkit-cli/... ./app/cmd/speechkit-mcp/... ./examples/...
GOOS=linux CGO_ENABLED=0 go build ./app/cmd/speechkit-server ./app/cmd/speechkit-mcp ./app/cmd/speechkit-cli
```

The full set is in [CONTRIBUTING.md](CONTRIBUTING.md). Most of `pkg/speechkit`
is pure Go; packages that need cgo or an external binary are listed in the
[SDK surface boundary](docs/architecture/sdk-surface-boundary.md) and fail with
a typed error when the dependency is absent.

## Documentation

| Document | Purpose |
| --- | --- |
| [docs/README.md](docs/README.md) | Documentation index |
| [docs/sdk/README.md](docs/sdk/README.md) | SDK in 10 minutes |
| [docs/sdk/custom-provider.md](docs/sdk/custom-provider.md) | Add your own STT, TTS or live provider |
| [docs/speechkit-framework-api.md](docs/speechkit-framework-api.md) | Framework API contracts |
| [docs/server/README.md](docs/server/README.md) | Server target |
| [docs/mcp/README.md](docs/mcp/README.md) | MCP server |
| [android/README.md](android/README.md) | Android modules |
| [examples/README.md](examples/README.md) | Runnable examples |

## Releases

Windows and macOS desktop builds, the server image
`ghcr.io/kombifyio/speechkit-server` and release notes are published on
[GitHub Releases](https://github.com/kombifyio/SpeechKit/releases). Desktop
builds are not code-signed yet; each release carries checksums and an
`UNSIGNED-*-RELEASE.txt` notice. Download only from the official releases page.

## Contributing and support

Issues and pull requests are welcome; see [CONTRIBUTING.md](CONTRIBUTING.md),
[SUPPORT.md](SUPPORT.md), [SECURITY.md](SECURITY.md) and the
[Code of Conduct](CODE_OF_CONDUCT.md).

## License

Apache-2.0, see [LICENSE](LICENSE). The Android keyboard links a GPL-3.0 fork of
HeliBoard; see [android/GPL-NOTICE.md](android/GPL-NOTICE.md).
