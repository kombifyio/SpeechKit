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

    /** 100 ms of 16 kHz S16 mono — streaming dictation and Voice Agent chunks. */
    const val STREAM_CHUNK_BYTES = SAMPLE_RATE * BYTES_PER_SAMPLE / 10
}

/**
 * Reinterprets a capture frame as 16-bit signed little-endian samples, the
 * shape every [io.kombify.speechkit.vad.VadDetector] expects.
 *
 * A trailing odd byte cannot form a sample and is dropped.
 */
fun ByteArray.toPcm16Samples(): ShortArray {
    val samples = ShortArray(size / 2)
    for (i in samples.indices) {
        samples[i] = ((this[i * 2].toInt() and 0xFF) or
            (this[i * 2 + 1].toInt() shl 8)).toShort()
    }
    return samples
}

/**
 * Duration of a **capture** frame in milliseconds.
 *
 * Capture is the one genuinely fixed leg of the pipeline: every microphone
 * path in this SDK records at [AudioFormat.SAMPLE_RATE], so the rate is not a
 * question the caller should be asked.
 *
 * Playback is not fixed — the Voice Agent downlink is 24 kHz — and there is
 * deliberately no rate-taking overload of this function. An overload would put
 * the wrong answer under the right name: a caller holding playback PCM could
 * write `pcm.frameDurationMillis()`, get 16 kHz arithmetic, and have nothing
 * flag it. Code that measures playback takes the rate as a required argument
 * instead; see [io.kombify.speechkit.turn.TurnEngine.notePlaybackFrame].
 */
fun ByteArray.frameDurationMillis(): Long =
    (size.toLong() * 1000) / (AudioFormat.SAMPLE_RATE * AudioFormat.BYTES_PER_SAMPLE)
