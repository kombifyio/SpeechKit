package io.kombify.speechkit.audio

import android.media.AudioAttributes
import android.media.AudioFormat as AndroidAudioFormat
import android.media.AudioTrack
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import java.util.concurrent.atomic.AtomicInteger
import kotlin.coroutines.CoroutineContext
import io.kombify.speechkit.log.VoiceLog

/**
 * Streams agent PCM to the speaker. Shared by the keyboard panel, the Voice
 * IME, and the in-app Voice Agent test surface so those hosts do not each
 * own an AudioTrack.
 *
 * The track is created lazily and released when the conversation ends. [play]
 * is `suspend` and runs on [playback]: `AudioTrack.write` in `MODE_STREAM`
 * blocks, and Compose hosts call this from the main thread.
 *
 * Every touch of the track goes through one [Mutex]. A frame remembers how
 * many [release] calls had happened when it was handed over, and a frame that
 * arrives across a release boundary is dropped instead of resurrecting the
 * track.
 *
 * @param sampleRateHz the rate of the PCM this player will be given, in Hz.
 *   No default on purpose. Playback is the one variable rate in this pipeline
 *   — the Voice Agent downlink is 24 kHz S16 mono (`:net`'s
 *   `VoiceAgentAudio.SERVER_SAMPLE_RATE`; `:core` cannot name it, the
 *   dependency runs the other way) while capture is 16 kHz
 *   ([AudioFormat.SAMPLE_RATE]) — and a default is exactly the omission that goes
 *   unnoticed: an `AudioTrack` opened at 16 kHz accepts 24 kHz bytes without
 *   complaint and simply reads them out too slowly, so the agent speaks at
 *   two-thirds speed roughly a fifth low. Nothing throws and no log line
 *   fires; the only symptom is the sound. Every host here knows which stream
 *   it holds, so it says so.
 * @param playback the dispatcher `AudioTrack.write` blocks on.
 */
class PcmStreamPlayer(
    private val sampleRateHz: Int,
    private val playback: CoroutineContext = Dispatchers.IO,
) {
    init {
        require(sampleRateHz > 0) { "playback sample rate must be positive, was $sampleRateHz" }
    }

    private val scope = CoroutineScope(SupervisorJob() + playback)
    private val trackLock = Mutex()
    private var track: AudioTrack? = null
    private val releases = AtomicInteger()

    suspend fun play(pcm: ByteArray) {
        if (pcm.isEmpty()) return
        val issued = releases.get()
        withContext(playback) {
            trackLock.withLock {
                if (releases.get() != issued) return@withLock
                val active = track ?: create().also { track = it }
                runCatching { active.write(pcm, 0, pcm.size) }
                    .onFailure { VoiceLog.w(VoiceLog.AUDIO, "agent playback failed", it) }
            }
        }
    }

    /** Drops buffered speech. Barge-in cuts the agent mid-sentence. */
    fun flush() {
        scope.launch {
            trackLock.withLock {
                val active = track ?: return@withLock
                runCatching {
                    active.pause()
                    active.flush()
                    active.play()
                }.onFailure { VoiceLog.w(VoiceLog.AUDIO, "agent flush failed", it) }
            }
        }
    }

    fun release() {
        releases.incrementAndGet()
        scope.launch {
            trackLock.withLock {
                runCatching {
                    track?.stop()
                    track?.release()
                }
                track = null
            }
        }
    }

    private fun create(): AudioTrack {
        val minBuffer = AudioTrack.getMinBufferSize(
            sampleRateHz,
            AndroidAudioFormat.CHANNEL_OUT_MONO,
            AndroidAudioFormat.ENCODING_PCM_16BIT,
        )
        // A duration, not a byte count: the same 400 ms of slack whatever the
        // stream's rate. Derived from AudioFormat.STREAM_CHUNK_BYTES it would
        // silently shrink to 267 ms on the 24 kHz downlink.
        val floor = sampleRateHz * AudioFormat.BYTES_PER_SAMPLE * BUFFER_MILLIS / 1000
        return AudioTrack.Builder()
            .setAudioAttributes(
                AudioAttributes.Builder()
                    .setUsage(AudioAttributes.USAGE_ASSISTANT)
                    .setContentType(AudioAttributes.CONTENT_TYPE_SPEECH)
                    .build(),
            )
            .setAudioFormat(
                AndroidAudioFormat.Builder()
                    .setEncoding(AndroidAudioFormat.ENCODING_PCM_16BIT)
                    .setSampleRate(sampleRateHz)
                    .setChannelMask(AndroidAudioFormat.CHANNEL_OUT_MONO)
                    .build(),
            )
            .setBufferSizeInBytes(maxOf(minBuffer, floor))
            .setTransferMode(AudioTrack.MODE_STREAM)
            .build()
            .also {
                // The capture side logs its rate too (AndroidAudioSession).
                // A rate mismatch has no exception and no failed write to
                // find it by, so the opened rate has to be on the record:
                // `adb logcat -s sk.voice` is the whole diagnosis.
                VoiceLog.i(VoiceLog.AUDIO, "agent track opened rate=$sampleRateHz")
                it.play()
            }
    }

    private companion object {
        const val BUFFER_MILLIS = 400
    }
}
