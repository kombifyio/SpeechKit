package io.kombify.speechkit.audio

import kotlinx.coroutines.flow.Flow

/**
 * Audio capture session interface.
 *
 * Mirrors: internal/audio/capturer.go AudioSession interface.
 * Go callbacks map to Kotlin Flow emissions.
 *
 * All audio is captured as PCM 16-bit signed mono at 16kHz,
 * matching the Go implementation's WASAPI capture format.
 */
interface AudioSession {
    /** Start capturing audio. Emits PCM frames via [pcmFrames]. */
    suspend fun start()

    /** Stop capturing and return the complete audio buffer. */
    suspend fun stop(): ByteArray

    /** Flow of raw PCM frames (16kHz, 16-bit signed, mono) during capture. */
    val pcmFrames: Flow<ByteArray>

    /** Whether the session is currently recording. */
    val isRecording: Boolean
}

/** Audio format constants matching the Go implementation. */
object AudioFormat {
    const val SAMPLE_RATE = 16000
    const val CHANNELS = 1
    const val BITS_PER_SAMPLE = 16
    const val BYTES_PER_SAMPLE = 2
    const val FRAME_SIZE_BYTES = 512 // Matches Silero VAD expectation
}
