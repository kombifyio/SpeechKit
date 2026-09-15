# Technical Getting Started

This guide is for engineers setting up SpeechKit directly.

## Choose an integration path

- Embedded Go: https://github.com/kombifyio/SpeechKit/blob/main/docs/sdk/README.md.
  Use `dictation.NewService` with `stt.AsTranscriber`, a recorder and output.
  Root `pkg/speechkit` holds contracts; orchestration and catalog live in
  `pipeline` and `catalog`. Local WAV transcription needs a whisper.cpp runtime
  and model, not a SpeechKit Server or cloud key.
- TypeScript: https://github.com/kombifyio/SpeechKit/tree/main/clients/typescript.
  `@kombifyio/speechkit-client` covers REST; `speechkit-voiceagent-client` covers
  realtime sessions; `speechkit-voice-ui` provides custom elements. Follow each
  package's installation instructions and pin versions. `/assistant` on a
  SpeechKit Server provides a ready-to-use voice surface.
- Android: https://github.com/kombifyio/SpeechKit/tree/main/android. Use public
  Kotlin `core`, `net`, `domain` and Compose modules via JitPack. The reusable
  modules are Apache-2.0; the HeliBoard reference APK has a separate GPL boundary.
- Desktop: Windows supports the full app; macOS 14+ arm64 is a Dictation-only
  ad-hoc signed beta. Read release notes and macOS setup instructions before
  installing: https://github.com/kombifyio/SpeechKit/releases/latest.

## Install the SpeechKit Server

Stable:

```sh
curl -fsSL https://speechkit.cc/install-server.sh | sh
```

The installer writes:

- `docker-compose.yml`
- `config.toml`
- `.env`
- a persistent `data/` directory

Default install directory: `/opt/speechkit`.

The default Docker Compose port binding is `127.0.0.1:8080:8080`. For public
servers, put SpeechKit behind a TLS reverse proxy and run the installer with
`--public-bind` only when the host should listen on all interfaces.

## Verify

```sh
cd /opt/speechkit
docker compose ps
curl -fsS http://localhost:8080/healthz
curl -fsS http://localhost:8080/readyz
```

Authenticated API calls use the generated `SPEECHKIT_SERVER_TOKEN` from
`/opt/speechkit/.env`.

## Use the API

OpenAPI:

```text
https://speechkit.cc/api/openapi.v1.yaml
```

Voice Agent AsyncAPI:

```text
https://speechkit.cc/api/asyncapi.v1.yaml
```

Core endpoints:

- `POST /v1/dictation/transcribe`
- `POST /v1/assist/process`
- `POST /v1/voiceagent/sessions`
- `GET /v1/customization/templates`
- `GET /v1/customization/templates/{templateId}/pack`
- `GET /v1/catalog/profiles`
- `GET /v1/catalog/providers`
- `GET /v1/config`
- `POST /v1/tts/synthesize`

## Use Go

```sh
go get github.com/kombifyio/SpeechKit
```

Use `pkg/speechkit/client` when you want to call a running SpeechKit Server.

Use the public SDK packages when you embed SpeechKit directly into another Go
host. Import the smallest public component that matches the job instead of
loading the whole framework:

```sh
go run ./examples/embed-companion
go run ./examples/embed-tts
go run ./examples/embed-event-bus
```

Important packages:

- `pkg/speechkit/wakeword` and `pkg/speechkit/wakeword/sherpa` for wake-word contracts.
- `pkg/speechkit/companion` for hands-free target routing with `TargetAssist`, `TargetVoiceAgent`, or `TargetDictationUIAssisted`.
- `pkg/speechkit/tts` for Provider, Router, Service, and provider-kind routing.
- `pkg/speechkit/assist` for one-shot Assist services, multi-turn skill context, codeword routing, and optional Genkit adapters.
- `pkg/speechkit/dictation` for strict STT-only embedded dictation.
- `pkg/speechkit/customize` for Words, Replacements, Lexicons, Rulesets, and
  portable Customization Packs. Native Templates are curated pack sources
  selected through `active_template_ids`.
- `pkg/speechkit/agentkit` and `pkg/speechkit/voiceagent/live` for embedded realtime Voice Agent hosts.

Hands-Free is an activation and voice-output layer, not a fourth mode. Voice
Companions are usually `TargetAssist`; continuous dialogue companions use
`TargetVoiceAgent`; Dictation uses `TargetDictationUIAssisted` because text
still needs a visible target or explicit commit surface.

## Embed Meeting

`pkg/speechkit/meeting` owns capture lifecycle and public transcript/notes
helpers. The host supplies capture pipelines and commits transcript segments.
From a SpeechKit checkout, run:

```sh
go run ./examples/meeting/synthetic-host
```

This is a synthetic composition demo: no microphone, STT call or generated AI
review. Start with https://speechkit.cc/getting-started/agents/meeting-notes-go.md.
The full capture, notepad and review workflow is available in the Windows app;
do not invent Meeting routes on the standard server or assume macOS parity.

## Use MCP

```json
{
  "mcpServers": {
    "speechkit": {
      "command": "speechkit-mcp",
      "args": ["--mode=docs,test"]
    }
  }
}
```
