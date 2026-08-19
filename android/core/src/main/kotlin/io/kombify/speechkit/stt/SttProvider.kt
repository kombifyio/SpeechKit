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

/**
 * The multilanguage value SpeechKit carries internally.
 *
 * SpeechKit does not narrow speech to one language. A pinned language breaks
 * switching language mid-conversation and speaking different languages with
 * different people in one session, and it is a silent data-loss bug: the same
 * English audio yields a zero-length transcript at `language=de` and a full
 * transcript in multilanguage mode, both with HTTP 200 and no error.
 *
 * This is the value, not a fallback something else may replace. An explicit
 * user pin stays supported as a deliberate action, but nothing may infer one
 * from the device locale, from history, or from an omitted field.
 *
 * Each provider adapter translates this into its own native expression —
 * `multi` is only Deepgram's spelling, and the Android platform recognizer
 * expresses it by not pinning a language tag at all. See
 * [isMultilanguage] and `pkg/speechkit/provideropts` for the per-provider
 * support matrix.
 */
const val LANGUAGE_MULTI = "multi"

/** True when [language] asks for multilanguage rather than a specific locale. */
fun isMultilanguage(language: String?): Boolean {
    val normalized = language?.trim()?.lowercase()
    return normalized.isNullOrEmpty() || normalized == LANGUAGE_MULTI || normalized == "auto"
}

/** Mirrors: internal/stt/provider.go TranscribeOpts. */
data class TranscribeOpts(
    val language: String = LANGUAGE_MULTI,
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
