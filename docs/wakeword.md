# Wake-Word

SpeechKit's wake-word listener is a client-side framework module. It is not a
server feature: always-on microphone capture belongs in the user's host
application, desktop client, mobile app, or device agent.

## Public SDK

Use the public packages when embedding wake-word behavior:

- `pkg/speechkit/wakeword` defines detector, dispatcher, event, and auto-end
  contracts.
- `pkg/speechkit/wakeword/sherpa` exposes the sherpa-onnx adapter surface.

The package boundary is intentionally host-neutral. Your host supplies audio
frames, lifecycle, logging, model assets, and UI state; SpeechKit supplies the
contracts that keep wake-word activation compatible with Dictation, Assist, and
Voice Agent flows.

## Typical Host Flow

1. Capture microphone audio in the host application.
2. Feed normalized frames into a wake-word detector.
3. Publish detection events through your app's event bus or directly into a
   SpeechKit mode activation.
4. Start Dictation, Assist, or Voice Agent with the same policy you use for
   hotkey activation.
5. Stop or pause detection while a mode owns the microphone.

## Agent Guidance

Agents should import only public packages from `pkg/speechkit/...`. Do not
import Go `internal` packages from another module; they are implementation
packages for the source tree's own binaries.

For generated integrations, start with:

```bash
go test ./pkg/speechkit/wakeword/... ./pkg/speechkit/voiceagent/...
go run ./cmd/speechkit-mcp --mode=docs,test
```

## Privacy Contract

Wake-word is opt-in. If a host enables it, the microphone is continuously read
until the host disables listening. Hosts should show visible listening state,
avoid recording wake-word audio unless the user explicitly opts in, and keep
network use explicit in their own privacy policy.
