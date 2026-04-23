# SpeechKit Examples

These examples show the public framework surface that OSS consumers can import.

- `library/`: embeds the dictation recording and transcription pipeline with host-provided adapters.
- `provider-catalog/`: reads the v23 mode contracts and provider catalog used by host applications.

Run an example from the repository root:

```bash
go run ./examples/provider-catalog
```
