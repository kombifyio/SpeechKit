# Wake-Word — SpeechKit Client Module

SpeechKit's wake-word listener is a **framework module that clients embed**.
It is never a server-side concern. The Server-Target (`internal/server`)
exposes Dictation / Assist / Voice Agent over HTTP/WS but has no wake-word
surface — running an always-on microphone in a server process makes no
architectural sense and is intentionally out of scope.

The framework ships:

- A platform-neutral Go kernel under `internal/wakeword/` (`Pipeline`,
  `Detector`, `Dispatcher`, `DetectionEvent`).
- Per-client adapters that wire the kernel into a particular target's
  audio source, hotkey bus and status surface.

Today's reference adapter is the Windows desktop in
`cmd/speechkit/desktop_wakeword.go`. Other client targets that consume the
same kernel are roadmap items:

| Client target | Adapter location | Status |
|---|---|---|
| Windows desktop (Wails) | `cmd/speechkit/desktop_wakeword.go` + sibling `speechkit-wakeword.exe` from `cmd/speechkit-wakeword/` | Working as of v0.34.6 |
| Local-Target Go CLI | `cmd/speechkit-cli/` (planned) | Not started |
| Android (gomobile) | `mobile/android/wakeword.aar` (planned) | Not started |
| iOS (gomobile) | `mobile/ios/Wakeword.framework` (planned) | Not started |
| Web | WASM build of `internal/wakeword` (planned) | Not started |

Each adapter brings its own audio source and event sink; all share the
same wake-word trigger contract.

## Detection backends

