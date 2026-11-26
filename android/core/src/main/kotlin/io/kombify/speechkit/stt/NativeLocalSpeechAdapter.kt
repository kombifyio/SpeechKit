package io.kombify.speechkit.stt

import android.content.Context
import io.kombify.speechkit.stt.parakeet.LocalParakeetSession
import io.kombify.speechkit.stt.parakeet.ParakeetModels
import io.kombify.speechkit.stt.parakeet.ParakeetOnDevice
import io.kombify.speechkit.stt.streaming.DictationSegmentOptions
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.withContext

/** Uses the same selected installed model and process-wide recognizer pool as local dictation. */
class NativeLocalSpeechAdapter(context: Context) : LocalSpeechAdapter {
    private val context = context.applicationContext
    override val id: String = "native"

    override suspend fun availability(options: DictationSegmentOptions): LocalSpeechAvailability = withContext(Dispatchers.IO) {
        val files = try { ParakeetOnDevice.usableFiles(context, options.language ?: "multi") }
            catch (e: CancellationException) { throw e }
            catch (_: Exception) { null }
            catch (_: LinkageError) { null }
            ?: return@withContext LocalSpeechAvailability.UNAVAILABLE
        if (files.supports(options.language)) LocalSpeechAvailability.AVAILABLE else LocalSpeechAvailability.LANGUAGE_UNSUPPORTED
    }

    override suspend fun transcribe(audio: LocalSpeechAudio, options: DictationSegmentOptions): LocalSpeechResult = withContext(Dispatchers.IO) {
        val files = try { ParakeetOnDevice.usableFiles(context, options.language ?: "multi") }
            catch (e: CancellationException) { throw e }
            catch (e: Exception) { throw LocalSpeechFailure(LocalSpeechFailureKind.UNAVAILABLE, "Local model is unavailable", e) }
            catch (e: LinkageError) { throw LocalSpeechFailure(LocalSpeechFailureKind.UNAVAILABLE, "Local model is unavailable", e) }
            ?: throw LocalSpeechFailure(LocalSpeechFailureKind.UNAVAILABLE, "No usable local speech model")
        if (!files.supports(options.language)) throw LocalSpeechFailure(LocalSpeechFailureKind.INPUT, "The selected model does not support this language")
        val pcm = when (audio) {
            is LocalSpeechAudio.Pcm16 -> audio.bytes
            else -> audio.readPcm16(context)
        }
        try {
        if (pcm.size < 2 || pcm.size % 2 != 0 || pcm.size > LocalSpeechLimits.MAX_PCM_BYTES)
            throw LocalSpeechFailure(LocalSpeechFailureKind.INPUT, "Invalid or oversized audio")
        currentCoroutineContext().ensureActive()
        val lease = try { ParakeetOnDevice.recognizers.lease(files) }
            catch (e: CancellationException) { throw e }
            catch (e: Exception) { throw LocalSpeechFailure(LocalSpeechFailureKind.UNAVAILABLE, "Local model is unavailable", e) }
            catch (e: LinkageError) { throw LocalSpeechFailure(LocalSpeechFailureKind.UNAVAILABLE, "Local model is unavailable", e) }
        try {
            val recognizer = try { lease.recognizer() }
                catch (e: CancellationException) { throw e }
                catch (e: Exception) { throw LocalSpeechFailure(LocalSpeechFailureKind.UNAVAILABLE, "Local model is unavailable", e) }
                catch (e: LinkageError) { throw LocalSpeechFailure(LocalSpeechFailureKind.UNAVAILABLE, "Local model is unavailable", e) }
            val text = try {
                withContext(Dispatchers.Default) { recognizer.transcribe(LocalParakeetSession.pcm16ToFloat(pcm)) }
            } catch (e: CancellationException) { throw e }
              catch (e: Exception) { throw LocalSpeechFailure(LocalSpeechFailureKind.TRANSIENT, "Local transcription failed", e) }
              catch (e: LinkageError) { throw LocalSpeechFailure(LocalSpeechFailureKind.UNAVAILABLE, "Local model is unavailable", e) }
            currentCoroutineContext().ensureActive()
            val engineId = ParakeetModels.all.firstOrNull { it.dirName == files.dir.name }?.id
                ?: throw LocalSpeechFailure(LocalSpeechFailureKind.UNAVAILABLE, "Local model identity is unknown")
            LocalSpeechResult(text, engineId)
        } finally {
            lease.release()
        }
        } finally { if (audio !is LocalSpeechAudio.Pcm16) pcm.fill(0) }
    }
}
