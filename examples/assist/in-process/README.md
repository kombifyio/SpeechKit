# assist/in-process

Fully in-process Assist: text (or speech) in, one useful result out, no
SpeechKit server. It wires a real LLM Generator (Gemini via the genai SDK)
into `pkg/speechkit/assist.Service`, and — when an OpenAI key is present —
the public `pkg/speechkit/tts` router for spoken output.

## Requirements

- `GOOGLE_AI_API_KEY` (a Google AI Studio key) — required.
- `OPENAI_API_KEY` — optional; enables spoken output through the public TTS
  router.
- `-model` flag selects the Gemini generateContent model (default
  `gemini-2.5-flash`).

## Run

```bash
GOOGLE_AI_API_KEY=... go run ./examples/assist/in-process
# optional spoken output: also set OPENAI_API_KEY
```

## Expected output

An interactive stdin loop. Type a request and press Enter; the assistant
answers with one or two short sentences per line (`assist: ...`). With TTS
enabled, each answer also reports the synthesized audio size
(`(+N bytes of <format> audio)`). An empty line quits.
