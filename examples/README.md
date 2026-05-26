# SpeechKit Examples

These examples show the public framework surface that OSS consumers can import.

- `library/`: embeds the dictation recording and transcription pipeline with host-provided adapters.
- `provider-catalog/`: reads the v23 mode contracts and provider catalog used by host applications.
- `embed-companion/`: composes wake detections, Assist requests, optional TTS, and runtime events through the v0.40.1 SDK surface.
- `embed-tts/`: demonstrates `pkg/speechkit/tts` Provider, Router, Service, and provider-kind routing.
- `embed-event-bus/`: publishes and consumes the additive wake, skill, companion, Voice-Agent, and TTS events.
- `voice-agent/game-instructor/`: end-to-end 15-minute Voice Agent embedded in a Go program (persona/role/sequence TOML + WebSocket client). Reference for the single-prompt "build a voice agent into my app" use case.

Run an example from the repository root:

```bash
go run ./examples/provider-catalog
go run ./examples/embed-companion
go run ./examples/embed-tts
go run ./examples/embed-event-bus
```
