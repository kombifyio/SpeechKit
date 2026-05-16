# SpeechKit Examples

These examples show the public framework surface that OSS consumers can import.

- `library/`: embeds the dictation recording and transcription pipeline with host-provided adapters.
- `provider-catalog/`: reads the v23 mode contracts and provider catalog used by host applications.
- `voice-agent/game-instructor/`: end-to-end 15-minute Voice Agent embedded in a Go program (persona/role/sequence TOML + WebSocket client). Reference for the single-prompt "build a voice agent into my app" use case.

Run an example from the repository root:

```bash
go run ./examples/provider-catalog
```
