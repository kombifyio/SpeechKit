# Voice Companion

Hands-Free is an activation and voice-output capability for SpeechKit's three
strict modes, not a fourth mode. It combines wake activation, microphone
capture, auto-end policy, and optional speaker output.

Voice Companion is the Assist-flavored hands-free experience. It combines
wake-word activation, host-provided transcription, Assist, optional TTS, and
event publication into a Siri/Alexa-style one-shot flow.

## Public SDK

Use these packages:

- `pkg/speechkit/companion` for the hands-free composer.
- `pkg/speechkit/wakeword` for activation events and auto-end policy.
- `pkg/speechkit/assist` for one-shot Assist processing contracts.
- `pkg/speechkit/tts` for spoken output routing.
- `pkg/speechkit/agentkit` when your host also needs tool registration,
  lifecycle hooks, or session memory.

Component-level imports are intentional:

| Need | Import |
| --- | --- |
| Wake activation only | `pkg/speechkit/wakeword` |
| Spoken output only | `pkg/speechkit/tts` |
| One-shot utilities or LLM Assist | `pkg/speechkit/assist` |
| Hands-Free composition | `pkg/speechkit/companion` |
| Realtime Voice Agent tools/session harness | `pkg/speechkit/agentkit`, `pkg/speechkit/voiceagent/live` |
| Self-host server client | `pkg/speechkit/client` |
| Mode contracts, events, readiness | `pkg/speechkit` |

Do not import a wider package just to reach one primitive. For example, a host
that only needs wake events should import `wakeword`, not `companion`.

## Config

New hosts should use `[hands_free]` as the source of truth and keep
`[wakeword]` for detector-level compatibility:

```toml
[hands_free]
enabled = true
activation_phrase_id = "hey_quby"
target_mode = "assist" # assist | voice_agent | dictation_ui_assisted
auto_end_silence_cutoff_sec = 10
voice_output_enabled = true
```

`dictation_ui_assisted` always keeps spoken output off. It may wake and start
capture, but text still commits through a visible target or explicit UI action.

The host owns microphone capture, device permissions, local UI, playback, and
the chosen hands-free target. SpeechKit owns the contracts that keep wake-word,
Assist, Voice Agent, TTS, and events composable across desktop apps, self-host
services, and embedded Go hosts.

## Targets

| Target | Experience | UI expectation |
| --- | --- | --- |
| `assist` | Voice Companion: one-shot utilities, smart-home commands, spoken answers. | Can run with minimal or no visible UI. |
| `voice_agent` | Continuous realtime dialogue for companion, game moderator, or brainstorming flows. | Can run with minimal or no visible UI. |
| `dictation_ui_assisted` | Wake-triggered Dictation start/stop. | Requires a visible text target or explicit commit surface. |

## Flow

```text
Mic or host event
  -> wake-word or explicit activation
  -> target mode selection
  -> host transcript function
  -> Assist service
  -> optional TTS router
  -> companion events + host action
```

For self-host scenarios, call the server's Assist endpoint or connect through
`pkg/speechkit/client`. For embedded Go scenarios, compose the public SDK
interfaces directly and keep provider initialization inside your host.

## Single-Prompt Agent Recipes

Use these prompts when a coding agent should add a companion experience without
reverse-engineering the repository.

### Assist Voice Companion

```text
Add a SpeechKit Assist Voice Companion to this Go app. Import only public packages from github.com/kombifyio/SpeechKit/pkg/speechkit/...: companion, wakeword, assist, tts, and speechkit for events. Wire companion.NewHandsFree with TargetMode: companion.TargetAssist. The host owns microphone capture and playback; SpeechKit owns wake event contracts, transcript request, Assist routing, optional TTS, and EventBus publication. Do not import internal/* or the Windows client.
```

### Voice Agent Companion

```text
Add a SpeechKit hands-free Voice Agent companion to this app. Use companion.NewHandsFree with TargetMode: companion.TargetVoiceAgent for wake activation, and use pkg/speechkit/client for a running speechkit-server or pkg/speechkit/agentkit for an embedded Go Voice Agent harness. Keep persona/role/sequence configuration in host-owned config, stream PCM 16 kHz S16LE mono into the session, and do not use internal/* packages.
```

### Dictation UI-Assisted Activation

```text
Add hands-free Dictation activation to this UI app. Import pkg/speechkit/wakeword, pkg/speechkit/companion, and pkg/speechkit for mode commands/events. Wire companion.NewHandsFree with TargetMode: companion.TargetDictationUIAssisted. A wake event may start or stop capture, but the transcript must commit through a visible text target or explicit UI action. Do not synthesize TTS for Dictation.
```

## Agent-Native Usage

Agents should start from the docs and examples instead of reverse-engineering
implementation packages:

```bash
go run ./cmd/speechkit-mcp --mode=docs,test
go run ./cmd/speechkit-cli init --template go-assist-voice-companion ./my-companion
go run ./cmd/speechkit-cli init --template go-voice-agent-companion ./my-agent
go run ./cmd/speechkit-cli init --template go-dictation-handsfree-ui ./my-dictation-ui
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
