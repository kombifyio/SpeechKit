# Real local dictation

This starter uses **real whisper.cpp**, not a fake transcriber. It composes
`dictation.NewService`, `stt.AsTranscriber`, and the local provider through
public packages only. No SpeechKit server, account, cloud key or Docker is
needed. The whisper subprocess is local inference, not the SpeechKit server.

## Prerequisites

- Go 1.26+ and a checkout of SpeechKit containing this example.
- A trusted [whisper.cpp](https://github.com/ggml-org/whisper.cpp) release or
  build containing `whisper-server` and its runtime libraries.
- A downloaded, compatible multilingual GGML model, for example
  `ggml-base.bin`. See whisper.cpp's
  [model instructions](https://github.com/ggml-org/whisper.cpp/tree/master/models).
  Nothing is downloaded automatically.
- For the file path: a **16 kHz, mono, signed PCM16 WAV**, at most 32 MiB.
  Other encodings and sample rates are rejected, not silently relabeled.
- For the microphone path: Windows and a cgo-capable Go/C toolchain such as
  MinGW-w64. File transcription also works with `CGO_ENABLED=0`; native
  capture on other platforms is not provided by this starter.

The provider searches next to the built executable, then its managed install
directories (`%LOCALAPPDATA%\SpeechKit\bin` on Windows). For a separate runtime
installation, explicitly opt into PATH lookup. Keep the runtime's libraries
beside `whisper-server`. These PowerShell examples assume the runtime is in
`C:\whisper\bin`, the model is in `C:\whisper\models`, and you are in the
SpeechKit checkout:

```powershell
$env:PATH = "C:\whisper\bin;$env:PATH"
$env:SPEECHKIT_ALLOW_WHISPER_PATH = "1"

# No microphone or native capture toolchain is needed.
$env:CGO_ENABLED = "0"
go run .\examples\local-dictation -model C:\whisper\models\ggml-base.bin -wav C:\audio\sentence.wav -language de
```

On Linux/macOS, use the equivalent environment variables and your platform's
whisper runtime with the same flags. The WAV path remains pure Go.

## Record a sentence on Windows

```powershell
$env:PATH = "C:\msys64\mingw64\bin;C:\whisper\bin;$env:PATH"
$env:CGO_ENABLED = "1"
$env:SPEECHKIT_ALLOW_WHISPER_PATH = "1"
go run .\examples\local-dictation -model C:\whisper\models\ggml-base.bin -record-for 5s -language de
```

Wait for **"Microphone active"** before speaking: model loading and warmup
happen first and can take several minutes. Capture is explicit and bounded
to at most one minute in this starter. Ctrl+C discards the in-progress
recording and closes the owned capture and model process. The default input
device is used. The program never writes to the clipboard or types into
another application.

Relative model paths are resolved to absolute paths before provider creation.
Use `-gpu cpu` to disable GPU inference. `-timeout 5m` controls transcription
only; the provider keeps its own startup readiness budget and remains alive
until the host finishes. `-h` lists the options without starting any resources.

## Result and ownership

The recognized text is printed to stdout; progress and provider diagnostics
go to stderr. An empty transcript or a missing model, binary, capture backend
or audio file produces an error, never substitute text.

Every run owns a whisper subprocess on a separately selected loopback port.
It does not connect to an existing SpeechKit instance or stop that instance's
model process. The provider itself handles startup readiness and guarded
subprocess teardown. Cleanup also runs when startup, capture or transcription
fails. A concurrent port-allocation conflict is a startup error, not a reason
to take over another process.

No persistence adapter is configured: this example does not save audio or
history. The WAV input remains yours; microphone PCM stays in memory.
Printing the transcript is intentional output, so do not redirect it into a
shared log. The example does not configure cloud providers or fallback.

For your own host, copy the composition in `main.go`, replace stdout with your
chosen output, and keep provider lifetime separate from request deadlines.
Keep any host-specific retention rules in force when adding storage.
`examples/library` remains the portable **fake-provider** wiring demonstration.
