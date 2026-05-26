# Voice-Companion via Assist-Mode

> **Heads-up (2026-05-26):** The v0.40.1 implementation branch exposes the
> wake-word kernel, TTS service, and "Wake + host transcript + Assist + Skills + AutoEndPolicy"
> assembly as public SDK packages: `pkg/speechkit/wakeword`,
> `pkg/speechkit/wakeword/sherpa`, `pkg/speechkit/tts`, and
> `pkg/speechkit/companion`. Release waits for the v0.40.0 tag/API baseline. See the
> [v0.40.1 roadmap section](roadmap/v0.40-and-beyond-enterprise-modularity.md#v0401--sdk-surface-modularity-track-a-continuation)
> and the `kombify-SpeechKit-jsn` beads epic.

This document describes the always-listening, wake-word-triggered voice-companion
pattern. It is **not a fourth SpeechKit mode** — it is the existing
[Assist-Mode](../internal/assist/) used in a hands-free configuration plus a
catalog of voice-oriented skills.

The same Assist-Mode that powers the dictation app's "Hey Quby → utility →
spoken reply" flow is the surface you embed in a Linux container, a Mini-PC,
or a Wails desktop build to get Alexa/Siri-style behaviour. There is no UI
requirement — Assist is callable headless via `POST /v1/assist/process` and the
Result schema carries an `Action` field for execution and an `Audio` field for
TTS playback. The dictation UI is one consumer of that schema; a tray-only
voice-companion build is another. SDK embedders provide the capture/transcript
step through `companion.WakeRequestFunc`; `NewHandsFree` owns the wake event,
Assist call, optional TTS synthesis, and event publication.

## Why Assist, not a new mode

The Assist pipeline is **STT → Router (Intent | Utility | LLM) → optional TTS →
Result{Text, Audio, Action, …}** — see [internal/assist/pipeline.go](../internal/assist/pipeline.go).

That is semantically identical to the Alexa/Siri pipeline. Adding a fourth mode
would duplicate that code path. Instead, voice-companion-specific behaviour is
introduced as additional **skills** registered against the same
`UtilityRegistry` ([internal/assist/registry.go](../internal/assist/registry.go))
that today carries the text-utility tools (`copy_last`, `insert_last`,
`summarize`, `quick_note`).

Voice-Agent mode remains untouched. Voice-Agent is multi-hour by design and
covers realtime/diarization use-cases (11Seconds Party Mode, kombify-AI
Companion Live). When a user wants "ask once, get an answer" semantics, they
configure wake-word's `default_mode` to `assist` and the existing
wake-word dispatcher routes the synthetic hotkey event into the Assist
controller.

## Pipeline

```
[Mic]                  capture loop running while wakeword.enabled
   |
   v
[Wake-Word Sidecar]    sherpa-onnx KWS / openWakeWord / STT-phrase
   |  DetectionEvent{phrase="Hey Quby", mode="assist"}
   v
[Dispatcher]           pkg/speechkit/wakeword.Dispatcher (desktop uses an internal adapter)
   |  HotkeyEvent{KeyDown:true, Binding:"assist", Source:"wakeword"}
   v
[Mode-Hotkey-Manager]  (existing) routes to assist handler when Binding="assist"
   |
   v
[Assist-Activation]    cmd/speechkit/desktop_assist_activation.go (Phase 1)
   |  opens audio.Session, attaches AutoEndPolicy
   v
[STT]                  internal/stt/* (whisper.cpp, OpenAI, Groq, Google, HF, VPS)
   |  transcript
   v
[Assist Router]        internal/assist/router.go
   |   |
   |   +-- IntentMatch (shortcut catalog hit)
   |   |        |
   |   |        v
   |   |   [Skill Executor]  internal/assist/skills/voice_companion/*
   |   |        |    time | weather | timer | reminder | math | wikipedia | ha (P4)
   |   |        v
   |   |   ToolResult{Text, SpeakText, Action="execute"}
   |   |
   |   +-- No match
   |            |
   |            v
   |       [Assist LLM Flow]  internal/ai/flows/assist.go (Genkit)
   |            |
   |            v
   |       AssistOutput{Text, SpeakText, Action="respond"}
   |
   v
[TTS Router]           internal/tts/* (Google, OpenAI, Piper P3)
   |
   v
[Audio Player]         internal/audio/player.go (oto v3) on Device-Target
                       JSON{audio_b64} on Server-Target
   |
   v
[Speaker]
```

`AutoEndPolicy` (silence cutoff + exit phrases) from
`pkg/speechkit/wakeword` terminates the
session after the spoken reply finishes or on silence — same contract as
wake-word→voice_agent today.

## Skill Surface

Voice-companion skills are `ToolExecutor` implementations
([internal/assist/types.go:60-62](../internal/assist/types.go)) registered into
the default `UtilityRegistry`. Each skill maps one or more `shortcuts.Intent`
values to a single executor.

### Skills (released through v0.38.2)

| Intent | Skill | Trigger Phrases (DE / EN excerpt) | Backend | Released |
|---|---|---|---|---|
| `time` | time | "wie spät", "uhrzeit" / "what time", "current time" | local — `time.Now()` | v0.37.0 |
| `date` | time | "welcher tag", "datum" / "what day", "today's date" | local | v0.37.0 |
| `weather` | weather | "wetter", "wie wird das wetter" / "weather", "forecast" | [Open-Meteo](https://open-meteo.com) (free, no key) | v0.37.0 |
| `timer` | timer | "stell einen timer", "timer auf X" / "set a timer", "timer for X" | local in-memory + Store-Persist | v0.37.0 |
| `reminder` | reminder | "erinnere mich an X um Y" / "remind me to X at Y" | Store-Persist + Background-Worker | v0.37.0 |
| `math` | math | "was ist 15 mal 8" / "what is 15 times 8" | local arithmetic evaluator | v0.37.0 |
| `wikipedia` | wikipedia | "erzähl mir was über X" / "tell me about X" | Wikipedia summary API | v0.37.0 |
| `home_assistant` | home_assistant | "schalte X", "turn on/off X" → `POST {ha_url}/api/conversation/process` | Home Assistant Conversation API | v0.37.0 (kernel) · v0.38.2 (Settings UI) |
| (no-match) | LLM-Fallback | open questions | Gemini Flash / configured Assist LLM | v0.37.0 |

Skill registration happens during Assist-Pipeline construction; each skill
sits behind a `UtilityDefinition` flag and can be disabled per deployment via
`[assist].enabled_tools` in TOML.

The HomeAssistant skill is wired automatically when both `[assist.home_assistant].url`
and `[assist.home_assistant].token_env` resolve to non-empty values; otherwise
HA-style requests fall through to the LLM. v0.38.2 adds a Settings UI section
(Settings → Integrations → "Home Assistant Bridge") that posts URL/Token-Env
and a `POST /settings/homeassistant/test` probe button.

## Wake-Word Wiring

No new dispatcher code is needed — the existing wake-word dispatcher already
synthesizes hotkey events for any mode. Setting
`[wakeword].default_mode = "assist"` in TOML routes detections to the Assist
controller via the existing mode-hotkey-manager.

The Device-Target needs a `routeAssistHotkey` handler analogous to
[cmd/speechkit/desktop_voice_agent_activation.go](../cmd/speechkit/desktop_voice_agent_activation.go).
The handler:

1. Opens an audio capture session via `internal/audio`
2. Attaches an `AutoEndPolicy` (silence-cutoff 10 s + exit-phrases)
3. Streams PCM to the configured STT provider
4. On final transcript, calls `Pipeline.Process(ctx, transcript, opts)`
5. Plays back `Result.Audio` via `internal/audio/player.go` (oto v3)
6. Ends the session

The Server-Target already exposes `POST /v1/assist/process` —
[internal/server/assist/handler.go](../internal/server/assist/handler.go) —
which accepts an audio upload, runs the same pipeline, and returns
JSON{text, audio_b64, action}. No new server endpoint is needed for
Phase 1. Phase 5 adds satellite endpoints for multi-room streaming.

## Configuration

Voice-Companion does not introduce a new config block. It uses the existing
sections:

```toml
[wakeword]
enabled = true
phrase_id = "hey_quby"          # or any catalog entry
default_mode = "assist"          # routes wake-word → Assist
backend = "livekit_openwakeword" # default — uses bundled per-phrase ONNX

  [wakeword.auto_end]
  silence_cutoff_sec = 10
  # exit_phrases defaults to DE+EN closers ("stop", "danke", "thanks", ...)

[assist]
enabled_tools = [
  "copy_last", "insert_last", "summarize", "quick_note",
  # voice-companion skills:
  "time", "weather", "timer", "reminder", "math", "wikipedia",
]

[model_selection.assist]
primary_profile_id = "assist.builtin.gemma4-e4b"
# OR cloud:
# primary_profile_id = "assist.cloud.gemini-flash"

[tts]
enabled = true
# default voice and provider already in [tts.providers.*]

[model_selection.tts]
# Voice-Output picker. Pins which TTS provider Assist + Voice-Agent
# use when speaking back. v0.37.2+ — see pkg/speechkit/catalog.go for
# the available profile IDs (tts.google.studio-o-de is the v0.37
# recommended default; tts.openai.tts-1-hd is an HQ fallback).
primary_profile_id = "tts.google.studio-o-de"
fallback_profile_id = "tts.openai.tts-1-hd"

[audio]
# existing audio device selection applies to playback
```

## Three Deployment Targets

Voice-Companion runs in all three SpeechKit deployment targets per the
three-target test matrix:

| Target | What runs | Test command |
|---|---|---|
| Device-Target (Wails) | Tray app + wake-word + Assist + audio output | `scripts/build.ps1 -SkipInstaller` |
| Server-Target (Linux container) | HTTP `/v1/assist/process` only (no wake-word) | `docker build -f deploy/docker/Dockerfile.server` |
| Local-Library (Go embed) | Public `pkg/speechkit/{wakeword,companion,assist,tts}` packages imported by host binary | `go test ./pkg/speechkit/...` |

Per [docs/wakeword.md](wakeword.md), wake-word does not run in the Server-Target
itself. Multi-room deployments (Phase 5) put wake-word on the satellite, not the
hub.

## Latency Budgets

Phase 1 targets (Single-Node, Cloud-LLM-fallback enabled):

| Use-Case | Total | Stages |
|---|---|---|
| "Wie spät" (local skill) | < 1.5 s | wake 200 ms · capture 800 ms (silence) · skill 50 ms · TTS 400 ms |
| "Wie wird das Wetter" | < 2.5 s | + Open-Meteo HTTP 400 ms |
| Open Q&A (LLM fallback) | < 4 s | + Gemini Flash 1500–2000 ms |
| "Stell einen Timer 5 Min" | < 1.5 s | local skill, no LLM |

All-local mode (Ollama assist LLM + Piper TTS, no GPU) typically multiplies
LLM-fallback latency by 2–6× versus cloud Gemini. Latency tracking goes into
`internal/audit` so users can see per-stage breakdown.

## Phase Roadmap (current state)

| Phase | Scope | Status |
|---|---|---|
| 0 | Doc, intent catalog, skill registry stubs | done — v0.37.0 |
| 1 | Six skills (time, weather, timer, reminder, math, wikipedia) + Device-Target assist activation + audio playback | done — v0.37.0 |
| 2 | Multi-turn skills with 60 s context store ("Timer" → "Wie lange?" → "5 Minuten") | done — v0.38.0 |
| 3 | All-local TTS via Piper subprocess (operators run `scripts/prepare-piper-voices.{ps1,sh}`) | done — v0.38.0 (folded from v0.39.0) |
| 4 | Home Assistant skill (Conversation API) + Settings UI (URL + Token + Test connection) + per-locale Piper voice picker | done — v0.37.0 (kernel) · v0.38.2 (Settings UI + voice picker) |
| 5 | Multi-room satellite topology (LiveKit / ESPHome-API) | not started |

Piper TTS expects voice models under `cfg.TTS.Piper.VoiceDir`; download via
`scripts/prepare-piper-voices.ps1` (Windows) or `prepare-piper-voices.sh`
(Linux). The Settings UI under Settings → Integrations → "Piper Local Voices"
lists installed `.onnx` files and lets users pin a default voice per locale
(en, de) via the `[tts.piper].default_voices` map.
for detailed deliverables per phase (local path; not committed to repo).

## See Also

- [docs/wakeword.md](wakeword.md) — wake-word framework, backends, training pipeline
- [docs/speechkit-architecture-v2.md](speechkit-architecture-v2.md) — three-mode framework
- [docs/speechkit-framework-api.md](speechkit-framework-api.md) — public SDK contracts
- [docs/architecture/sdk-surface-boundary.md](architecture/sdk-surface-boundary.md) — SDK/internal boundary rules
- [examples/embed-companion/](../examples/embed-companion/) — Local-Library companion composer example
- [examples/embed-tts/](../examples/embed-tts/) — TTS SDK example
- [examples/embed-event-bus/](../examples/embed-event-bus/) — Event-Bus subscription example
- [internal/assist/](../internal/assist/) — Assist pipeline source
- [pkg/speechkit/wakeword/](../pkg/speechkit/wakeword/) — public Wake-Word kernel + AutoEndPolicy
- [internal/shortcuts/](../internal/shortcuts/) — Intent catalog (where new skills land)
