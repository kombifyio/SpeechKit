# @kombifyio/speechkit-voiceagent-client

TypeScript WebSocket client for the SpeechKit Voice Agent:

- ticket sessions (`POST /v1/voiceagent/sessions` + `Sec-WebSocket-Protocol: ticket.<ticket>`)
- full JSON control-frame protocol (`state`, `input_transcript`, `output_transcript`,
  `tool_call`, `sequence_step`, `event`, `interrupted`, `error`, `session_end`,
  `ping`/`pong`, `audio_end`)
- browser microphone capture resampled to 16 kHz S16 LE mono
  (AudioWorklet, ScriptProcessor fallback) and 24 kHz playback
- Node variant on top of the optional `ws` peer dependency

```ts
import { openBrowserSession } from "@kombifyio/speechkit-voiceagent-client";

const session = await openBrowserSession({
  serverUrl: "https://api.kombify.io",
  basePath: "/v1/speechkit",
  token: auth0AccessToken,
  resolveWsUrl: (t) =>
    `wss://api.kombify.io/v1/speechkit/voiceagent/sessions/${t.session_id}/ws`,
  // `provider` selects the realtime backend for this session
  // (gemini | openai | deepgram | assemblyai | cascaded); empty = server default.
  start: { locale: "en-US", provider: "gemini" },
  onPlaybackLevel: (level) => setAgentLevel(level), // RMS 0..1 for visualizers
  hooks: {
    onAgentTranscript: (text, done) => render(text, done),
    onAudio: (chunk) => session.playChunk(chunk),
    // Barge-in: drop queued agent audio so stale speech stops immediately.
    onInterrupted: () => session.flushPlayback(),
  },
});

const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
const stopMic = session.attachMicrophone(stream);
```

See `docs/clients/typescript.md` in the
[SpeechKit repository](https://github.com/kombifyio/SpeechKit) for full usage,
including the REST companion package `@kombifyio/speechkit-client`.

Licensed under the Apache License 2.0.
