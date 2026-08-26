package io.kombify.speechkit.audio

/**
 * What a capture chain already did about acoustic echo.
 *
 * A capability declaration, not a tuning knob: it says what happened to the
 * microphone signal before anyone else saw it, and
 * `io.kombify.speechkit.turn.TurnEngine` picks its own barge-in thresholds
 * from that. Hosts read it off the capture ([MicAudioCapture.echoControl])
 * rather than choosing a value.
 *
 * It is only knowable once capture is open — on Android the canceller attaches
 * to an `AudioRecord` session — so it is read while frames flow and forwarded
 * with `TurnEngine.noteEchoControl`, never sampled beforehand.
 */
enum class EchoControl {
    /**
     * The platform's `AcousticEchoCanceler` is attached to the capture session
     * and the recorder runs on `VOICE_COMMUNICATION`, so loudspeaker leakage
     * is largely removed before anything downstream sees it.
     */
    PlatformAec,

    /**
     * No echo cancellation. The turn engine still never mutes the microphone,
     * but it demands a considerably louder and longer onset before it will cut
     * the agent off, because the loudspeaker is in the signal.
     */
    None,
}
