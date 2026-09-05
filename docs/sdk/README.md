# SpeechKit SDK in 10 Minutes

Start with a real local transcript, then use this map to build your own host.
Everything here uses public API under `pkg/speechkit/...`. No SpeechKit server
or cloud key is required. Local inference needs an installed whisper.cpp
runtime and model; WAV input needs no cgo, while built-in microphone capture
needs Windows and cgo.

## 1. Install

```bash
go get github.com/kombifyio/SpeechKit
```

This installs the Go library, **not** whisper.cpp or model files. For the
executable starter, use a SpeechKit checkout containing
[`examples/local-dictation`](../../examples/local-dictation/README.md) and
follow its runtime prerequisites.

The module path is an import path, not a repository location — see
[Repository identities](../../CONTRIBUTING.md#repository-identities) if you
ever see a different one.

## 2. The four things every host wires

```
AudioRecorder  ──PCM──▶  Transcriber  ──Transcript──▶  TranscriptOutput
                              ▲
                     stt.AsTranscriber(provider)
```

| Role | Contract (root package) | You usually use |
|---|---|---|
| Microphone | `speechkit.AudioRecorder` | `audio/capture.Open(...)` on Windows; your own `Start/Stop/SetPCMHandler` elsewhere |
| Speech-to-text | `speechkit.Transcriber` | any `stt.STTProvider` wrapped with `stt.AsTranscriber` |
| Where text goes | `speechkit.TranscriptOutput` | a small type with one `Deliver(ctx, transcript, target)` method |
| Mode runtime | `dictation.Service` (`NewService` / `NewRuntime`) | the same shape as `assist.NewService`, `tts.NewService`, `voiceagent` |

The root package `speechkit` holds only contracts and value types. The engine
behind Dictation lives in `speechkit/pipeline`; the shipped provider/model
data in `speechkit/catalog`. You do not import either for a first host.

## 3. Dictation end to end

The [local starter](../../examples/local-dictation/README.md) contains the
complete runnable program: a file-backed recorder, optional Windows microphone
capture, an owned local model process and cancellation cleanup.

From the SpeechKit checkout, with your runtime and model already installed:

```powershell
$env:PATH = "C:\whisper\bin;$env:PATH"
$env:SPEECHKIT_ALLOW_WHISPER_PATH = "1"
$env:CGO_ENABLED = "0"
go run .\examples\local-dictation -model C:\whisper\models\ggml-base.bin -wav C:\audio\sentence.wav -language de
```

The input must be 16 kHz, mono PCM16 WAV. For a live sentence on Windows,
enable cgo with the documented native toolchain and replace `-wav ...` with
`-record-for 5s`. The microphone is never opened implicitly. Missing
prerequisites and empty transcripts are errors, not successful fake results.

The starter resolves the model path, starts and warms `local.New(...)` using
`StartServer(hostContext)`, and defers `StopServer()`. The constructor alone
does **not** start inference. A separate request context limits transcription
without canceling the model process while it is still needed.

Inside that lifecycle, `dictation.NewService` receives the recorder and
`stt.AsTranscriber(provider)`. `Start` begins the input; `Stop` returns a
`speechkit.DictationRun`. The starter prints its transcript to stdout and
keeps diagnostics on stderr. It configures neither cloud fallback nor
history/clipboard output. To deliver to your own editor, add a
`speechkit.TranscriptOutput`; the service forwards its opaque target unchanged.

[`examples/library`](../../examples/library/main.go) is a separate
**fake-provider composition demo**, not a real recognition path. It can use
synthetic audio when native capture is unavailable.

Errors are sentinels: `dictation.ErrMissingRecorder`, `ErrAudioTooShort`,
`ErrNotRecording`, `capture.ErrBackendUnavailable`, `live.Err*`,
`assist.ErrMissingExecutor` — branch with `errors.Is`, never on message text.

## 4. Pick a provider and a policy

- **Providers** live in `stt/{local,openaicompat,deepgram,google,assemblyai,huggingface,openrouter,vps}`;
  `stt/allproviders.BuildRouter(cfg, enabled)` builds the shipped set from a
  config. `stt.Router{Strategy: ...}` with `SetLocal` / `AddCloud` falls back
  between providers (`StrategyLocalOnly`, `StrategyCloudOnly`,
  `StrategyDynamic`); its `Route(...)` returns the same `*stt.Result` a single
  provider does, and `stt.ToTranscript` maps it to a `speechkit.Transcript`.
- **Per-instance hooks**, not globals: `router.OnProviderSelected(...)`,
  `stt.SecretResolver` on the providers that need one.
- **Policy**: `speechkit.RuntimePolicy{EnabledModes, FixedProfiles, ...}` is
  validated against the catalog when you construct the service, so a
  misconfigured host fails at startup rather than on the first recording.
- **Config file instead of code**: `hostconfig.Load(path)` returns
  `ModeSettings` + `RuntimePolicy` from the same `config.toml` the reference
  desktop app reads.

## 5. Add your own provider

Implement `stt.STTProvider` (three methods), prove it with
`stt/sttcontract.RunContract`, and register a `speechkit.ProviderProfile` via
`catalog.DefaultCatalog().With(...)`. Full walkthrough:
[custom-provider.md](custom-provider.md).

## 6. Go beyond Dictation

| Want | Package | Example |
|---|---|---|
| One-shot voice command → LLM → text/TTS | `assist` | `examples/assist/in-process` |
| Realtime audio-to-audio agent | `voiceagent`, `voiceagent/live` | `examples/voice-agent/in-process` |
| Text-to-speech routing | `tts` | `examples/embed-tts` |
| Wake word → hands-free routing across modes | `companion`, `wakeword` | `examples/embed-companion` |
| Runtime events (wake, skill, TTS, agent) | `speechkit.Runtime` (`Publish` / `Events`) | `examples/embed-event-bus` |
| Talk to a remote SpeechKit Server instead | `client` | `docs/server/README.md` |
| Drive an external coding agent by voice | `agentbridge`, `agentbridge/codex` | `examples/agentbridge-codex` |

Dictation, Assist and Voice Agent are deliberately separate modes: Dictation
never rewrites text, Assist is one-shot, Voice Agent is realtime. Mixing them
is the host's decision, made explicit through `companion.NewHandsFree`.

## 7. What is stable

- Every package under `pkg/speechkit/...` is checked by an API diff in CI;
  breaking removals need the `breaking-api-approved` label and a CHANGELOG
  entry with the old→new mapping.
- Which packages need cgo or an external binary — and how each fails closed
  without it — is one table:
  [Native requirements](../architecture/sdk-surface-boundary.md#native-requirements).
- Module cut, dependency rules and naming vocabulary:
  [SDK surface boundary](../architecture/sdk-surface-boundary.md).
- Full reference: [Framework API](../speechkit-framework-api.md) and
  [pkg.go.dev](https://pkg.go.dev/github.com/kombifyio/SpeechKit/pkg/speechkit).
