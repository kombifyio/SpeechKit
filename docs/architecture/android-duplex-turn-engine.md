# Android Duplex Turn Engine

Decision date: 2026-08-26. Owner: `:core`,
`io.kombify.speechkit.turn.TurnEngine`.

A hands-free spoken dialogue needs four decisions made in the right order:
when the user started speaking, what audio belongs to that turn, when the turn
ended, and whether audio arriving over the agent's own voice is a real
interruption. This document says who makes them and why the consuming surface
is not allowed to.

## The defect this exists to make impossible

The Companion shipped a voice agent that was not usable as a dialogue. Two
symptoms from the tester build:

- after every sentence the surface fell back to "tap to interrupt"; the user
  had to tap to speak again, which is not hands-free
- when he did speak, only the middle of the sentence arrived — the beginning
  and the end were gone

The cause was a half-duplex scheme written in the consuming app: every
microphone chunk was discarded while agent audio played, plus eight more
chunks (~800 ms) afterwards, with no VAD and no look-back buffer. Speech
starting during or just after the agent's audio was thrown away, and nothing
detected that the user had started talking.

That is not a bug in one app. This repository owned the VAD, the capture, the
players and the frame protocol but shipped no finished turn-taking endpoint,
so every surface improvised one. The keyboard improvised push-to-talk; the
assistant improvised a one-shot capture; the Companion improvised a mute
window. The engine is the endpoint that removes the improvisation.

## Ownership

Per [android-sdk-surface-boundary.md](android-sdk-surface-boundary.md) the host
owns the microphone and the loudspeaker. The engine opens no audio device: it
consumes PCM frames and emits decisions. It lives in `:core`, is Apache-2.0,
depends on nothing outside `:core`, and imports no Android API, so a host,
`:ime`, `:assistant` and Companion all consume the same one.

The consumer's whole contract:

```kotlin
val capture = MicAudioCapture(duplex = true)
val engine = TurnEngine(echo = capture.echoControl)

capture.frames().collect { frame ->
    engine.offer(frame).forEach { event ->
        when (event) {
            is TurnEvent.TurnAudio -> agent.sendAudio(event.pcm)
            is TurnEvent.SpeechStarted -> if (event.bargeIn) cutPlayback()
            is TurnEvent.TurnEnded -> agent.endTurn()
            is TurnEvent.InputLevel -> orb.level = event.level
            is TurnEvent.BargeInRejected -> Unit
        }
    }
}
```

plus `engine.notePlaybackFrame(pcm, sampleRateHz)` beside the player and
`engine.noteProviderTurnEnd()` where provider turn signals arrive.

There is no threshold, window, tail or timer in that surface. A consumer
cannot reintroduce the defect while using the API, because the API never asks
it a timing question. `TurnAudio` is the only PCM that leaves the engine —
a consumer that forwards capture frames instead is visibly doing something
the API did not ask for.

## Onset: loudness and the first words

A turn opens only when both hold, continuously, for a confirmation window:

- **loudness** — frame RMS over a gate, with a 150 ms peak hold so a consonant
  closure does not reset a half-confirmed onset
- **speech** — `VadDetector` probability at or above 0.5, the same verdict
  every other consumer in this repository compares against

The confirmation window is 250 ms while the agent is quiet (the value
`VadConfig.minSpeechDurationMs` already carried) and 350 ms against the
agent's own voice. A single loud frame plus its peak hold cannot reach either,
which is the property the tests pin.

`LevelVadDetector` is the default because it needs no model file on a fresh
install. With it, loudness and speech are two readings of the same envelope,
and the sustain requirement does the discriminating. A host that has
downloaded the Silero weights passes `SileroVadDetector` to the constructor
and the two become independent signals; nothing else changes.

## Speech-likeness, against ambient noise

Sustain alone does not separate a fan from a voice: a fan is loud, and it lasts
longer than any window. So a barge-in candidate must also *move*. The engine
measures the RMS envelope in 20 ms sub-windows across the candidate and
requires a relative spread of at least 0.30.

Speech is amplitude-modulated at roughly 3–8 Hz and goes near-silent between
syllables, so it runs far above that line. A 20 ms window at 16 kHz holds 320
samples, so a stationary source's per-window RMS varies by only a few percent;
even drifting real-world noise stays well under 0.15.

This check applies **only during playback**. Idle, a false onset costs a short
empty turn; over the agent it cuts an answer off mid-sentence. Only the
expensive side has to be sure.

## Pre-roll: 600 ms

