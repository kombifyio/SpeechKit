package io.kombify.speechkit.net

import java.io.ByteArrayOutputStream
import java.nio.ByteBuffer
import java.nio.ByteOrder

/** Wraps raw PCM S16LE in a minimal RIFF/WAVE container for the batch endpoint. */
object WavEncoder {

    private const val HEADER_SIZE = 44
    private const val BITS_PER_SAMPLE = 16

    fun pcm16ToWav(pcm: ByteArray, sampleRateHz: Int, channels: Int = 1): ByteArray {
        val byteRate = sampleRateHz * channels * BITS_PER_SAMPLE / 8
        val blockAlign = channels * BITS_PER_SAMPLE / 8
        val header = ByteBuffer.allocate(HEADER_SIZE).order(ByteOrder.LITTLE_ENDIAN)
            .put("RIFF".toByteArray(Charsets.US_ASCII))
            .putInt(36 + pcm.size)
            .put("WAVE".toByteArray(Charsets.US_ASCII))
            .put("fmt ".toByteArray(Charsets.US_ASCII))
            .putInt(16) // PCM fmt chunk size
            .putShort(1) // audio format: PCM
            .putShort(channels.toShort())
            .putInt(sampleRateHz)
            .putInt(byteRate)
            .putShort(blockAlign.toShort())
            .putShort(BITS_PER_SAMPLE.toShort())
            .put("data".toByteArray(Charsets.US_ASCII))
            .putInt(pcm.size)

        val out = ByteArrayOutputStream(HEADER_SIZE + pcm.size)
        out.write(header.array())
        out.write(pcm)
        return out.toByteArray()
    }
}
