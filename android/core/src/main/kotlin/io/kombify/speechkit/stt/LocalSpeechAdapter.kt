package io.kombify.speechkit.stt

import android.net.Uri
import android.content.Context
import io.kombify.speechkit.stt.streaming.DictationSegmentOptions
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

/** Replayable local input. PCM is 16 kHz, mono, little-endian signed 16-bit audio. */
sealed interface LocalSpeechAudio {
    /** The caller owns these bytes until the suspended operation returns; adapters must not retain them. */
    data class Pcm16(val bytes: ByteArray) : LocalSpeechAudio
    /** A bounded encoded file snapshot; used when cloud and device must see the same original bytes. */
    data class Encoded(val bytes: ByteArray) : LocalSpeechAudio
    /** The caller retains URI access for the operation. Adapters must bound decoded audio as well as input bytes. */
    data class Content(val uri: Uri) : LocalSpeechAudio
}

enum class LocalSpeechAvailability { AVAILABLE, UNAVAILABLE, LANGUAGE_UNSUPPORTED }
enum class LocalSpeechFailureKind { UNAVAILABLE, TRANSIENT, INPUT, PERMISSION }

/** Only UNAVAILABLE and TRANSIENT admit a second local attempt. Cancellation always propagates unchanged. */
class LocalSpeechFailure(val kind: LocalSpeechFailureKind, message: String, cause: Throwable? = null) :
    Exception(message, cause)

data class LocalSpeechResult(val text: String, val engineId: String, val fallbackFrom: String? = null)

/**
 * Optional SDK adapter for side-effect-free, strictly local transcription of one complete recording.
 * Implementations never download a model, open a microphone, contact a remote inference service or emit
 * intermediate finals. Availability must honor the requested language, including SpeechKit's multi mode.
 * The router owns ordering and retry; the adapter owns only its SDK/model lifecycle and actual provenance.
 */
interface LocalSpeechAdapter {
    val id: String
    suspend fun availability(options: DictationSegmentOptions): LocalSpeechAvailability
    suspend fun transcribe(audio: LocalSpeechAudio, options: DictationSegmentOptions): LocalSpeechResult
}

object LocalSpeechLimits {
    const val SAMPLE_RATE = 16_000
    const val MAX_PCM_BYTES = SAMPLE_RATE * 2 * 120
    const val MAX_INPUT_BYTES = 14 * 1024 * 1024
}

/** A bounded, owned snapshot shared by all local retries; erase it after the operation. */
suspend fun LocalSpeechAudio.readPcm16(context: Context): ByteArray = withContext(Dispatchers.IO) {
    try { when (val input = this@readPcm16) {
        is LocalSpeechAudio.Pcm16 -> {
            if (input.bytes.isEmpty() || input.bytes.size % 2 != 0)
                throw LocalSpeechFailure(LocalSpeechFailureKind.INPUT, "PCM input has an invalid length")
            if (input.bytes.size > LocalSpeechLimits.MAX_PCM_BYTES)
                throw LocalSpeechFailure(LocalSpeechFailureKind.UNAVAILABLE, "Audio exceeds local processing capacity")
            input.bytes.copyOf()
        }
        is LocalSpeechAudio.Content -> BoundedLocalAudioDecoder.decode(context.applicationContext, input.uri)
        is LocalSpeechAudio.Encoded -> BoundedLocalAudioDecoder.decodeContainer(input.bytes)
    } } catch (cancelled: kotlinx.coroutines.CancellationException) { throw cancelled }
      catch (failure: LocalSpeechFailure) { throw failure }
      catch (failure: SecurityException) { throw LocalSpeechFailure(LocalSpeechFailureKind.PERMISSION, "Audio access was denied", failure) }
      catch (failure: Exception) { throw LocalSpeechFailure(LocalSpeechFailureKind.INPUT, "Cannot decode audio", failure) }
}