The Windows desktop ships three selectable detection paths. The default
production backend loads the [sherpa-onnx](https://github.com/k2-fsa/sherpa-onnx)
Zipformer keyword-spotting model via
[`github.com/k2-fsa/sherpa-onnx-go`](https://github.com/k2-fsa/sherpa-onnx-go).
sherpa-onnx ships its own MinGW-compiled ONNX Runtime, which matches the
SpeechKit MinGW build toolchain and avoids the MSVC vs MinGW ABI clash that
gated the previous openWakeWord-based implementation. The KWS model is
~13 MB unpacked, Apache-2.0, supports open-vocabulary keyword spotting
(keywords are declared as BPE-tokenised text, no per-phrase retraining).

As of v0.36, Settings exposes a temporary backend selector so we can test
the available paths explicitly:

| `backend` | UI label | Runtime status |
|---|---|---|
| `sherpa_kws` | Sherpa KWS sidecar | Implemented and bundled. This is the current production path. |
| `livekit_openwakeword` | LiveKit/openWakeWord | Implemented via `speechkit-openwakeword.exe`, using bundled openWakeWord-compatible ONNX frontend, embedding, and phrase models. |
| `stt_phrase` | STT phrase match | Implemented as a continuous local STT listener that confirms the configured phrase through the current dictation transcriber. |

Selecting a backend does not silently fall back to another detector. If a
selected backend is missing assets or credentials, the desktop status reports
that concrete problem so testing results stay attributable to the backend that
was actually selected.

The build pipeline downloads everything reproducibly:

- `scripts/prepare-sherpa-runtime.ps1` — copies the matching native libs
  (`onnxruntime.dll`, `sherpa-onnx-c-api.dll`, `sherpa-onnx-cxx-api.dll`)
  out of the Go module cache and into the bundle.
- `scripts/prepare-wakeword-model.ps1` — fetches the pinned
  `sherpa-onnx-kws-zipformer-gigaspeech-3.3M-2024-01-01` release with
  SHA256 verification, unpacks `encoder/decoder/joiner/tokens/keywords`
  into `dist/windows/SpeechKit/wakeword-kws/`, and stages the bundled
  openWakeWord ONNX artifacts into `dist/windows/SpeechKit/models/wakeword/`.

## Privacy trade-off

When the wake-word listener is active, your microphone is always on while
SpeechKit is running. Audio is read continuously and fed to the keyword
spotter. It is never recorded to disk and never sent to a network. The
tray indicator reflects the listening state so you can see at a glance
whether the mic is open.

Wake-word is opt-in and disabled by default. The hotkey paths
(`ctrl+win` / `win+alt` / `ctrl+shift`) work without ever turning the
listener on. This matches the dictation-app industry norm
([Wispr Flow](https://wisprflow.ai/) treats "Hey Flow" as a Pro-tier
opt-in; [Superwhisper](https://superwhisper.com/) and
[VoiceInk](https://github.com/Beingpax/VoiceInk) are hotkey-only).

## Available phrases (Windows reference adapter)

The bundled `keywords.txt` ships the upstream sherpa-onnx gigaspeech
keyword list. The SpeechKit catalog maps the following stable IDs (saved
in `[wakeword].phrase_id`) to display labels for the settings UI:

| `phrase_id` | Display name | Notes |
|---|---|---|
| `hey_quby` | Hey Quby (Cubi / Kubi) | SpeechKit brand default. |
| `hey_computer` | Hey Computer | Star Trek classic; 4 syllables, very distinct phonemes. |
| `hey_jarvis` | Hey Jarvis | Marvel-popular; strong J/RV/S consonants. |
| `hey_mira` | Hey Mira | Short brand-style alternative; equally natural in DE+EN. |
| `hey_kombify` | Hey Kombify | Organisation brand; 4 syllables, three distinct consonant onsets. |

Custom phrases work by editing the bundled `keywords.txt` directly — the
file format is one BPE-tokenised line per phrase with an optional
`:boost-score` and trailing `@display-label`. The
`sherpa-onnx-cli text2token` utility from the upstream toolkit converts
plain text to the required BPE form. A future iteration of the framework
ships an in-app keyword editor that hides the BPE step.

## Setup (Windows reference adapter)

1. Open Settings → Modes.
2. Find the "Wake-word" panel.
3. Toggle **Enable wake-word**.
4. Pick the detection backend. Use `sherpa_kws` for the bundled runtime.
5. Pick a wake phrase from the dropdown.
6. Pick your default mode (the mode that starts when the phrase fires).
7. Optionally adjust the threshold (lower = more sensitive). Empty falls
   back to the sherpa-onnx default (~0.25).
8. Click Apply. The status feed shows `Wake-word ready: "<phrase>" → <mode>`
   when the listener is running.

## Auto-end policy (framework contract)

Wake-word-triggered Voice Agent sessions need a different termination
contract than hotkey-triggered ones: the synthesized event is a single
KeyDown with no KeyUp counterpart, so neither Hold-to-Talk nor Toggle
semantics can end the session on their own. The framework therefore
exposes `internal/wakeword.AutoEndPolicy` — a provider-agnostic watcher
every client target wires the same way.

| Trigger | Default | Configurable via |
|---|---|---|
| Silence cutoff (no user audio activity) | 10 s | `[wakeword.auto_end] silence_cutoff_sec` |
| Exit phrase (case-insensitive substring match against user transcript) | `["danke","tschuess","tschüss","ende","stop","thanks","thank you","bye","goodbye"]` | `[wakeword.auto_end] exit_phrases` |

There is intentionally **no hard-cap** on session duration. Voice Agent
is designed for multi-hour dialogues; a forced limit would break the
manual hotkey path that shares the same session machinery.

### Public API

```go
// AutoEndConfig — TOML/runtime surface.
type AutoEndConfig struct {
    SilenceCutoff time.Duration
    ExitPhrases   []string
}

// DefaultAutoEndConfig — framework baseline; clients should start from
// this and override fields rather than hard-coding the constants.
func DefaultAutoEndConfig() AutoEndConfig

// AutoEndPolicy — one watcher per detection event. Provider-agnostic.
type AutoEndPolicy struct { /* ... */ }

func NewAutoEndPolicy(cfg AutoEndConfig, logger *slog.Logger) *AutoEndPolicy
func (p *AutoEndPolicy) Start()
func (p *AutoEndPolicy) NotifyActivity()
func (p *AutoEndPolicy) NotifyTranscript(text string)
func (p *AutoEndPolicy) EndSignal() <-chan EndReason
func (p *AutoEndPolicy) Close()

type EndReason string
const (
    EndReasonSilence    EndReason = "silence"
    EndReasonExitPhrase EndReason = "exit_phrase"
)
```

### Client adapter wiring (Windows reference)

```
sidecar detection event
  -> handleSidecarEvent
        policy := wakeword.NewAutoEndPolicy(cfg.Wakeword.AutoEnd, logger)
        state.setWakewordSessionPolicy(policy)
        hkManager.Submit(hotkey.Event{
            Type:    hotkey.EventKeyDown,
            Binding: mode,
            Source:  hotkey.EventSourceWakeword,  // <-- distinguishes from real hotkey
        })

  -> routeVoiceAgentHotkey
        if evt.Source == EventSourceWakeword:
            activateVoiceAgentWakewordSession(ctx)
        else:
            existing hotkey path (unchanged)

  -> activateVoiceAgentWakewordSession
        policy.Start()
        go watchWakewordAutoEnd(ctx, policy):
            select <-policy.EndSignal():
                deactivateVoiceAgentWithReason(ctx, false, "wakeword_"+reason)
        startVoiceAgentSession(...)  // PCM handler calls policy.NotifyActivity per frame

  -> voice_agent_session.OnInputTranscript callback
        policy.NotifyTranscript(text)   // drives exit-phrase matching

  -> session ends (any path)
        OnSessionEnd: state.clearWakewordSessionPolicy().Close()
```

### Provider compatibility

| Provider | Silence cutoff | Exit phrase |
|---|---|---|
| Gemini Live (realtime audio) | ✓ (every PCM frame) | ✓ when `LivePolicies.EnableInputAudioTranscription=true` |
| OpenAI Realtime | ✓ (every PCM frame) | ✓ when input transcription is enabled on the session |
| Local Cascaded (Session-wrapped) | ✓ (every PCM frame) | ✓ (STT result feeds `NotifyTranscript` directly) |

Silence cutoff is always available because it rides on PCM-frame
delivery, which every provider sees identically. Exit phrase depends on
provider transcript capability — when transcripts are off the silence
timer is the sole termination mechanism.

### Audit

`voiceagent.session.end` audit events emitted by wake-word-triggered
sessions carry two new `terminated_by` values in addition to the
existing `user`/`error`/`idle`:

- `wakeword_silence` — `EndReasonSilence` fired
- `wakeword_exit_phrase` — `EndReasonExitPhrase` fired

See [docs/compliance/audit-event-catalog.md](compliance/audit-event-catalog.md)
for the full catalog and
[docs/superpowers/specs/2026-05-20-wakeword-auto-end-design.md](superpowers/specs/2026-05-20-wakeword-auto-end-design.md)
for the design rationale.

## Sidecar architecture

The Windows reference adapter spawns `speechkit-wakeword.exe` as a sibling
process when wake-word is enabled. The desktop app and the sidecar
communicate via a one-JSON-event-per-line protocol on stdout/stdin:

- **`{"type":"ready", ...}`** — sidecar reports successful startup with
  backend and active phrase.
- **`{"type":"detection","keyword":"hey_siri","phrase":"Hey Siri","mode":"voice_agent", ...}`** — keyword
  hit; the adapter forwards it into the desktop's hotkey-event bus, which
  starts the configured mode the same way a hotkey press would.
- **`{"type":"heartbeat","bytesIn":...,"decodesIn":...,"uptimeSec":...}`** —
  every 30 s, so silent audio failures surface in the log.
- **`{"type":"error","msg":"..."}` / `{"type":"log",...}`** — surfaced into
  the desktop status feed.

The sidecar exits cleanly on stdin `{"type":"shutdown"}` or process
context cancellation. A native crash (exit code 2) does **not** take the
host process down: the main app logs the exit and surfaces a "Wake-word
offline" status. Auto-restart with backoff is roadmap work — for now the
user re-toggles wake-word in Settings to respawn.

The same `cmd/speechkit-wakeword` source compiles cleanly on Linux and
macOS via the upstream sherpa-onnx-go-{linux,macos} modules, so the
sidecar is the natural shipping vehicle for those client targets too.
Mobile (gomobile) and web (WASM) targets will use a different vehicle but
import the same `internal/wakeword` kernel.