The engine keeps a 600 ms ring of recent capture and flushes it as the first
`TurnAudio` of the turn. That is what rescues the sentence beginning.

The number is derived, not picked:

| Term | Value | Why |
|---|---|---|
| Confirmation window | 350 ms | worst case (barge-in); this much real speech is spent proving the speech is real |
| Unvoiced run-in | 150 ms | an initial `s`, `f`, `sh` sits under the loudness gate |
| Chunk quantisation | 100 ms | one `AudioFormat.STREAM_CHUNK_BYTES` capture chunk |
| **Total** | **600 ms** | leaves ≥ 250 ms of genuine pre-onset audio in the worst case |

At 16 kHz S16 mono that is 19 200 bytes held per engine.

The pre-roll of a barge-in also carries the tail of whatever the loudspeaker
was playing. That is accepted: the sentence start is worth more than a clean
uplink, and with platform AEC engaged the leakage in it is largely cancelled.

## Endpointing: engine or provider, and the consumer never knows which

`TurnPolicy.Adaptive` is the default. The engine starts endpointing on 700 ms
of trailing silence (`VadConfig.minSilenceDurationMs`) and permanently hands
endpointing to the provider the first time `noteProviderTurnEnd()` is called.

- A cascaded pipeline never sends a provider turn signal and stays on
  client-side endpointing forever.
- A provider with native turn detection — Deepgram Flux, gpt-realtime, Gemini
  Live — sends one on the first turn and takes over. Uplink then becomes
  continuous, because a provider running its own VAD must hear everything.

The consumer forwards the signal unconditionally in both cases and writes no
branch. `TurnPolicy.EngineEndpointed` and `TurnPolicy.ProviderEndpointed` pin
the behaviour explicitly where a host already knows.

A 30 s cap closes a turn that endpointing never closes, matching the
assistant's one-shot capture.

## Duplex without a mute window

There is no mute window and no tail. Microphone frames are processed, kept in
the pre-roll, and eligible to open a turn at every instant, including while the
agent is speaking. What changes during playback is only how strict the onset
test is.

The engine tracks playback from the audio the host hands it:
`notePlaybackFrame(pcm, sampleRateHz)` adds that PCM's duration to a remaining
budget, and each microphone frame drains it by that frame's duration. So the
guard ends when the agent's audio ends — that is a physical boundary, not a
chosen timer, and it is why "I had to tap to speak again" cannot come back. An
accepted barge-in zeroes the budget, because the host is required to cut
playback on `SpeechStarted(bargeIn = true)`.

The rate is a required argument, and the one place this API asks the consumer
for a number. It has to: the budget is a duration, and bytes only become a
duration at a rate. Playback is the single leg of the pipeline where the rate
varies — the Voice Agent downlink is 24 kHz S16 mono
(`internal/audio/stream_player.go`, `cmd/speechkit/voice_agent_echo_guard.go`)
while capture is fixed at 16 kHz. Measuring downlink bytes at the capture rate
overstates the agent's audible time by half, which arms the barge-in gate over
~2 s of silence after a 4 s answer: the original defect, smaller. So
`frameDurationMillis()` stays capture-only with no rate-taking overload — an
overload would put 16 kHz arithmetic one zero-argument call away from playback
bytes — and the variable-rate arithmetic lives privately in the engine, behind
a call that cannot omit the rate.

Echo itself is the platform's job, not ours. `MicAudioCapture(duplex = true)`
switches the recorder to `VOICE_COMMUNICATION` — the source Android routes
through its own echo-cancelling chain — and attaches `AcousticEchoCanceler`
where the device offers it. The reference signal and the loop delay live in
the audio HAL; an app cannot do better in Kotlin. The default stays
`VOICE_RECOGNITION` for every other mode, because that source deliberately
carries no AGC or noise suppression and those cost transcription accuracy.

`MicAudioCapture.echoControl` reports the result and is what the engine should
be constructed with, so the host declares a capability rather than choosing a
number. It selects the residual guard:

| `EchoControl` | Barge-in loudness gate | Reasoning |
|---|---|---|
| `PlatformAec` | 0.012 (`LevelVadDetector.DEFAULT_SPEECH_ABOVE`) | the canceller leaves a residual far below speech |
| `None` | 0.030 | leakage occupies the ordinary voice band (raw 0.010–0.030 on a moderately gained mic); the user's own voice, far closer to the mic, must clear the top of it |

The idle gate in both cases is the midpoint of the level VAD's hysteresis
band — exactly where that detector starts calling a frame speech.

