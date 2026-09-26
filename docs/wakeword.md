# Wake-Word

SpeechKit's wake-word listener is the activation primitive behind Hands-Free.
Hands-Free is not a fourth SpeechKit mode; it is the layer that combines wake
activation, microphone capture, auto-end policy, and optional voice output for
Dictation, Assist, or Voice Agent.

Wake-word is not a server feature: always-on microphone capture belongs in the
user's host application, desktop client, mobile app, or device agent.

## Public SDK

Use the public packages when embedding wake-word behavior:

- `pkg/speechkit/wakeword` defines detector, pipeline, dispatcher, event, and
  auto-end contracts. It links no keyword-spotting engine; `RegisterEngine`
  is the extension point.
- `pkg/speechkit/wakeword/sherpa` is the sherpa-onnx engine. Importing it
  registers the engine in cgo builds; `sherpa.NewDetector` returns
  `wakeword.ErrCgoRequired` without cgo.
- `pkg/speechkit/wakeword/training` is the opt-in training-data capture and
  uploader around detections (off by default).

The package boundary is intentionally host-neutral. Your host supplies audio
frames, lifecycle, logging, model assets, playback, and UI state; SpeechKit
supplies the contracts that keep wake activation compatible with the three
strict modes.

Hands-free targets:

- `assist`: one-shot Voice Companion requests with optional spoken output.
- `voice_agent`: continuous realtime dialogue.
- `dictation_ui_assisted`: Dictation activation with a visible text target or
  explicit commit surface.

## Typical Host Flow

1. Capture microphone audio in the host application.
2. Feed normalized frames into a wake-word detector.
3. Publish detection events through your app's event bus or directly into a
   SpeechKit mode activation.
4. Start Assist, Voice Agent, or UI-assisted Dictation with the same policy you
   use for hotkey activation.
5. Stop or pause detection while a mode owns the microphone.

## Runtime Split

The desktop host imports only the engine-free `pkg/speechkit/wakeword` root
(catalog, `AutoEndPolicy`, detection contracts) and never links Sherpa.
Sherpa KWS support is isolated in `speechkit-wakeword.exe`, built from
`cmd/speechkit-wakeword`: it is the only binary that imports the
`pkg/speechkit/wakeword/sherpa` engine (a cgo build) and it is bundled next
to its private Sherpa runtime DLLs.

OpenWakeWord stays a separate sidecar, `speechkit-openwakeword.exe`, built from
`cmd/speechkit-openwakeword` with `-tags openwakeword_sidecar`. Keep these
sidecars independent: the host starts them as subprocesses and communicates
over the sidecar protocol instead of importing provider-specific runtime
packages. Both sidecars record opt-in training clips through
`pkg/speechkit/wakeword/training`.

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


## Branded homelab / Home Assistant profile

The [HA/Sonos appliance plan](roadmap/home-assistant-sonos-wakewords.md) uses
`hey_kombify` by default after the user opts into listening. Bare `kombify` stays
lab-only until separately qualified and is then an explicit option; do not
silently change neutral OSS or unrelated desktop defaults. ONNX availability,
microWakeWord training-tool repair and physical German accuracy are different
evidence gates. One utterance matching both phrases must produce one turn.

Sonos supplies output only. The microphone host retains detection/capture and
must respect the remote playback lease; a TTS URL or completed HA action does
not establish that remote speech has ended. Wake activation is not identity or
permission to execute administrative homelab actions.
