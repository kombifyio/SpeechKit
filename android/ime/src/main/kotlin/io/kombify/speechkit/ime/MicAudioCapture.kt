package io.kombify.speechkit.ime

import android.annotation.SuppressLint
import android.media.AudioFormat
import android.media.AudioRecord
import android.media.MediaRecorder
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flow
import kotlinx.coroutines.flow.flowOn
import kotlinx.coroutines.isActive

/**
 * AudioRecord-backed [AudioCapture]: 16 kHz S16 mono in 100 ms chunks — the
 * same pipeline the dev dictation screen streams, sized so drafts feel live
 * without flooding the socket.
 *
 * Cold flow contract: the mic opens when the flow is collected and is
 * guaranteed released when collection is cancelled — the controller cancels
 * the capture job the instant it leaves Listening, which is what makes the
 * visible capture indicator (privacy checklist) truthful.
 */
class MicAudioCapture(
    private val sampleRateHz: Int = SAMPLE_RATE_HZ,
    private val chunkBytes: Int = CHUNK_BYTES,
) : AudioCapture {

    // Callers gate on MicPermissionGate before collecting; lint cannot see
    // through the FSM (same rationale as the dev screen).
    @SuppressLint("MissingPermission")
    override fun frames(): Flow<ByteArray> = flow {
        val minBuffer = AudioRecord.getMinBufferSize(
            sampleRateHz, AudioFormat.CHANNEL_IN_MONO, AudioFormat.ENCODING_PCM_16BIT,
        )
        val recorder = AudioRecord(
            MediaRecorder.AudioSource.VOICE_RECOGNITION,
            sampleRateHz,
            AudioFormat.CHANNEL_IN_MONO,
            AudioFormat.ENCODING_PCM_16BIT,
            maxOf(minBuffer, chunkBytes),
        )
        if (recorder.state != AudioRecord.STATE_INITIALIZED) {
            recorder.release()
            error("AudioRecord failed to initialize")
        }
        try {
            recorder.startRecording()
            val chunk = ByteArray(chunkBytes)
            while (currentCoroutineContext().isActive) {
                val read = recorder.read(chunk, 0, chunk.size)
                when {
                    read > 0 -> emit(chunk.copyOf(read))
                    read < 0 -> error("AudioRecord.read failed: $read")
                }
            }
        } finally {
            runCatching { recorder.stop() }
            recorder.release()
        }
    }.flowOn(Dispatchers.IO)

    companion object {
        const val SAMPLE_RATE_HZ = 16_000

        /** 100 ms at 16 kHz S16 mono. */
        const val CHUNK_BYTES = 3_200
    }
}
