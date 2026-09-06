# kombify-companion

Go host for the kombify box following the SpeechKit hands-free contract.
The current default path runs locally on the host machine and deliberately
still without speechkit-server:
Box USB microphone -> sherpa KWS wake word -> optional SpeechKit
hands-free/Assist flow -> Box speaker.

The Voice Agent mode (`[mode].target = "voice_agent"`) has two transports:

- **`[voice_agent].transport = "local"` (default)**: the Box talks directly
  to the realtime provider (Deepgram Voice Agent, Gemini Live, AssemblyAI,
  OpenAI); no speechkit-server needed. Tools (`home_assistant` via
  toolbridge) run client-side; `think_endpoint_url`/`think_model`/
  `think_api_key_env` switch the kombify AI Gateway in as the BYO brain of
  the Deepgram agent. The provider key comes from `DEEPGRAM_API_KEY` or
  `ASSEMBLYAI_API_KEY`/`GOOGLE_AI_API_KEY`/`OPENAI_API_KEY`.
  Idle teardown defaults to a 90 s reminder / 3 min deactivate (cost control;
  Deepgram bills connection time).
- **`transport = "server"`**: mic PCM 16 kHz S16LE -> speechkit-server
  Voice Agent WebSocket -> 24 kHz PCM back to the Box speaker.

For pure device/wake-word tests, `[mode].target = "wake_only"` uses only the
microphone, the local KWS and the Box status, without calling STT/Assist/TTS.

## Setup

```powershell
powershell -File tools\get-model.ps1       # sherpa KWS model (once)
powershell -File tools\make-keywords.ps1   # keywords.txt + keywords.jarvis.txt
powershell -ExecutionPolicy Bypass -File examples\kombify-box-satellite\run-companion.ps1
```

Notes:
- cgo is required (sherpa-onnx). On Windows use a MinGW toolchain.
- At **runtime** the sherpa-onnx DLLs (`sherpa-onnx-c-api.dll`,
  `onnxruntime.dll`, ...) must be on the `PATH`. They live in the Go module
  cache under `sherpa-onnx-go-windows@<ver>\lib\x86_64-pc-windows-gnu\`; put
  that folder on the `PATH` before starting (otherwise `0xc0000135` /
  "DLL not found").
- `config.toml` is expected at start (path optional as the first argument).
- The Box ring UI is driven directly by the companion: the
  `companion.Options.OnStage` hook writes `KBX <state>` lines to the CDC
  status port (boxlink.go, auto-detected through USB VID/PID 303A:8000;
  override via `[box].status_port` or `KOMBIFY_BOX_STATUS_PORT`, `"off"`
  disables it). run-companion.ps1 only stages env/DLLs now.
- Devices are chosen by name substring (`[box]` in config.toml); the default
  "kombify box" matches the firmware's USB device.
- Secrets are resolved through the normal SpeechKit secret convention:
  environment variable first (Deepgram always `DEEPGRAM_API_KEY`), optional
  Doppler CLI fallback via `DOPPLER_PROJECT`/`DOPPLER_CONFIG`.
  The companion sets `SPEECHKIT_DISABLE_PORTABLE=1` so that it uses the
  global SpeechKit store despite the example `config.toml`.
- `[speechkit_server]` points to `https://speechkit.kombify.io` by default.
  It is only used once `[mode].target = "voice_agent"` is set.
  `[voice_agent].provider = "deepgram"` selects Deepgram on the server; the
  Deepgram key belongs on the speechkit-server, not in this companion.
- Short feedback signals are synthesized by the Companion. Assist and the
  server Voice Agent use the wake cue before capture; the local realtime HA
  path deliberately uses only the Ring UI so self-generated audio cannot enter
  the authority capture. The two-stage understood cue follows only after the
  capture is closed. No SD card is required.
- STT can run locally or directly through Deepgram. For Deepgram, set in the
  local `config.toml`:

  ```toml
  [stt]
  provider    = "deepgram"
  model       = "nova-3"
  api_key_env = "DEEPGRAM_API_KEY"
  language    = "de"
  ```

  `DEEPGRAM_API_KEY` must then be available in the environment, in the local
  SpeechKit secret store or through the configured Doppler source; the
  companion then starts no local `whisper-server.exe`.
