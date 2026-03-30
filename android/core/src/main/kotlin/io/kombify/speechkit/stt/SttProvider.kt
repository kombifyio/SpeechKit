package io.kombify.speechkit.stt

import kotlin.time.Duration

/**
 * Speech-to-text provider interface.
 *
 * Mirrors: internal/stt/provider.go STTProvider interface.
 * All implementations use the OpenAI-compatible /v1/audio/transcriptions API contract.
 */
interface SttProvider {
    /** Transcribe audio data and return the result. */
    suspend fun transcribe(audio: ByteArray, opts: TranscribeOpts): Result

    /** Provider identifier (e.g. "local", "vps", "huggingface"). */
    val name: String

    /** Check if the provider is reachable and ready. Throws on failure. */
    suspend fun health()
}

/** Mirrors: internal/stt/provider.go TranscribeOpts. */
data class TranscribeOpts(
    val language: String = "de",
    val model: String? = null,
)

/** Mirrors: internal/stt/provider.go Result. */
data class Result(
    val text: String,
    val language: String,
    val duration: Duration,
    val provider: String,
    val model: String,
    val confidence: Double = 0.0,
)
