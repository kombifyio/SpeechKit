# voice-agent/in-process

Fully in-process Voice Agent: no SpeechKit server in the path. The realtime
provider runs inside the binary — a `live.LiveProvider` (Gemini Live) wrapped
in a `live.Session` drives a text dialogue, with STT, the LLM turn, and TTS
all happening provider-side. It is the in-process counterpart to
`examples/voice-agent/game-instructor`, which drives a running
speechkit-server over WebSocket.

## Requirements

- `GOOGLE_AI_API_KEY` (a Google AI Studio key with Gemini Live access) —
  required. No audio device is needed; agent audio frames are counted, not
  played.

## Run

```bash
GOOGLE_AI_API_KEY=... go run ./examples/voice-agent/in-process
```

## Expected output

After connecting, an interactive stdin loop. Each line you type is sent as a
turn; the session prints state transitions (`[state=...]`), the transcript of
what the agent heard (`you (heard): ...`), the agent's spoken answer
(`agent: ...`), and on exit the total agent audio received
(`session ended cleanly (received N bytes of agent audio).`). An empty line
or EOF ends the session.
