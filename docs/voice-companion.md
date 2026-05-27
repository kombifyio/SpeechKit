# Voice Companion

Voice Companion is an agent-style composition pattern, not a fourth SpeechKit
mode. It combines wake-word activation, host-provided transcription, Assist,
optional TTS, and event publication into a hands-free flow.

## Public SDK

Use these packages:

- `pkg/speechkit/companion` for the hands-free composer.
- `pkg/speechkit/wakeword` for activation events and auto-end policy.
- `pkg/speechkit/assist` for one-shot Assist processing contracts.
- `pkg/speechkit/tts` for spoken output routing.
- `pkg/speechkit/agentkit` when your host also needs tool registration,
  lifecycle hooks, or session memory.

The host owns microphone capture, device permissions, local UI, and playback.
SpeechKit owns the contracts that keep wake-word, Assist, TTS, and events
composable across desktop apps, self-host services, and embedded Go hosts.

## Flow

```text
Mic or host event
  -> wake-word or explicit activation
  -> host transcript function
  -> Assist service
  -> optional TTS router
  -> companion events + host action
```

For self-host scenarios, call the server's Assist endpoint or connect through
`pkg/speechkit/client`. For embedded Go scenarios, compose the public SDK
interfaces directly and keep provider initialization inside your host.

## Agent-Native Usage

Agents should start from the docs and examples instead of reverse-engineering
implementation packages:

```bash
go run ./cmd/speechkit-mcp --mode=docs,test
go run ./examples/embed-companion
```

Useful entrypoints:

- [Framework API](speechkit-framework-api.md)
- [MCP docs](mcp/README.md)
- [Agent entrypoint](agent/llms.txt)
- [Voice Agent game example](../examples/voice-agent/game-instructor/README.md)

## Boundary

Import `pkg/speechkit/...` packages from external applications. Go `internal`
packages in this repository are implementation details for the source tree's
own server, CLI, MCP, and release-built Windows client.
