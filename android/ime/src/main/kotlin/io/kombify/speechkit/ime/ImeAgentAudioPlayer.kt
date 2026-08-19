package io.kombify.speechkit.ime

import android.media.AudioAttributes
import android.media.AudioFormat
import android.media.AudioTrack
import timber.log.Timber

/**
 * Plays the agent's spoken answer inside the keyboard window.
 *
 * The track is created lazily and released when the conversation ends: an
 * input method is started and stopped constantly as the user moves between
 * fields, and holding an open audio track across that would keep the audio
 * focus of a keyboard that is not even visible.
 */
internal class ImeAgentAudioPlayer(
    private val sampleRateHz: Int = 16_000,
) {
    private var track: AudioTrack? = null

    fun play(pcm: ByteArray) {
        if (pcm.isEmpty()) return
        val active = track ?: create().also { track = it }
        runCatching { active.write(pcm, 0, pcm.size) }
            .onFailure { Timber.w(it, "agent audio playback failed") }
    }

    fun release() {
        runCatching {
            track?.stop()
            track?.release()
        }
        track = null
    }

    private fun create(): AudioTrack {
        val minBuffer = AudioTrack.getMinBufferSize(
            sampleRateHz,
            AudioFormat.CHANNEL_OUT_MONO,
            AudioFormat.ENCODING_PCM_16BIT,
        )
        return AudioTrack.Builder()
            .setAudioAttributes(
                AudioAttributes.Builder()
                    .setUsage(AudioAttributes.USAGE_ASSISTANT)
                    .setContentType(AudioAttributes.CONTENT_TYPE_SPEECH)
                    .build(),
            )
            .setAudioFormat(
                AudioFormat.Builder()
                    .setEncoding(AudioFormat.ENCODING_PCM_16BIT)
                    .setSampleRate(sampleRateHz)
                    .setChannelMask(AudioFormat.CHANNEL_OUT_MONO)
                    .build(),
            )
            .setBufferSizeInBytes(maxOf(minBuffer, MIN_BUFFER_BYTES))
            .setTransferMode(AudioTrack.MODE_STREAM)
            .build()
            .also { it.play() }
    }

    private companion object {
        const val MIN_BUFFER_BYTES = 12_800 // 400 ms of 16 kHz S16 mono
    }
}
