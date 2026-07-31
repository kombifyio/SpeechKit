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
  start: { locale: "en-US" },
  hooks: {
    onAgentTranscript: (text, done) => render(text, done),
    onAudio: (chunk) => session.playChunk(chunk),
  },
});

const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
const stopMic = session.attachMicrophone(stream);
```

See `docs/clients/typescript.md` in the
[SpeechKit repository](https://github.com/kombifyio/SpeechKit) for full usage,
including the REST companion package `@kombifyio/speechkit-client`.

Licensed under the Apache License 2.0.
