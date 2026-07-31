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

// Through the kombify Gateway
const gatewayClient = new SpeechKitClient({
  baseUrl: "https://api.kombify.io",
  basePath: "/v1/speechkit",
  token: auth0AccessToken,
});

const result = await client.transcribe(audioBlob, { language: "en" });
console.log(result.text);
```

See `docs/clients/typescript.md` in the
[SpeechKit repository](https://github.com/kombifyio/SpeechKit) for full usage,
including the voice agent WebSocket companion package
`@kombifyio/speechkit-voiceagent-client`.

Licensed under the Apache License 2.0.
