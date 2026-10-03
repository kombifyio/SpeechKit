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

const abortController = new AbortController();
const session = await openBrowserSession({
  serverUrl: "https://speech.example.com",
  basePath: "/v1/speechkit",
  token: accessToken,
  resolveWsUrl: (t) =>
    `wss://speech.example.com/v1/speechkit/voiceagent/sessions/${t.session_id}/ws`,
  // `provider` selects the realtime backend for this session
  // (openai | deepgram | assemblyai | cascaded, or opt-in BYOK gemini); empty = server default.
  start: { locale: "en-US", provider: "deepgram" },
  signal: abortController.signal, // cancels ticket minting, setup, and active tools
  onPlaybackLevel: (level) => setAgentLevel(level), // RMS 0..1 for visualizers
  hooks: {
    onAgentTranscript: (text, done) => render(text, done),
    onAudio: (chunk) => session.playChunk(chunk),
  },
});

const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
const stopMic = session.attachMicrophone(stream);
```

The browser and Node factories resolve after the server confirms both
`state: "listening"` and `event_type: "session_ready"`. Ticket minting expires
after 15 seconds, socket upgrade after 10 seconds, and provider readiness
after 20 seconds from upgrade. Direct `VoiceAgentSession` users can await `ready`.
Microphone bytes sent before readiness are discarded rather than buffered.
Pass an `AbortSignal` to cancel pending minting, setup, and the active session.
Tool handlers also receive `{ signal }` as their second argument; late results
after cancellation, interruption, or termination are never sent.

Hosts may override `connectTimeoutMs`, `readyTimeoutMs`, and `maxOutboundBytes`
on either opener or `VoiceAgentSession`. Values must be finite and positive;
deadlines may be at most 2,147,483,647 ms and the outbound budget at most
960,000 bytes (30 seconds of microphone PCM). Invalid values keep the defaults
and fractional values are rounded down. Browser hosts can inject `createWebSocket` and
`createAudioContext` for their existing device adapters. A `playback` adapter
can carry a context resumed in an earlier user gesture, avoiding another
speaker context when authentication happens before opening. It implements
`play`, `flush`, `resume`, and `dispose`; cleanup calls `dispose` once, so a
host that owns the underlying player can implement disposal as a flush.

Call `openBrowserSession` directly from a click/tap to resume playback during
that gesture. If the browser suspends audio later, call `resumeAudio()` from a
new click/tap. `session.cancel()` flushes scheduled playback synchronously and
mutes in-flight PCM until the server acknowledges interruption. Closing or
receiving a terminal frame stops capture tracks, clears playback, and closes
the local socket without waiting for the peer.
Attempting playback while the context is suspended ends the session with
`audio_resume_required`; resume audio before supplying playback chunks.

Outbound WebSocket buffering is limited to 64,000 bytes (two seconds of
microphone PCM); control frames are limited to 64 KiB. Transcript callbacks
forward the complete delta within that wire limit, preserving prefixes and turn
content; hosts that accumulate turns should bound their own retained text. Browser playback retains
at most 96,000 PCM bytes (two seconds) and 64 scheduled sources. Exceeding a
media/control budget terminates with `voice_buffer_overflow`. Error frames
without `fatal: true` remain recoverable; fatal `auth_expired` terminates with
`authorization_expired`, and other fatal errors terminate with `error`. Local
transport/tool failures expose stable codes without raw exceptions or HTTP
response bodies. The server's structured error/remediation fields remain
available through `onError`.

See `docs/clients/typescript.md` in the
[SpeechKit repository](https://github.com/kombifyio/SpeechKit) for full usage,
including the REST companion package `@kombifyio/speechkit-client`.

Licensed under the Apache License 2.0.
