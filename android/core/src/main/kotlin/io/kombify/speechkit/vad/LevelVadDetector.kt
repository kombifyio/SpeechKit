package io.kombify.speechkit.vad

/**
 * Level-based voice activity detection.
 *
 * Mirrors: internal/vad/level_vad.go LevelVAD.
 *
 * Unlike [SileroVadDetector] this needs no model file, so it is the endpointer
 * that works on a fresh install. Model weights are never bundled into a
 * release, and the Silero model is only present after an on-demand download —
 * a detector that throws in its constructor cannot be the default path for the
 * system assistant or the keyboard.
 *
 * Hysteresis rather than a single threshold: a frame counts as speech above
 * [speechAbove] and as silence below [silenceBelow], and anything between the
 * two scales linearly. The band exists so a dictation is not cut mid-sentence
 * on a consonant gap while still going quiet rather than tripping on a breath.
 *
 * The default thresholds are the Go production values and their rationale is
 * carried over verbatim: field logs show normal voice on a moderately-gained
 * microphone peaking at only raw 0.010-0.030, with sustained stretches near
 * 0.010. Erring toward "speech" only delays the auto-stop; erring toward
 * "silence" truncates recordings.
 */
class LevelVadDetector(
    silenceBelow: Float = DEFAULT_SILENCE_BELOW,
    speechAbove: Float = DEFAULT_SPEECH_ABOVE,
    hangoverMs: Int = DEFAULT_HANGOVER_MS,
) : VadDetector {

    private val silenceBelow: Float
    private val speechAbove: Float
    private val hangoverMs: Int

    /**
     * How much of the hangover window is left, decremented per processed frame
     * with the frame duration derived from the fixed 16 kHz pipeline rate.
     */
    private var hangoverRemainingMs: Float = 0f

    init {
        // Zero or negative values fall back to the defaults, and so does a
        // nonsensical pair where speech does not sit above silence.
        var silence = if (silenceBelow <= 0f) DEFAULT_SILENCE_BELOW else silenceBelow
        var speech = if (speechAbove <= 0f) DEFAULT_SPEECH_ABOVE else speechAbove
        if (speech <= silence) {
            silence = DEFAULT_SILENCE_BELOW
            speech = DEFAULT_SPEECH_ABOVE
        }
        this.silenceBelow = silence
        this.speechAbove = speech
        this.hangoverMs = if (hangoverMs <= 0) DEFAULT_HANGOVER_MS else hangoverMs
    }

    /**
     * Returns a per-frame speech probability. The contract matches the Silero
     * binding: 0 means silence, 1 means active speech, and the consumer
     * compares against its own threshold.
     *
     * Any frame length is accepted — unlike the Silero binding this is not
     * tied to a fixed tensor shape.
     */
    override fun processFrame(pcmFrame: ShortArray): Float {
        if (pcmFrame.isEmpty()) return 0f

        var sumSq = 0.0
        for (sample in pcmFrame) {
            val x = sample.toDouble()
            sumSq += x * x
        }
        val rms = (Math.sqrt(sumSq / pcmFrame.size) / 32768.0).toFloat()

        val prob = when {
            rms >= speechAbove -> 1f
            rms > silenceBelow -> (rms - silenceBelow) / (speechAbove - silenceBelow)
            else -> 0f
        }

        if (prob >= 0.5f) {
            hangoverRemainingMs = hangoverMs.toFloat()
            return prob
        }

        // Hangover smoothing: hold a weak speech verdict briefly after the
        // last speech frame so consonant gaps and short breaths do not
        // register as silence at the frame level.
        if (hangoverRemainingMs > 0f) {
            hangoverRemainingMs -= pcmFrame.size * MILLIS_PER_SECOND / SAMPLE_RATE
            return if (prob < HANGOVER_FLOOR) HANGOVER_FLOOR else prob
        }
        return prob
    }

    override fun reset() {
        hangoverRemainingMs = 0f
    }

    /** No native resources to release. */
    override fun close() = Unit

    companion object {
        const val DEFAULT_SILENCE_BELOW = 0.004f
        const val DEFAULT_SPEECH_ABOVE = 0.012f
        const val DEFAULT_HANGOVER_MS = 400

        private const val SAMPLE_RATE = 16000f
        private const val MILLIS_PER_SECOND = 1000f

        /** Weak-speech floor held during the hangover window. */
        private const val HANGOVER_FLOOR = 0.6f
    }
}
