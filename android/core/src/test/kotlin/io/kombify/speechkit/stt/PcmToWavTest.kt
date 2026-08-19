package io.kombify.speechkit.stt

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class PcmToWavTest {

    @Test
    fun `wav header is 44 bytes`() {
        val pcm = ByteArray(0)
        val wav = pcmToWav(pcm)
        assertEquals(44, wav.size)
    }

    @Test
    fun `wav starts with RIFF header`() {
        val wav = pcmToWav(ByteArray(100))
        assertEquals('R'.code.toByte(), wav[0])
        assertEquals('I'.code.toByte(), wav[1])
        assertEquals('F'.code.toByte(), wav[2])
        assertEquals('F'.code.toByte(), wav[3])
    }

    @Test
    fun `wav contains WAVE format`() {
        val wav = pcmToWav(ByteArray(100))
        assertEquals('W'.code.toByte(), wav[8])
        assertEquals('A'.code.toByte(), wav[9])
        assertEquals('V'.code.toByte(), wav[10])
        assertEquals('E'.code.toByte(), wav[11])
    }

    @Test
    fun `wav sample rate is 16000`() {
        val wav = pcmToWav(ByteArray(100))
        // Sample rate at offset 24, little-endian
        val sampleRate = (wav[24].toInt() and 0xFF) or
            ((wav[25].toInt() and 0xFF) shl 8) or
            ((wav[26].toInt() and 0xFF) shl 16) or
            ((wav[27].toInt() and 0xFF) shl 24)
        assertEquals(16000, sampleRate)
    }

    @Test
    fun `wav channels is 1 (mono)`() {
        val wav = pcmToWav(ByteArray(100))
        val channels = (wav[22].toInt() and 0xFF) or ((wav[23].toInt() and 0xFF) shl 8)
        assertEquals(1, channels)
    }

    @Test
    fun `wav bits per sample is 16`() {
        val wav = pcmToWav(ByteArray(100))
        val bits = (wav[34].toInt() and 0xFF) or ((wav[35].toInt() and 0xFF) shl 8)
        assertEquals(16, bits)
    }

    @Test
    fun `wav data size matches pcm input`() {
        val pcm = ByteArray(3200) // 100ms at 16kHz 16bit mono
        val wav = pcmToWav(pcm)
        // Data size at offset 40
        val dataSize = (wav[40].toInt() and 0xFF) or
            ((wav[41].toInt() and 0xFF) shl 8) or
            ((wav[42].toInt() and 0xFF) shl 16) or
            ((wav[43].toInt() and 0xFF) shl 24)
        assertEquals(3200, dataSize)
        assertEquals(44 + 3200, wav.size)
    }

    @Test
    fun `wav preserves pcm data after header`() {
        val pcm = byteArrayOf(1, 2, 3, 4, 5)
        val wav = pcmToWav(pcm)
        assertTrue(pcm.contentEquals(wav.sliceArray(44 until wav.size)))
    }
}
