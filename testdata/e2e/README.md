# Install-E2E Audio Fixtures

These WAV files are 16 kHz S16 mono ~2 s recordings used by the install-E2E
pipelines:

| File | Spoken content | Used by |
|---|---|---|
| `dictation/hello-world.wav` | "Hello world." | Dictation mode (English) |
| `dictation/hallo-welt.wav` | "Hallo Welt." | Dictation mode (German) |
| `assist/utility-uppercase.wav` | "uppercase test phrase." | Assist codeword utility |
| `assist/llm-shortq.wav` | "What is two plus two?" | Assist LLM free-form |
| `voiceagent/turn1.wav` | "My name is Marcel." | Voice Agent turn 1 |
| `voiceagent/turn2.wav` | "What is my name?" | Voice Agent turn 2 (context test) |

## Expected outputs

`*.expected.txt` files hold the assertion patterns. Patterns starting
with `(?` or `^` are interpreted as Go regular expressions; everything
else is compared case-insensitive, whitespace-trimmed.

`*.expected-prefix.txt` holds an optional substring assertion for
non-deterministic LLM outputs.

## Regeneration

```pwsh
doppler run -p kombination -c prd -- pwsh ./scripts/generate-e2e-fixtures.ps1
```

Generation requires `OPENAI_API_KEY` (Doppler injects it) and `ffmpeg`
on PATH. CI install-E2E runners NEVER need the key — they consume the
committed WAVs.

## Why these are committed to git

Total size is ~250 KB and they change very rarely. Committing avoids
making every CI run depend on the OpenAI TTS endpoint and credentials.
SHA-256 pinning is intentionally NOT used because Whisper STT is robust
across subtle TTS rerenders — the assertion regex is the contract,
not the audio bytes.

## Adding a new fixture

1. Append an entry to `$fixtures` in `scripts/generate-e2e-fixtures.ps1`.
2. Re-run the script via Doppler.
3. Write a matching `.expected.txt` (regex) or rely on programmatic
   assertions in `cmd/sk-localprobe` / `cmd/sk-e2e`.
4. Commit the new WAV + expected files together.
