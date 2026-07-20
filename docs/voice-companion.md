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
- `pkg/speechkit/assist/skills` for the ready-made Voice-Companion skill
  catalog (Time, Date, Math, Weather, Timer, Reminder, Wikipedia, plus an
  optional Home Assistant bridge) as an `assist.ToolMatcher` +
  `assist.ToolExecutor` — wire the real framework skills instead of writing
  your own keyword router. A recognized smart-home utterance is terminal:
  missing Home Assistant configuration, no-match, and execution errors must
  fail closed instead of falling through to a general LLM.
- `pkg/speechkit/tts` for spoken output routing.
- `pkg/speechkit/agentkit` when your host also needs tool registration,
  lifecycle hooks, or session memory.

Component-level imports are intentional:

| Need | Import |
| --- | --- |
| Wake activation only | `pkg/speechkit/wakeword` |
| Spoken output only | `pkg/speechkit/tts` |
| One-shot utilities or LLM Assist | `pkg/speechkit/assist` |
| Ready-made skill catalog (Time/Math/Weather/HA/…) | `pkg/speechkit/assist/skills` |
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

## Smart-home authority boundaries

The optional Home Assistant skill in the general Assist catalog is distinct
from the dedicated G0 device-agent bridge:

- **General Assist skill:** participates in the normal one-shot Assist matcher
  chain. Once a transcript is recognized as a smart-home utterance, Home
  Assistant is the sole semantic authority. An unconfigured bridge, a Home
  Assistant no-match response, or any Home Assistant error returns a terminal,
  fail-closed result. The transcript must never fall through to a general LLM
  for reinterpretation or action.
- **Realtime host integration:** a model-selected tool name is not authority.
  Use `agentkit.LifecycleHooks.AuthorizeToolCall` to fail closed before
  asynchronous dispatch, synchronously at tool-event arrival. First buffer the
  complete host capture, reject loss, truncation, cancellation, and source
  closure, then batch-transcribe that immutable capture and write exactly one
  host transcript seal for the active turn. Provider streaming transcripts are
  observability only. Require the host capture/`EndAudioStream` boundary, bind
  the call and result to the active session, narrow arguments to the sealed host
  transcript/locale, and allow at most one smart-home call per turn. Spoken
  output still requires a correlated terminal Home Assistant receipt;
  output-only gating is too late to prevent a side effect. Local Whisper is the
  only authority STT; unavailable local STT prevents the local realtime
  runtime from starting, and a per-turn STT/capture failure suppresses all
  provider playback. Never fall back to a provider transcript: it could
  misclassify a smart-home request as general and simulate a result. Start a
  monitored capture subscription atomically at the wake event before any
  provider connection, but exclude all pre-detection audio. Establish a clean
  post-wake silence boundary from ordered PCM samples before accepting user
  speech; reject an utterance already in progress rather than trimming it
  ambiguously. Do not play an audible cue inside this window. Mark captures
  overlapping any host playback as contaminated, and derive trailing silence
  and truncation from ordered PCM sample counts. This prevents setup latency,
  queued corrections, ambient pre-wake commands, and provider/TTS echo from
  producing an authoritative seal.
- **G0 device-agent bridge:** bypasses general Assist entirely. It accepts only
  an explicitly named static local light rule over the independently paired
  `/v1/device-agent/assist` route, commits its durable at-most-once claim before
  dispatch, and requires an exact Home Assistant target plus REST state
  readback. It has no LLM, MCP, Gateway, or federation fallback.

See [`device-agent.md`](device-agent.md) for the dedicated local protocol and
its narrower authorization contract.

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
