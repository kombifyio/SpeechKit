# @kombifyio/speechkit-client

TypeScript REST client for the SpeechKit Server v1 API: dictation transcribe,
assist, voice agent session minting, TTS, catalog, vocabulary, and transcripts.

```ts
import { SpeechKitClient } from "@kombifyio/speechkit-client";

// Direct server access
const client = new SpeechKitClient({
  baseUrl: "http://localhost:8080",
  token: process.env.SPEECHKIT_TOKEN,
});

// Behind a reverse proxy that mounts the server under a path prefix
const proxiedClient = new SpeechKitClient({
  baseUrl: "https://speech.example.com",
  basePath: "/v1/speechkit",
  token: accessToken,
});

const result = await client.transcribe(audioBlob, { language: "en" });
console.log(result.text);
```

See `docs/clients/typescript.md` in the
[SpeechKit repository](https://github.com/kombifyio/SpeechKit) for full usage,
including the voice agent WebSocket companion package
`@kombifyio/speechkit-voiceagent-client`.

## Mastra voice

The optional Node export `@kombifyio/speechkit-client/mastra` implements Mastra's
`MastraVoice` using this client's STT, TTS and configured-voices routes. Install
`@mastra/core` alongside the client when using this export. The normal client
does not load Mastra. The supported peer range starts at `1.75.0` and stays below
`2.0.0`.

```ts
import { Agent } from "@mastra/core/agent";
import { SpeechKitClient } from "@kombifyio/speechkit-client";
import { SpeechKitVoice } from "@kombifyio/speechkit-client/mastra";

const voice = new SpeechKitVoice({ client: SpeechKitClient.fromEnv() });
const agent = new Agent({
  id: "voice-assistant",
  name: "Voice assistant",
  instructions: "Answer briefly.",
  model: "openai/gpt-4.1-mini",
  voice,
});
```

`voice.listen(nodeReadableOrBlob, { filename: "speech.wav", language: "de-DE" })`
returns transcript text. `voice.speak("Hallo", { format: "wav", locale: "de-DE" })`
returns a Node `Readable` carrying the server's actual `format`, `sample_rate`
and other TTS metadata. The provider may return a different format than requested.
`speaker` maps to the server's `voice` field. Provider selection stays server-side.

Inputs are buffered before the REST call, with limits of 25 MiB for encoded audio
and 256 KiB for UTF-8 text. Both methods accept `signal`, `timeoutMs` and `headers`.
The timeout covers the HTTP request. Use an abort signal to bound input-stream
collection too. Stream errors and HTTP errors propagate to the caller.

`getSpeakers()` returns the server's configured snapshot with `voiceId` added to
each entry. This snapshot may omit providers and does not query vendor catalogues.
`getListener()` advertises adapter capability, without checking provider readiness.
`connect()`, `send()` and `answer()` reject realtime use. Use the Voice Agent client
for live sessions, interruption and audio events. The host owns microphone capture,
playback and consent. A local SpeechKit server requires no Kombify account, and
the adapter adds no cloud fallback or provider credentials.

The packaged [WAV conversation example](examples/mastra-voice.mjs) reads a recording,
runs a Mastra agent and saves the spoken reply. It needs a configured SpeechKit
server and credentials for the agent's selected model. From a source checkout:

```bash
cd clients/typescript
pnpm install --frozen-lockfile
pnpm --filter @kombifyio/speechkit-client run build
node packages/client/examples/mastra-voice.mjs recording.wav reply.wav
```

Set `SPEECHKIT_SERVER_URL` and `SPEECHKIT_TOKEN` for the server. For a reverse
proxy set `SPEECHKIT_BASE_PATH` to its mounted prefix, such as `/v1/speechkit`.
`SpeechKitClient.fromEnv()` also reads these variables. The adapter does not
refresh tokens. `MASTRA_MODEL` selects the agent model and defaults to
`openai/gpt-4.1-mini`, which uses `OPENAI_API_KEY` through Mastra.
Source and packed examples do not establish npm publication or Worker support.

Licensed under the Apache License 2.0.
