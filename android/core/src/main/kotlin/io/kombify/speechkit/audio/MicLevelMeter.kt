package io.kombify.speechkit.audio

import kotlin.math.exp
import kotlin.math.min
import kotlin.math.sqrt

/**
 * Turns raw PCM capture frames into the smoothed 0..1 level the Voice
 * Assistant orb reacts to.
 *
 * This is the Kotlin counterpart of the Voice UI Kit's browser meter, and the
 * numbers are deliberately identical so a given voice produces the same orb
 * motion on both platforms:
 * - RMS over the frame, scaled by [RMS_GAIN] and clamped to 1
 *   (`clients/typescript/packages/voice-ui/src/adapters/voiceagent.ts`).
 * - Fast-rise / slow-decay envelope follower: attack pulls 55 % of the
 *   remaining distance per update, release decays exponentially at 5/s and
 *   snaps to zero below 0.002
 *   (`clients/typescript/packages/voice-ui/src/core/level.ts`).
 *
 * Not thread-safe: drive it from a single collector.
 */
class MicLevelMeter {
    private var value = 0f

    /**
     * Folds one capture frame into the level.
     *
     * @param frame PCM 16-bit signed little-endian mono, per [AudioFormat].
     * @param elapsedMillis time since the previous frame; clamped to 100 ms so
     *   a stalled capture cannot decay the level in one jump. Pass the real
     *   delta — callers hand in a measured value rather than a wall clock so
     *   this stays testable.
     */
    fun accept(frame: ByteArray, elapsedMillis: Long): Float = advance(rmsOf(frame), elapsedMillis)

    /** Current level without feeding a frame. */
    val level: Float get() = value

    /** Resets between capture sessions. */
    fun reset() {
        value = 0f
    }

    private fun advance(target: Float, elapsedMillis: Long): Float {
        val dt = elapsedMillis.coerceIn(0L, 100L)
        value = if (target > value) {
            value + (target - value) * ATTACK
        } else {
            val decayed = value * exp(-(dt / 1000f) * RELEASE_PER_SECOND)
            if (decayed < SILENCE_FLOOR) 0f else decayed
        }
        return value
    }

    private fun rmsOf(frame: ByteArray): Float {
        val samples = frame.size / 2
        if (samples == 0) return 0f
        var sumSquares = 0.0
        for (i in 0 until samples) {
            val lo = frame[i * 2].toInt() and 0xFF
            val hi = frame[i * 2 + 1].toInt()
            val sample = ((hi shl 8) or lo).toShort().toFloat() / Short.MAX_VALUE
            sumSquares += (sample * sample).toDouble()
        }
        return min(1f, sqrt(sumSquares / samples).toFloat() * RMS_GAIN)
    }

    private companion object {
        const val ATTACK = 0.55f
        const val RELEASE_PER_SECOND = 5f
        const val SILENCE_FLOOR = 0.002f

        /** Speech RMS sits well below 1; the kit lifts it into a usable range. */
        const val RMS_GAIN = 4f
    }
}