- The local STT alternative uses `whisper-server.exe` from
  `%LOCALAPPDATA%\SpeechKit`.
- Assist/TTS run local-first in the current Box profile: a local
  OpenAI-compatible LLM endpoint on `http://127.0.0.1:8082/v1` and optional
  Piper TTS.
- The local Whisper model is looked up automatically under
  `%LOCALAPPDATA%\SpeechKit\models\ggml-small.bin`. Alternatively set
  `[local].model_path`.
- Piper is looked up automatically under `%LOCALAPPDATA%\SpeechKit\piper...`;
  voices live in `%LOCALAPPDATA%\SpeechKit\piper-voices` by default.
- If the local LLM runtime has not started yet, local skills keep working;
  open questions return a setup hint instead of a gateway error.
- The local default uses `keywords.jarvis.txt` for "hey jarvis"/"jarvis".
  The file was generated with `tools/encode-keywords` from the current
  GigaSpeech KWS `tokens.txt`, so no Python tokenizer is needed.
- Inline `Keywords` in the wake-word detector are BPE-tokenized automatically
  by the framework (`wakeword.EncodeKeywords`); an explicit `keywords_file`
  is validated at start (raw text -> a clear error instead of a silent
  never-match).

## Wake-word smoke test

The small test tool checks a 16 kHz mono PCM16 WAV against exactly the same
SpeechKit/sherpa pipeline code the companion uses:

```powershell
go build -o examples\kombify-box-satellite\kws-smoke.exe ./examples/kombify-box-satellite/tools/kws-smoke
examples\kombify-box-satellite\kws-smoke.exe `
  --model-dir examples\kombify-box-satellite\models\sherpa-onnx-kws-zipformer-gigaspeech-3.3M-2024-01-01 `
  --keywords examples\kombify-box-satellite\keywords.jarvis.txt `
  --wav C:\path\to\hey-jarvis-16k-mono.wav `
  --phrase "hey jarvis"
```

Expectation: at least one `DETECTED` line. If the smoke test fires but the
live test does not, the cause is the real microphone audio, distance or
pronunciation, or the tuning of `[wakeword].threshold` and
`[wakeword].input_gain`. `input_gain` amplifies only the wake-word detector
path; the actual recording stays raw.

## Smart-Home: Home Assistant

Home Assistant is the sole semantic authority for commands recognized by the
deterministic Home Assistant lexicon in one-shot Assist and the realtime Voice
Agent. This is an intentionally exact capability boundary, not a claim that an
open-ended semantic classifier can recognize every implied smart-home phrase.
Missing configuration, no-match, and Home Assistant failures produce a
terminal local result. A recognized request is never reinterpreted by the
Gateway, an MCP, or the realtime model.

```toml
[home_assistant]
base_url  = "https://homeassistant.home.arpa:8123"
token_env = "KOMBIFY_HA_TOKEN"   # Long-Lived Access Token; never commit it
language  = "de"                  # optional; otherwise request locale
```

```powershell
$env:KOMBIFY_HA_TOKEN = "<long-lived-access-token>"
```

The bearer token may reach only a local Home Assistant origin. Plain HTTP is
accepted only for literal loopback/`localhost`; LAN addresses and local DNS
names require HTTPS. DNS targets are checked again after resolution, environment
proxies and redirects are disabled, and raw Home Assistant response bodies are
never surfaced.

