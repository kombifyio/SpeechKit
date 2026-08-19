package io.kombify.speechkit.net

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Test
import java.nio.ByteBuffer
import java.nio.ByteOrder

class WavEncoderTest {

    @Test
    fun `wav header carries riff layout and pcm metadata`() {
        val pcm = ByteArray(3200) { (it % 251).toByte() }
        val wav = WavEncoder.pcm16ToWav(pcm, 16_000)

        assertEquals(44 + pcm.size, wav.size)
        assertEquals("RIFF", String(wav, 0, 4, Charsets.US_ASCII))
        assertEquals("WAVE", String(wav, 8, 4, Charsets.US_ASCII))
        assertEquals("fmt ", String(wav, 12, 4, Charsets.US_ASCII))
        assertEquals("data", String(wav, 36, 4, Charsets.US_ASCII))

        val buf = ByteBuffer.wrap(wav).order(ByteOrder.LITTLE_ENDIAN)
        assertEquals(36 + pcm.size, buf.getInt(4)) // RIFF chunk size
        assertEquals(1, buf.getShort(20).toInt()) // PCM
        assertEquals(1, buf.getShort(22).toInt()) // mono
        assertEquals(16_000, buf.getInt(24)) // sample rate
        assertEquals(32_000, buf.getInt(28)) // byte rate
        assertEquals(2, buf.getShort(32).toInt()) // block align
        assertEquals(16, buf.getShort(34).toInt()) // bits per sample
        assertEquals(pcm.size, buf.getInt(40)) // data size

        // payload passthrough
        assertEquals(pcm[0], wav[44])
        assertEquals(pcm[pcm.size - 1], wav[wav.size - 1])
    }
}
