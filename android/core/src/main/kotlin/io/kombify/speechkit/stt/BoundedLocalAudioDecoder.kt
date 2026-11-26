package io.kombify.speechkit.stt

import android.content.Context
import android.media.AudioFormat
import android.media.MediaCodec
import android.media.MediaDataSource
import android.media.MediaExtractor
import android.media.MediaFormat
import android.net.Uri
import android.os.SystemClock
import java.io.ByteArrayOutputStream
import java.nio.ByteOrder
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.ensureActive

/** Bounded container decode to 16 kHz mono signed PCM; no audio is written to disk. */
internal object BoundedLocalAudioDecoder {
    suspend fun decode(context: Context, uri: Uri): ByteArray {
        if (uri.scheme != "content") throw LocalSpeechFailure(LocalSpeechFailureKind.INPUT, "Unsupported audio source")
        try {
            val bytes = context.contentResolver.openInputStream(uri)?.use { stream ->
                val output = ByteArrayOutputStream()
                val buffer = ByteArray(16 * 1024)
                var emptyReads = 0
                while (true) {
                    currentCoroutineContext().ensureActive()
                    val count = stream.read(buffer)
                    if (count < 0) break
                    if (count == 0) {
                        if (++emptyReads >= 100) throw LocalSpeechFailure(LocalSpeechFailureKind.INPUT, "Audio stream did not progress")
                        continue
                    }
                    emptyReads = 0
                    if (output.size() + count > LocalSpeechLimits.MAX_INPUT_BYTES)
                        throw LocalSpeechFailure(LocalSpeechFailureKind.UNAVAILABLE, "Audio exceeds local processing capacity")
                    output.write(buffer, 0, count)
                }
                output.toByteArray()
            } ?: throw LocalSpeechFailure(LocalSpeechFailureKind.INPUT, "Cannot open audio")
            if (bytes.isEmpty()) throw LocalSpeechFailure(LocalSpeechFailureKind.INPUT, "Audio file is empty")
            return try { decodeContainer(bytes) } finally { bytes.fill(0) }
        } catch (e: CancellationException) { throw e }
          catch (e: LocalSpeechFailure) { throw e }
          catch (e: SecurityException) { throw LocalSpeechFailure(LocalSpeechFailureKind.PERMISSION, "Audio access was denied", e) }
          catch (e: Exception) { throw LocalSpeechFailure(LocalSpeechFailureKind.INPUT, "Cannot decode audio", e) }
    }

