package io.kombify.speechkit.audio

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class MicLevelMeterTest {

    /** PCM 16-bit signed little-endian mono frame at a constant amplitude. */
    private fun frameAt(amplitude: Short, samples: Int = 256): ByteArray {
        val bytes = ByteArray(samples * 2)
        for (i in 0 until samples) {
            bytes[i * 2] = (amplitude.toInt() and 0xFF).toByte()
            bytes[i * 2 + 1] = (amplitude.toInt() shr 8).toByte()
        }
        return bytes
    }

    @Test
    fun `silence stays at zero`() {
        val meter = MicLevelMeter()
        assertEquals(0f, meter.accept(frameAt(0), elapsedMillis = 32))
    }

    @Test
    fun `empty frame is not a division by zero`() {
        val meter = MicLevelMeter()
        assertEquals(0f, meter.accept(ByteArray(0), elapsedMillis = 32))
    }

    @Test
    fun `attack pulls 55 percent of the distance per frame`() {
        val meter = MicLevelMeter()
        // Full-scale amplitude: RMS 1.0, gain-scaled and clamped to 1.0.
        val first = meter.accept(frameAt(Short.MAX_VALUE), elapsedMillis = 32)
        assertEquals(0.55f, first, 1e-4f)
        val second = meter.accept(frameAt(Short.MAX_VALUE), elapsedMillis = 32)
        assertEquals(0.55f + 0.45f * 0.55f, second, 1e-4f)
    }

    @Test
    fun `release decays exponentially and floors to zero`() {
        val meter = MicLevelMeter()
        meter.accept(frameAt(Short.MAX_VALUE), elapsedMillis = 32)
        val loud = meter.level

        val afterQuietFrame = meter.accept(frameAt(0), elapsedMillis = 100)
        assertTrue(afterQuietFrame < loud) { "level should decay, was $afterQuietFrame" }
        // exp(-0.1 * 5) = 0.6065
        assertEquals(loud * 0.6065f, afterQuietFrame, 1e-3f)

        repeat(50) { meter.accept(frameAt(0), elapsedMillis = 100) }
        assertEquals(0f, meter.level)
    }

    @Test
    fun `a stalled capture cannot decay more than 100ms worth`() {
        val meter = MicLevelMeter()
        meter.accept(frameAt(Short.MAX_VALUE), elapsedMillis = 32)
        val loud = meter.level

        val stalled = MicLevelMeter()
        stalled.accept(frameAt(Short.MAX_VALUE), elapsedMillis = 32)
        val afterStall = stalled.accept(frameAt(0), elapsedMillis = 5_000)

        val meterAfter100 = meter.accept(frameAt(0), elapsedMillis = 100)
        assertEquals(meterAfter100, afterStall, 1e-6f)
        assertEquals(loud * 0.6065f, afterStall, 1e-3f)
    }

    @Test
    fun `quiet speech still produces a visible level thanks to the gain`() {
        val meter = MicLevelMeter()
        // ~0.1 of full scale: below the orb's useful range without the gain.
        val quiet = (Short.MAX_VALUE * 0.1f).toInt().toShort()
        repeat(10) { meter.accept(frameAt(quiet), elapsedMillis = 32) }
        assertTrue(meter.level > 0.3f) { "expected gain-lifted level, was ${meter.level}" }
    }

    @Test
    fun `reset clears the envelope`() {
        val meter = MicLevelMeter()
        meter.accept(frameAt(Short.MAX_VALUE), elapsedMillis = 32)
        meter.reset()
        assertEquals(0f, meter.level)
    }
}