`0.030` is the one number in the engine that is a judgement rather than a
derivation. It needs device evidence to tune, and until then `EchoControl.None`
is an explicitly degraded mode: barge-in over an uncancelled loudspeaker is a
level contest.

## Speech barge-in is off by default

`SpeechBargeIn.Disabled` is the constructor default, and with it no microphone
audio opens a turn while the agent is audible. The refusal is reported as
`BargeInRejection.Disabled` so a device log still shows how often someone tried.

The reason is that everything below this line — the loudness gate, the
speech-likeness test, the playback-reference comparison — exists to answer one
question that a level cannot: is this a person, or the agent's own voice coming
back through the loudspeaker. On a device that does not cancel the loudspeaker
out of the capture signal, the two are the same signal. It is loud, it is
sustained, and it is amplitude-modulated like a voice because it *is* a voice.

Getting that wrong in the permissive direction does not cost one interruption,
it costs the feature. A consumer reported it from a shipped build: the agent
heard its own sentence, took it for a barge-in, cut itself off, answered the
fragment, and heard that too — a self-sustaining loop of broken sentences with
no way in for the person holding the phone. The envelope comparison that was
supposed to catch that fails open by design (see `echoLike`), and on hardware it
did not catch it.

Inverting it to fail closed was considered and rejected. It trades the loop for
an agent nobody can interrupt, and the threshold that separates a person over
the echo from the echo alone cannot be chosen without measuring what a
loudspeaker leaks into a handset microphone at speakerphone volume. A number
that cannot be calibrated without hardware is not a fix. So the feature is
absent rather than tuned, and the machinery stays behind the switch so that
re-enabling is one argument rather than a rewrite.

What is unaffected: onset detection, the 600 ms pre-roll, endpointing, the
provider handover, and the explicit interrupt — a host that flushes its player
and calls `notePlaybackStopped()` disarms the guard immediately, so the user's
next words are an ordinary turn with their pre-roll intact. A deliberate tap
carries no doubt about who is speaking, and it was never the problem.

### What re-enabling requires

Not a decision, a measurement. On a device session:

1. The host has put the session into a voice-communication route —
   `MODE_IN_COMMUNICATION`, playback on `USAGE_VOICE_COMMUNICATION`, and an
   output the person can actually hear. An `AcousticEchoCanceler` attached to a
   capture session whose playback runs as `USAGE_MEDIA` reports itself enabled
   and subtracts nothing, which is the arrangement the reporting consumer had.
2. `MicAudioCapture.echoControl` reports `PlatformAec` *and* the microphone
   level during an answer sits at the residual rather than at speech level —
   the second half is what proves the first is more than a label.
3. With that established, `ECHO_CORRELATION_MIN` and
   `UNAIDED_BARGE_IN_RMS_GATE` are tuned from a recorded session rather than
   from first principles, and `SpeechBargeIn.Enabled` becomes defensible.

## What synthetic tests can and cannot prove

`core/src/test/.../turn/TurnEngineTest.kt` drives the public API with
synthetic PCM: silence, speech-like modulated frames, steady noise at speech
level, a single loud frame, and speech starting mid-playback. It proves the
decision logic — onset needs sustain, the pre-roll precedes the onset in the
captured turn, speech over the agent takes no turn in the default
configuration and does with `SpeechBargeIn.Enabled`, steady noise never does,
endpointing waits for trailing silence and survives gaps inside a sentence, a
provider signal takes endpointing over, and the playback guard runs for the
downlink audio's real duration at 24 kHz rather than a rate-confused multiple
of it.

It proves nothing about acoustics. Real echo, real room noise, real
loudspeaker leakage at real volumes, and whether `AcousticEchoCanceler`
actually attaches on a given handset are device evidence. The bead closes on a
recorded multi-turn dialogue on hardware with no screen interaction, not on
this suite.

## Change log

- 2026-08-26: Initial engine — onset confirmation, 600 ms pre-roll, adaptive
  endpointing, playback-budget echo guard, duplex capture with platform AEC.
- 2026-08-26: `notePlaybackFrame` takes the playback sample rate as a required
  argument. It had measured the 24 kHz downlink at the 16 kHz capture rate and
  held the guard 1.5× too long. Found by the first consumer during migration.
- 2026-08-26: `SpeechBargeIn`, defaulting to `Disabled`. Speech over the agent
  no longer opens a turn; the explicit interrupt does. See the section above
  for what re-enabling requires. Filed against the same consumer report that
  produced the previous entry.