    internal suspend fun decodeContainer(input: ByteArray): ByteArray {
        if (input.isEmpty())
            throw LocalSpeechFailure(LocalSpeechFailureKind.INPUT, "Encoded audio has an invalid length")
        if (input.size > LocalSpeechLimits.MAX_INPUT_BYTES)
            throw LocalSpeechFailure(LocalSpeechFailureKind.UNAVAILABLE, "Audio exceeds local processing capacity")
        val extractor = MediaExtractor()
        var codec: MediaCodec? = null
        val source = object : MediaDataSource() {
            override fun getSize(): Long = input.size.toLong()
            override fun readAt(position: Long, buffer: ByteArray, offset: Int, size: Int): Int {
                if (size == 0) return 0
                if (position < 0 || position >= input.size) return -1
                val count = minOf(size, input.size - position.toInt())
                input.copyInto(buffer, offset, position.toInt(), position.toInt() + count)
                return count
            }
            override fun close() = Unit
        }
        try {
            extractor.setDataSource(source)
            val track = (0 until extractor.trackCount).firstOrNull {
                extractor.getTrackFormat(it).getString(MediaFormat.KEY_MIME)?.startsWith("audio/") == true
            } ?: throw LocalSpeechFailure(LocalSpeechFailureKind.INPUT, "No audio track")
            extractor.selectTrack(track)
            val format = extractor.getTrackFormat(track)
            if (format.containsKey(MediaFormat.KEY_DURATION) && format.getLong(MediaFormat.KEY_DURATION) > 120_000_000L)
                throw LocalSpeechFailure(LocalSpeechFailureKind.UNAVAILABLE, "Audio exceeds local processing capacity")
            val mime = format.getString(MediaFormat.KEY_MIME) ?: throw LocalSpeechFailure(LocalSpeechFailureKind.INPUT, "Missing audio format")
            val decoder = MediaCodec.createDecoderByType(mime)
            codec = decoder
            decoder.configure(format, null, null, 0)
            decoder.start()
            var rate = format.getInteger(MediaFormat.KEY_SAMPLE_RATE)
            var channels = format.getInteger(MediaFormat.KEY_CHANNEL_COUNT)
            var encoding = AudioFormat.ENCODING_PCM_16BIT
            var inputDone = false
            var outputDone = false
            var inputFrames = 0L
            var outputFrames = 0L
            val output = ByteArrayOutputStream()
            val info = MediaCodec.BufferInfo()
            var lastProgress = SystemClock.elapsedRealtime()
            while (!outputDone) {
                currentCoroutineContext().ensureActive()
                var progress = false
                if (!inputDone) {
                    val index = decoder.dequeueInputBuffer(10_000)
                    if (index >= 0) {
                        val buffer = decoder.getInputBuffer(index) ?: throw LocalSpeechFailure(LocalSpeechFailureKind.INPUT, "Audio decoder has no input buffer")
                        buffer.clear()
                        val size = extractor.readSampleData(buffer, 0)
                        if (size < 0) {
                            decoder.queueInputBuffer(index, 0, 0, 0, MediaCodec.BUFFER_FLAG_END_OF_STREAM)
                            inputDone = true
                        } else {
                            decoder.queueInputBuffer(index, 0, size, extractor.sampleTime.coerceAtLeast(0), 0)
                            extractor.advance()
                        }
                        progress = true
                    }
                }
                when (val index = decoder.dequeueOutputBuffer(info, 10_000)) {
                    MediaCodec.INFO_OUTPUT_FORMAT_CHANGED -> {
                        val actual = decoder.outputFormat
                        rate = actual.getInteger(MediaFormat.KEY_SAMPLE_RATE)
                        channels = actual.getInteger(MediaFormat.KEY_CHANNEL_COUNT)
                        encoding = if (actual.containsKey(MediaFormat.KEY_PCM_ENCODING)) actual.getInteger(MediaFormat.KEY_PCM_ENCODING) else AudioFormat.ENCODING_PCM_16BIT
                        if (inputFrames != 0L) throw LocalSpeechFailure(LocalSpeechFailureKind.INPUT, "Audio format changed during decoding")
                        progress = true
                    }
                    MediaCodec.INFO_TRY_AGAIN_LATER -> Unit
                    else -> if (index >= 0) {
                        val buffer = decoder.getOutputBuffer(index) ?: throw LocalSpeechFailure(LocalSpeechFailureKind.INPUT, "Audio decoder has no output buffer")
                        if (rate !in 8_000..192_000 || channels !in 1..8 ||
                            encoding !in setOf(AudioFormat.ENCODING_PCM_16BIT, AudioFormat.ENCODING_PCM_FLOAT))
                            throw LocalSpeechFailure(LocalSpeechFailureKind.INPUT, "Unsupported decoded audio format")
                        val bytesPerSample = if (encoding == AudioFormat.ENCODING_PCM_FLOAT) 4 else 2
                        val frameBytes = channels * bytesPerSample
                        if (info.size % frameBytes != 0) throw LocalSpeechFailure(LocalSpeechFailureKind.INPUT, "Invalid PCM frame")
                        buffer.position(info.offset)
                        buffer.limit(info.offset + info.size)
                        buffer.order(ByteOrder.LITTLE_ENDIAN)
                        while (buffer.remaining() >= frameBytes) {
                            if (inputFrames % 4096L == 0L) currentCoroutineContext().ensureActive()
                            var sum = 0.0
                            repeat(channels) {
                                val sample = if (encoding == AudioFormat.ENCODING_PCM_FLOAT) {
                                    val value = buffer.float
                                    if (!value.isFinite()) throw LocalSpeechFailure(LocalSpeechFailureKind.INPUT, "Invalid PCM sample")
                                    value.coerceIn(-1f, 1f).toDouble()
                                } else buffer.short.toDouble() / 32768.0
                                sum += sample
                            }
                            val sample = ((sum / channels) * 32767.0).toInt().coerceIn(-32768, 32767)
                            while (outputFrames * rate < (inputFrames + 1) * LocalSpeechLimits.SAMPLE_RATE.toLong()) {
                                if (output.size() + 2 > LocalSpeechLimits.MAX_PCM_BYTES)
                                    throw LocalSpeechFailure(LocalSpeechFailureKind.UNAVAILABLE, "Audio exceeds local processing capacity")
                                output.write(sample and 0xff)
                                output.write((sample shr 8) and 0xff)
                                outputFrames++
                            }
                            inputFrames++
                        }
                        outputDone = info.flags and MediaCodec.BUFFER_FLAG_END_OF_STREAM != 0
                        decoder.releaseOutputBuffer(index, false)
                        progress = true
                    }
                }
                if (progress) lastProgress = SystemClock.elapsedRealtime()
                else if (SystemClock.elapsedRealtime() - lastProgress > 15_000)
                    throw LocalSpeechFailure(LocalSpeechFailureKind.INPUT, "Audio decoder did not progress")
            }
            if (output.size() < 2) throw LocalSpeechFailure(LocalSpeechFailureKind.INPUT, "Audio contains no samples")
            return output.toByteArray()
        } finally {
            runCatching { codec?.stop() }
            runCatching { codec?.release() }
            runCatching { extractor.release() }
            runCatching { source.close() }
        }
    }
}
