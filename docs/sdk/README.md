# SpeechKit SDK in 10 Minutes

The shortest path from `go get` to text on screen, and the map you need after
that. Everything here is public API under `pkg/speechkit/...`; nothing needs a
SpeechKit server, a cloud key, or cgo.

## 1. Install

```bash
go get github.com/kombifyio/SpeechKit
```

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

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/dictation"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt/local"
)

type printOutput struct{}

func (printOutput) Deliver(_ context.Context, t speechkit.Transcript, target any) error {
	fmt.Printf("%v <- %s\n", target, t.Text)
	return nil
}

func main() {
	// Local-first: whisper.cpp on this machine, no cloud key. Swap for
	// stt.NewOpenAI(key), stt.NewGroq(key), or any stt.STTProvider you write.
	provider := local.New(8178, "models/ggml-base.bin", "auto")

	svc, err := dictation.NewService(dictation.Options{
		Recorder:    yourRecorder(), // speechkit.AudioRecorder
		Transcriber: stt.AsTranscriber(provider),
		Output:      printOutput{},
		Language:    "de",
		Target:      "editor", // opaque; handed to Deliver unchanged
	})
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	if err := svc.Start(ctx); err != nil {
		log.Fatal(err)
	}
	// ... user speaks ...
	run, err := svc.Stop(ctx)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(run.Transcript.Text, run.AudioDurationMs, "ms")
}
```

`examples/library` is this program with a runnable fake provider and a
microphone fallback; `go run ./examples/library` works on any OS with no
credentials.

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
