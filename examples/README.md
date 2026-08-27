# SpeechKit Examples

These examples show the public framework surface that OSS consumers can import.
They are intentionally component-level examples: copy the packages you need,
not the entire desktop application.

- `library/`: embeds the dictation recording and transcription pipeline with host-provided adapters.
- `provider-catalog/`: reads the current mode contracts and provider catalog used by host applications.
- `embed-companion/`: composes wake detections, explicit Hands-Free target routing, Assist requests, optional TTS, and runtime events through the public SDK surface.
- `embed-tts/`: demonstrates `pkg/speechkit/tts` Provider, Router, Service, and provider-kind routing.
- `embed-event-bus/`: publishes and consumes the additive wake, skill, companion, Voice-Agent, and TTS events.
- `assist/in-process/`: fully in-process Assist — wires a real Gemini Generator into `pkg/speechkit/assist.Service` with optional OpenAI TTS output, no SpeechKit server.
- `voice-agent/game-instructor/`: end-to-end 15-minute Voice Agent embedded in a Go program (persona/role/sequence TOML + WebSocket client). Reference for the single-prompt "build a voice agent into my app" use case.
- `voice-agent/in-process/`: fully in-process Voice Agent — constructs a `live.LiveProvider` (Gemini Live) and drives a text dialogue with no SpeechKit server in the path.
- `voice-agent/provider-switching/`: credential-free provider/profile/model selection using the public `pkg/speechkit/voiceagent/live` descriptors, `ProviderIntent`, and config helpers.
- `agentbridge-codex/`: External Coding Agent Bridge in exec mode — one prompt in, normalized events out, against the real codex CLI or the fakecodex fixture.
- `kombify-box-satellite/`: Go host for the kombify box following the SpeechKit Hands-Free contract (USB mic -> sherpa-KWS wakeword -> optional Hands-Free/Assist flow -> box speaker).
- `box-evidence-harness/`: headless fake box driving the exact Voice-Agent WebSocket path the kombify-Box firmware uses, reporting the runtime facts the firmware hardcodes against.

## Agent prompts

Assist Voice Companion:

```text
Add a SpeechKit Assist Voice Companion to this Go app. Use examples/embed-companion as the reference, import only pkg/speechkit/{companion,wakeword,assist,tts} plus pkg/speechkit for events, and wire companion.NewHandsFree with TargetMode: companion.TargetAssist. The host owns mic capture and playback. Do not import internal/*.
```

Voice Agent companion:

```text
Build a hands-free SpeechKit Voice Agent companion into this app. Use companion.TargetVoiceAgent for wake activation and examples/voice-agent/game-instructor for the realtime session pattern. Use pkg/speechkit/client when talking to speechkit-server, or pkg/speechkit/agentkit when embedding a Go Voice Agent harness. Do not load the Windows client.
```

Dictation UI-assisted activation:

```text
Add hands-free Dictation activation to this UI app with companion.TargetDictationUIAssisted. Wake can start/stop capture, but committing text must go through a visible target or explicit UI action. Do not synthesize TTS for Dictation.
```

Run an example from the repository root:

```bash
go run ./examples/provider-catalog
go run ./examples/embed-companion
go run ./examples/embed-tts
go run ./examples/embed-event-bus
go run ./examples/voice-agent/provider-switching -provider assemblyai
go run ./examples/voice-agent/provider-switching -provider auto -preferred openai,assemblyai -require-native-keyterms
```
