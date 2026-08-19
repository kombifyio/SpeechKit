package io.kombify.speechkit.vad

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import kotlin.math.abs

/**
 * Mirrors: internal/vad/level_vad_test.go.
 *
 * The Kotlin detector must behave identically to the Go one — the Device
 * target and the Android targets share the same endpointing contract, so a
 * divergence here would show up as "dictation cuts differently on Android".
 */
class LevelVadDetectorTest {

    @Test
    fun `returns zero for digital silence`() {
        val detector = LevelVadDetector()
        assertEquals(0f, detector.processFrame(constantPcm(0, 512)))
    }

    /**
     * An RMS well above speechAbove must saturate at 1. Guards against
     * someone raising speechAbove far enough that normal voiced audio falls
     * back into the linear ramp.
     */
    @Test
    fun `returns one for loud speech`() {
        val detector = LevelVadDetector()
        // 6000/32768 is about 0.183, far over the saturation point.
        assertEquals(1f, detector.processFrame(constantPcm(6000, 512)))
    }

    /**
     * Pins the linear interpolation between silenceBelow and speechAbove.
     * Collapsing this to a single hard threshold would silently break every
     * consumer that compares against 0.5.
     */
    @Test
    fun `ramps linearly between the thresholds`() {
        val detector = LevelVadDetector()
        // Midpoint of 0.004 and 0.012 is 0.008; the constant-signal amplitude
        // yielding that RMS is round(0.008 * 32768) = 262.
        val prob = detector.processFrame(constantPcm(262, 512))
        assertTrue(prob > 0f && prob < 1f, "midpoint frame prob $prob should be strictly in (0, 1)")
        // Slack absorbs float rounding; the mapping is deliberately linear,
        // not an exact 50% point.
        assertTrue(abs(prob - 0.5f) <= 0.1f, "midpoint frame prob $prob should sit near 0.5")
    }

    /**
     * Empty frames happen when the capture session is torn down mid-batch.
     * A throw here would surface as an audio-handler error instead of a
     * silence reset.
     */
    @Test
    fun `handles an empty frame`() {
        val detector = LevelVadDetector()
        assertEquals(0f, detector.processFrame(ShortArray(0)))
    }

    /**
     * Right after a clear speech frame, low-level frames inside the hangover
     * window still read as speech so consonant gaps and short breaths do not
     * advance the silence timer. Once the window is exhausted the same frame
     * reads as silence again.
     */
    @Test
    fun `hangover holds the speech verdict then expires`() {
        val detector = LevelVadDetector(hangoverMs = 90)

        assertEquals(1f, detector.processFrame(constantPcm(6000, 512)))

        // 512 samples at 16 kHz is 32 ms per frame, so three quiet frames
        // consume the 90 ms window: 90 -> 58 -> 26 -> exhausted.
        val quiet = constantPcm(0, 512)
        repeat(3) { index ->
            val prob = detector.processFrame(quiet)
            assertTrue(prob >= 0.5f, "quiet frame $index inside hangover got $prob, want >= 0.5")
        }
        assertEquals(0f, detector.processFrame(quiet), "hangover should be exhausted")

        detector.reset()
        assertEquals(0f, detector.processFrame(quiet), "reset should clear the hangover")
    }

    /** Nonsensical threshold pairs fall back to the production defaults. */
    @Test
    fun `falls back to defaults on an inverted threshold pair`() {
        val detector = LevelVadDetector(silenceBelow = 0.9f, speechAbove = 0.1f)
        assertEquals(0f, detector.processFrame(constantPcm(0, 512)))
        assertEquals(1f, detector.processFrame(constantPcm(6000, 512)))
    }

    /** Needs no model file — that is the whole reason this detector exists. */
    @Test
    fun `constructs without any model present`() {
        val detector: VadDetector = LevelVadDetector()
        detector.close()
    }

    private fun constantPcm(sample: Int, frames: Int) =
        ShortArray(frames) { sample.toShort() }
}
