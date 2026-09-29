# Local-Only Server Stack and OpenAI-Compatible Audio

This page is the wiring contract for running `speechkit-server` with local
providers only — the speech module of a self-hosted private-AI stack such as
the StackKits Private AI use case — and for letting OpenAI-audio clients such
as Open WebUI use it directly.

## Container set

| Service | Image | Role | Port | License |
| --- | --- | --- | --- | --- |
| `speechkit-server` | `ghcr.io/kombifyio/speechkit-server` | Dictation, Assist, Voice Agent, OpenAI-compatible audio | 8080 | Apache-2.0 |
| `speechkit-whisper` | `ghcr.io/ggml-org/whisper.cpp` (pinned digest) running `whisper-server` | STT | 8080 (internal) | MIT; Whisper weights MIT |
| `speechkit-tts` | `ghcr.io/remsky/kokoro-fastapi-cpu:v0.9.0` (pinned digest) | TTS over OpenAI `/v1/audio/speech` | 8880 (internal) | Apache-2.0; Kokoro-82M weights Apache-2.0 |
| `ollama` | `ollama/ollama:0.34.4` | Assist and Voice Agent LLM | 11434 (internal) | MIT; model licenses vary |

[`deploy/docker/docker-compose.local-only.yml`](../../deploy/docker/docker-compose.local-only.yml)
wires the set, and
[`deploy/config/server.local-only.toml`](../../deploy/config/server.local-only.toml)
is the server profile it mounts. Only `speechkit-server` publishes a port;
the sidecars stay on the compose network. An existing Ollama can replace the
`ollama` service: change `[local_llm] base_url`.

The Kokoro-FastAPI container runs `espeak-ng` (GPL-3.0) as a separate
phonemizer program. It is not linked into SpeechKit.

### Why Kokoro and not Piper

SpeechKit's in-process Piper adapter (`[tts.piper]`) runs the `piper` CLI as
a subprocess. That CLI is the archived MIT `rhasspy/piper`, whose maintained
successor `OHF-Voice/piper1-gpl` is GPL-3.0. The default voices
`en_US-amy-medium` and `de_DE-thorsten-medium` are fine-tuned from the
`lessac` voice, whose training data carries a restrictive Blizzard 2013
license. The server image also does not bundle Piper. Kokoro-82M is
Apache-2.0 end to end, so it is the default.

