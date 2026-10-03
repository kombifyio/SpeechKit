package io.kombify.speechkit.audio

import android.media.AudioAttributes
import android.media.AudioFormat as AndroidAudioFormat
import android.media.AudioTrack
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.delay
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import io.kombify.speechkit.log.VoiceLog
import kotlin.coroutines.CoroutineContext

/** A safe local failure code; no driver exception or captured media is retained. */
class PcmPlaybackException(val code: String) : IllegalStateException(code)

/** Shared S16 mono stream player. [play] suspends until a chunk has reached the driver.
 * [flush] and [release] invalidate waiting writes synchronously, including frames queued
 * behind another [play]. The next new call may open a track after [release].
 */
class PcmStreamPlayer internal constructor(
    private val sampleRateHz: Int,
    private val playback: CoroutineContext,
    private val createTrack: (Int) -> PcmPlaybackTrack,
) {
    constructor(sampleRateHz: Int, playback: CoroutineContext = Dispatchers.IO) :
        this(sampleRateHz, playback, ::createAndroidTrack)

    init { require(sampleRateHz > 0) { "Playback sample rate must be positive" } }

    private val stateLock = Any()
    private val writes = Mutex()
    private var track: PcmPlaybackTrack? = null
    private var generation = 0L

    suspend fun play(pcm: ByteArray) {
        if (pcm.isEmpty()) return
        if (pcm.size % AudioFormat.BYTES_PER_SAMPLE != 0 ||
            pcm.size.toLong() > sampleRateHz.toLong() * AudioFormat.BYTES_PER_SAMPLE * MAX_CHUNK_SECONDS) {
            throw PcmPlaybackException("playback_frame_invalid")
        }
        val issued = synchronized(stateLock) { generation }
        withContext(playback) {
            writes.withLock {
                var offset = 0
                var idleMillis = 0
                while (offset < pcm.size) {
                    currentCoroutineContext().ensureActive()
                    val written = synchronized(stateLock) {
                        if (generation != issued) return@withLock
                        try {
                            val active = track ?: createTrack(sampleRateHz).also { track = it }
                            active.write(pcm, offset, pcm.size - offset)
                        } catch (_: Exception) {
                            discardTrack()
                            throw PcmPlaybackException("playback_device_failed")
                        }
                    }
                    if (written < 0 || written > pcm.size - offset || written % AudioFormat.BYTES_PER_SAMPLE != 0) {
                        synchronized(stateLock) {
                            if (generation != issued) return@withLock
                            discardTrack()
                        }
                        throw PcmPlaybackException("playback_write_failed")
                    }
                    if (written == 0) {
                        idleMillis += RETRY_MILLIS
                        if (idleMillis >= WRITE_TIMEOUT_MILLIS) {
                            synchronized(stateLock) {
                                if (generation != issued) return@withLock
                                discardTrack()
                            }
                            throw PcmPlaybackException("playback_stalled")
                        }
                        delay(RETRY_MILLIS.toLong())
                    } else {
                        offset += written
                        idleMillis = 0
                    }
                }
            }
        }
    }

    /** Stops current speech and permits the next reply to play on a fresh track. */
    fun flush() = synchronized(stateLock) { discardTrack() }

    /** Returns only once the held track has been stopped and released. */
    fun release() = synchronized(stateLock) { discardTrack() }

    private fun discardTrack() {
        generation++
        val active = track
        track = null
        if (active != null) {
            // Nonblocking writes keep this critical section short. Cleanup steps are
            // independent so a failed stop cannot leak the native track.
            runCatching { active.stop() }.onFailure { VoiceLog.w(VoiceLog.AUDIO, "agent track stop failed") }
            runCatching { active.release() }.onFailure { VoiceLog.w(VoiceLog.AUDIO, "agent track release failed") }
        }
    }

    private companion object {
        const val MAX_CHUNK_SECONDS = 2
        const val RETRY_MILLIS = 10
        const val WRITE_TIMEOUT_MILLIS = 2000
    }
}

internal interface PcmPlaybackTrack {
    fun write(pcm: ByteArray, offset: Int, size: Int): Int
    fun stop()
    fun release()
}

private fun createAndroidTrack(sampleRateHz: Int): PcmPlaybackTrack {
    val minBuffer = AudioTrack.getMinBufferSize(sampleRateHz,
        AndroidAudioFormat.CHANNEL_OUT_MONO, AndroidAudioFormat.ENCODING_PCM_16BIT)
    if (minBuffer <= 0) throw PcmPlaybackException("playback_device_failed")
    val floor = sampleRateHz * AudioFormat.BYTES_PER_SAMPLE * 400 / 1000
    val active = AudioTrack.Builder()
        .setAudioAttributes(AudioAttributes.Builder().setUsage(AudioAttributes.USAGE_ASSISTANT)
            .setContentType(AudioAttributes.CONTENT_TYPE_SPEECH).build())
        .setAudioFormat(AndroidAudioFormat.Builder().setEncoding(AndroidAudioFormat.ENCODING_PCM_16BIT)
            .setSampleRate(sampleRateHz).setChannelMask(AndroidAudioFormat.CHANNEL_OUT_MONO).build())
        .setBufferSizeInBytes(maxOf(minBuffer, floor))
        .setTransferMode(AudioTrack.MODE_STREAM).build()
    try {
        if (active.state != AudioTrack.STATE_INITIALIZED) throw PcmPlaybackException("playback_device_failed")
        active.play()
    } catch (_: Exception) {
        runCatching { active.release() }
        throw PcmPlaybackException("playback_device_failed")
    }
    VoiceLog.i(VoiceLog.AUDIO, "agent track opened rate=$sampleRateHz")
    return object : PcmPlaybackTrack {
        override fun write(pcm: ByteArray, offset: Int, size: Int): Int =
            active.write(pcm, offset, size, AudioTrack.WRITE_NON_BLOCKING)
        override fun stop() {
            active.pause()
            active.flush()
            active.stop()
        }
        override fun release() = active.release()
    }
}