The realtime `home_assistant` tool is installed even when the integration is
not configured. Its description and framework instruction require the model to
call it for every command recognized by the deterministic lexicon. Prompting is
not the authorization boundary: synchronously when a tool event arrives and
before asynchronous dispatch, the host requires a write-once transcript seal
derived by batch STT from the complete local PCM capture. Only a normal silence
boundary with zero local fan-out drops is eligible for a seal; cancellation,
source closure, maximum-duration truncation, empty/error STT, or any dropped
microphone frame leaves Home Assistant fail-closed. Streaming provider
transcripts are diagnostic input only and can neither create nor replace the
seal. The host also requires `EndAudioStream` to have completed for the exact
turn and the model's query to match the sealed text after whitespace-only
normalization. It replaces all model arguments with the sealed host transcript
and locale and authorizes at most one Home Assistant call for that turn. A
server-VAD tool call arriving before the host capture closes is terminally
denied and never reaches the executor. Box playback then remains buffered until
the
host has a terminal result receipt for the exact tool call, session, and turn,
and the final output transcript matches the authoritative Home Assistant text.
Missing host seals, stale sessions or receipts, early audio, mismatches,
additional calls, and indeterminate turns discard all model audio and use only
the local error cue. Late or multiple provider transcript segments do not alter
the host seal. General, deterministically non-HA turns continue through the
realtime conversation unchanged unless they contain a rejected smart-home tool
call.

The Box accepts only local Whisper as the batch STT authority for this seal and
starts it before opening a local realtime session. A configured remote/gateway
STT provider is never reused and never becomes a fallback for realtime Home
Assistant authorization. This keeps transcript authority independent from the
conversation provider and preserves the local SpeechKit-to-Home-Assistant
boundary without cloud federation. If local Whisper or its model is
unavailable, the local realtime runtime refuses to start. It does not fall back
to provider transcripts even for apparently general turns: otherwise a remote
provider could mislabel a smart-home request and speak a simulated result
without a Home Assistant receipt.

The HA capture subscribes atomically at the wake event before provider
connection, but deliberately excludes all pre-detection audio from authority.
It first requires 250 ms of clean, sample-counted silence and then real user
speech; a command already in progress when detection arrives is rejected rather
than partially transcribed. Local realtime uses the Ring UI instead of an
audible wake cue. Every host playback epoch marks all overlapping capture
subscriptions contaminated, so provider/TTS echo can never authorize a turn.
Buffer overflow is counted and invalidates authority instead of silently
truncating the command. Silence and maximum duration are measured from ordered
PCM sample counts, not consumer wall-clock time, so a queued correction is
consumed before the silence boundary can seal.

Audio buffers are generation-scoped. Out-of-turn audio is accepted only after
the local idle timer synchronously announces a trusted reminder/deactivation
prompt; a late audio callback or stale `listening` transition cannot create or
complete a playback generation. Every HA-classified or indeterminate turn
retires its realtime session after the decision, so callbacks from a provider
response without a portable response ID cannot be mistaken for a later idle
reminder. The next wake starts a fresh session. Providers without final output
transcription fail closed for the realtime HA surface even when the host input
seal is valid.

> **Migration (v0.49, applied):** `skills.go` now delegates the deterministic
> skills (time, date, math, weather, timer, reminder, Wikipedia, temperature)
> to the public catalog package `pkg/speechkit/assist/skills`
> (`companionskills.New(...)`); only `help`/`status` stay Box-local. Timer and
> reminder fire for real through the built-in scheduler (`OnAlarm` → `audio.Ding()`),
> `router.Close()` cleans them up during shutdown. `hass.go` adapts the same hardened
> public Home Assistant boundary for the realtime tool; it no longer contains a
> separate HTTP implementation or a no-match-to-model fallback.
>
> Verified with a local cgo build and test (`CGO_ENABLED=1 CC="zig cc" go
> build/vet/test ./examples/kombify-box-satellite/`). CI
> (`.github/workflows/example-box-satellite.yml`) builds and vets the example
> (Windows/mingw); `go test` does not run there because the binary links
> sherpa-onnx + onnxruntime as shared libraries (runtime DLLs are a device-target matter).

## Wake-word robustness
- Wake detection pauses during ding + recording + TTS playback
  (`Pipeline.Pause()/Resume()`), which prevents self-triggering.
- The quick local test uses "hey jarvis" because the phrase is phonemically
  more distinct than the single-word wake word "kombify" and can be tokenized
  at once with the existing KWS model.
- Single-word wake words have more false accepts than "hey ..." phrases; tune
  `[wakeword].threshold` and `min_consecutive_frames` in config.toml.
