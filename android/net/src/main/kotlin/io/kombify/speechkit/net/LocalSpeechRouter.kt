package io.kombify.speechkit.net

import android.content.Context
import io.kombify.speechkit.domain.ExecutionFailure
import io.kombify.speechkit.domain.ExecutionFailureKind
import io.kombify.speechkit.stt.LocalSpeechAdapter
import io.kombify.speechkit.stt.LocalSpeechAudio
import io.kombify.speechkit.stt.LocalSpeechAvailability
import io.kombify.speechkit.stt.LocalSpeechFailure
import io.kombify.speechkit.stt.LocalSpeechFailureKind
import io.kombify.speechkit.stt.LocalSpeechLimits
import io.kombify.speechkit.stt.LocalSpeechResult
import io.kombify.speechkit.stt.NativeLocalSpeechAdapter
import io.kombify.speechkit.stt.readPcm16
import io.kombify.speechkit.stt.streaming.DictationSegmentOptions
import io.kombify.speechkit.stt.streaming.StreamingSttSession
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.ensureActive

/**
 * Host composition hook for optional local SDKs. Register once during Application startup.
 * The factory receives application context and creates operation-scoped adapters, never a retained Activity.
 * A standalone SDK host may instead supply its adapters directly to LocalSpeechRouter.
 */
object LocalSpeechAdapters {
    @Volatile private var factory: ((Context) -> List<LocalSpeechAdapter>)? = null
    val enabled: Boolean get() = factory != null
    fun install(factory: ((Context) -> List<LocalSpeechAdapter>)?) { this.factory = factory }
    fun router(context: Context): LocalSpeechRouter? {
        val optional = factory ?: return null
        val app = context.applicationContext
        return LocalSpeechRouter(app, listOf(NativeLocalSpeechAdapter(app)) + optional(app))
    }
}

/**
 * One local attempt order, reused by microphone segments, imported files and cloud-to-device fallback.
 * It cannot contact a remote endpoint. The caller's ExecutionPolicy owns that separate decision.
 */
class LocalSpeechRouter(
    context: Context,
    adapters: List<LocalSpeechAdapter>,
    preferSystem: Boolean = OnDeviceEngine.choice(context) == OnDeviceEngine.Choice.SYSTEM,
) {
    private val context = context.applicationContext
    private val adapters = adapters.distinctBy { it.id }.sortedBy {
        if ((it.id == "native") == preferSystem) 1 else 0
    }

    suspend fun available(options: DictationSegmentOptions = DictationSegmentOptions()): Boolean {
        for (adapter in adapters) if (availability(adapter, options) == LocalSpeechAvailability.AVAILABLE) return true
        return false
    }

    suspend fun transcribe(audio: LocalSpeechAudio, options: DictationSegmentOptions = DictationSegmentOptions()): LocalSpeechResult {
        if (!available(options)) throw ExecutionFailure(ExecutionFailureKind.UNAVAILABLE, "No local speech adapter supports this language")
        // A ContentProvider is read once: every attempt receives exactly the same bounded recording.
        val pcm = try { audio.readPcm16(context) } catch (failure: LocalSpeechFailure) { throw failure.asExecutionFailure() }
        try {
            var previous: String? = null
            var failure: ExecutionFailure? = null
            for (adapter in adapters) {
                currentCoroutineContext().ensureActive()
                if (availability(adapter, options) != LocalSpeechAvailability.AVAILABLE) {
                    if (previous == null) previous = adapter.id
                    continue
                }
                try {
                    val result = adapter.transcribe(LocalSpeechAudio.Pcm16(pcm), options)
                    currentCoroutineContext().ensureActive()
                    // Empty text means silence, not a reason to replay the recording.
                    return result.copy(fallbackFrom = previous?.takeIf { it != adapter.id })
                } catch (cancelled: CancellationException) { throw cancelled }
                  catch (error: LocalSpeechFailure) {
                    if (error.kind !in setOf(LocalSpeechFailureKind.UNAVAILABLE, LocalSpeechFailureKind.TRANSIENT))
                        throw error.asExecutionFailure()
                    previous = adapter.id
                    failure = error.asExecutionFailure()
                }
            }
            throw failure ?: ExecutionFailure(ExecutionFailureKind.UNAVAILABLE, "No local speech adapter supports this recording and language")
        } finally { pcm.fill(0) }
    }

    /** Capture ownership never changes. Only a completed, uncommitted recording is retried locally. */
    fun openSession(onResult: (LocalSpeechResult) -> Unit = {}): StreamingSttSession =
        BatchDictationSession.withPcmTranscriber(LocalSpeechLimits.MAX_PCM_BYTES) { pcm, options ->
            val result = transcribe(LocalSpeechAudio.Pcm16(pcm), options)
            onResult(result)
            BatchTranscribeResponse(text = result.text)
        }

    private suspend fun availability(adapter: LocalSpeechAdapter, options: DictationSegmentOptions): LocalSpeechAvailability =
        try { adapter.availability(options) }
        catch (cancelled: CancellationException) { throw cancelled }
        catch (failure: LocalSpeechFailure) {
            if (failure.kind !in setOf(LocalSpeechFailureKind.UNAVAILABLE, LocalSpeechFailureKind.TRANSIENT)) throw failure.asExecutionFailure()
            LocalSpeechAvailability.UNAVAILABLE
        }

    private fun LocalSpeechFailure.asExecutionFailure(): ExecutionFailure = ExecutionFailure(when (kind) {
        LocalSpeechFailureKind.UNAVAILABLE -> ExecutionFailureKind.UNAVAILABLE
        LocalSpeechFailureKind.TRANSIENT -> ExecutionFailureKind.TRANSIENT
        LocalSpeechFailureKind.PERMISSION -> ExecutionFailureKind.POLICY_DENIED
        LocalSpeechFailureKind.INPUT -> ExecutionFailureKind.OTHER
    }, "Local speech recognition failed", this)
}