Kokoro has no German voice. For German, run [Speaches](https://github.com/speaches-ai/speaches)
(MIT) instead of Kokoro-FastAPI. It serves the same `/v1/audio/speech` API
with Piper voices. Point `[tts.local] url` at it and set `voice` to the
Speaches voice ID.

## Server profile

`server.local-only.toml` enables only the following:

- **STT:** `[vps]` sends requests to `http://speechkit-whisper:8080`. The
  `cloud-only` routing strategy is the name of the setting that selects this
  network client. It does not enable any cloud provider.
- **TTS:** `[tts.local]` sends requests to `http://speechkit-tts:8880` with
  model `kokoro` and voice `af_bella`. The `[tts] strategy` is `local-only`.
- **LLM:** `[local_llm]` sends requests to `http://ollama:11434/v1` with
  model `gemma3:4b`. Any OpenAI-compatible server works here, including
  llama.cpp `llama-server`.
- **Voice Agent:** `cascaded` (local STT, then LLM, then TTS).
- **Auth:** `bearer` using `SPEECHKIT_SERVER_TOKEN`.

Every cloud provider section is explicitly `enabled = false`.
`[privacy] network_scope = "local_network"` additionally suspends cloud TTS
and restricts the TTS sidecar to private addresses. The server's STT and LLM
wiring does not consume the network scope yet. On those paths, the
guarantee is the disabled provider sections plus the absence of cloud keys.
`GET /v1/deployment/status` reports `providers.cloud_keys_present`. The
Voice Agent's per-session provider switch still lists the cloud realtime
providers. Without their keys, those sessions fail at start.

### Environment

| Variable | Required | Purpose |
| --- | --- | --- |
| `SPEECHKIT_SERVER_TOKEN` | yes | Bearer token for every `/v1` route, including the OpenAI-compatible ones |
| `SPEECHKIT_PUBLIC_URL` | no | Public origin; also added to the CORS allow-list |
| `SPEECHKIT_WHISPER_MODEL_FILE` | no | whisper.cpp model file (default `ggml-large-v3-turbo.bin`; `ggml-small.bin` on small hosts) |
| `SPEECHKIT_SERVER_IMAGE`, `SPEECHKIT_WHISPER_IMAGE`, `SPEECHKIT_TTS_IMAGE`, `SPEECHKIT_OLLAMA_IMAGE` | no | Image overrides |

Do not set any cloud key variable (`OPENAI_API_KEY`, `GROQ_API_KEY`,
`DEEPGRAM_API_KEY`, `ASSEMBLYAI_API_KEY`, `HF_TOKEN`, `GOOGLE_AI_API_KEY`,
`OPENROUTER_API_KEY`).

### Env-only alternative

You can skip the config file and use the self-hosted defaults instead
(`SPEECHKIT_SELFHOSTED_DEFAULTS=true`) by setting these variables:

- `SPEECHKIT_SELFHOSTED_STT_URL`
- `SPEECHKIT_SELFHOSTED_LLM_BASE_URL`
- `SPEECHKIT_SELFHOSTED_LLM_MODEL`
- `SPEECHKIT_SELFHOSTED_TTS_URL`
- `SPEECHKIT_SELFHOSTED_TTS_VOICE`

Self-hosted defaults rewrite the LLM model `gemma4:e4b` to the llama.cpp GGUF
default. With Ollama, use the config file or pick a different model tag.

## OpenAI-compatible audio endpoints

Point the OpenAI client's base URL at the server with a `/v1` suffix, for
example `http://speechkit-server:8080/v1`. Use `SPEECHKIT_SERVER_TOKEN` as
the API key. These routes use the same auth, rate limits and provider
routing as the native routes. Their contract is in
[`openapi.v1.yaml`](openapi.v1.yaml) under the `openai-compat` tag.

### `POST /v1/audio/transcriptions`

- **Request:** a multipart `file` part. Optional fields are `model`,
  `language`, `prompt` and `response_format`. The route also accepts the
  JSON `input_audio` variant.
- **Response:** the default is `{"text": "..."}`. `response_format` can
  also be `text` or `verbose_json`.
- **Kernel path:** the same as `/v1/dictation/transcribe`: decode,
  customization, routing and transcript persistence.
- **`model`:** pins a provider only when it is a Dictation provider-profile
  ID. `whisper-1` and an empty value both use the configured routing.
- **Audio formats:** WAV, MP3, WebM/Opus and OGG/Opus. The format comes from
  the part Content-Type, then the file extension, then sniffing. MP4/M4A is
  not supported.
- **Silence:** returns `{"text": ""}` instead of an error.

### `POST /v1/audio/speech`

- **Request:** JSON with `input` (required, at most 4096 characters), plus
  optional `voice`, `response_format` (`mp3` by default; also `wav`, `opus`
  or `pcm`) and `speed`.
- **Response:** raw audio bytes. The `Content-Type` header reports the format
  actually produced. Piper always returns WAV. `X-SpeechKit-TTS-Provider`
  names the provider that produced the audio.
- **`model`:** ignored. The configured TTS routing picks the provider.
- **Voices:** OpenAI stock voice names such as `alloy` and `nova` select the
  provider's default voice. Any other name is passed through to the provider.

### Open WebUI

These settings are for Open WebUI v0.11.3, which sends only `model` and
`language` for STT, and only `model`, `input` and `voice` for TTS:

```env
AUDIO_STT_ENGINE=openai
AUDIO_STT_OPENAI_API_BASE_URL=http://speechkit-server:8080/v1
AUDIO_STT_OPENAI_API_KEY=${SPEECHKIT_SERVER_TOKEN}
AUDIO_STT_MODEL=whisper-1
AUDIO_TTS_ENGINE=openai
AUDIO_TTS_OPENAI_API_BASE_URL=http://speechkit-server:8080/v1
AUDIO_TTS_OPENAI_API_KEY=${SPEECHKIT_SERVER_TOKEN}
AUDIO_TTS_MODEL=tts-1
AUDIO_TTS_VOICE=af_bella
```

Open WebUI fills its voice and model pickers from `GET /audio/voices` and
`GET /audio/models`, and it sends those requests without the API key. The
server keeps every route authenticated, so Open WebUI falls back to its
built-in OpenAI list. The configured `AUDIO_TTS_VOICE` still applies.
Open WebUI transcodes any non-MP3 response to MP3 itself.
