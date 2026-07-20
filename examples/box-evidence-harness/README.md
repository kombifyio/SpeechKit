# box-evidence-harness

A headless **fake box**. It drives the exact Voice-Agent WebSocket path the
kombify-Box firmware uses and reports the two runtime facts the firmware
hardcodes against — so the ESP32 firmware
([`kombify-box`](../../../kombify-box), milestones M1.3/M1.4) never bakes in a
stale server assumption.

This is the SpeechKit side of the box roadmap's **M1.0 Server-Evidence-Harness**
([`kombify-box/docs/roadmap-standalone-voice.md`](../../../kombify-box/docs/roadmap-standalone-voice.md)).

## What it proves

1. **Empty-Origin dial is accepted.** The ESP32 sends no `Origin` header. The
   server default-denies that, so it must run `SPEECHKIT_ALLOW_EMPTY_ORIGIN=1`.
   The harness dials with no Origin (via `pkg/speechkit/client`, which sends only
   `Authorization` + the ticket subprotocol) and fails loudly if rejected.
2. **Downlink is 24 kHz.** The firmware upsamples the response 24→48 kHz. The
   harness cross-checks the response PCM byte count against the output
   transcript's word count (~2.5 synthesized words/sec); the rate whose implied
   duration best matches is the server's true downlink rate. Drift from 24 kHz
   fails the run.

It also confirms the full turn: Bearer session create → ticket → WS dial →
`start{provider:"deepgram"}` → `state:listening` → streamed 16 kHz utterance →
`input_transcript` → response audio.

## Run

From the repo root (so the default fixture path resolves), against a server that
has a Deepgram key and `SPEECHKIT_ALLOW_EMPTY_ORIGIN=1`:

```sh
export SPEECHKIT_SERVER_URL=https://speechkit.kombify.io
export SPEECHKIT_TOKEN=<service-bearer>
go run ./examples/box-evidence-harness -verbose
```

Flags: `-server`, `-token`, `-provider` (default `deepgram`), `-audio` (default
`testdata/e2e/voiceagent/turn1.wav`, 16 kHz S16LE mono), `-timeout`, `-verbose`.

## Exit codes / result line

The last stdout line is machine-readable: `RESULT: passed`, `RESULT: fail`, or
`RESULT: blocked_by_auth`.

| Code | Meaning |
|------|---------|
| `0`  | Evidence PASS. |
| `2`  | `blocked_by_auth` — no token, or session create returned 401/403. CI treats this as skipped. |
| `1`  | Hard failure — dial rejected, no `listening`, empty transcript, no audio, or downlink-rate drift. |

> Note: `go run` collapses a non-zero program exit to `1`. For a true exit code,
> `go build -o box-evidence-harness ./examples/box-evidence-harness` and run the
> binary, or parse the `RESULT:` line. The CI gate does the former.
