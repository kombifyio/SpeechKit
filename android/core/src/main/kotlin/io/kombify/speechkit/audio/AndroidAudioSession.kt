package io.kombify.speechkit.audio

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancelAndJoin
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.launch
import io.kombify.speechkit.log.VoiceLog
import java.io.ByteArrayOutputStream

/**
 * Start/stop buffer over a cold [AudioCapture]. The system assistant and
 * [io.kombify.speechkit.dictation.DefaultDictationSession] need the full
 * utterance as bytes; keyboard and Voice Agent collect [AudioCapture.frames]
 * directly. Both paths share one [MicAudioCapture] so there is a single
 * AudioRecord.
 */
class AndroidAudioSession(
    private val capture: AudioCapture = MicAudioCapture(),
) : AudioSession {

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private val buffer = ByteArrayOutputStream()
    private val frames = MutableSharedFlow<ByteArray>(extraBufferCapacity = 64)
    private var collectJob: Job? = null

    @Volatile
    override var isRecording: Boolean = false
        private set

    override val pcmFrames: Flow<ByteArray> = frames.asSharedFlow()

    override suspend fun start() {
        if (isRecording) return
        synchronized(buffer) { buffer.reset() }
        isRecording = true
        collectJob = scope.launch {
            try {
                capture.frames().collect { frame ->
                    if (!isRecording) return@collect
                    synchronized(buffer) { buffer.write(frame) }
                    frames.emit(frame)
                }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                VoiceLog.e(VoiceLog.AUDIO, "AudioSession capture failed", e)
            }
        }
        VoiceLog.i(VoiceLog.AUDIO, "AudioSession started rate=${AudioFormat.SAMPLE_RATE}")
    }

    override suspend fun stop(): ByteArray {
        isRecording = false
        collectJob?.cancelAndJoin()
        collectJob = null
        val data = synchronized(buffer) { buffer.toByteArray() }
        synchronized(buffer) { buffer.reset() }
        VoiceLog.i(VoiceLog.AUDIO, "AudioSession stopped bytes=${data.size}")
        return data
    }
}
