package io.kombify.speechkit.audio

import android.Manifest
import android.media.AudioFormat as AndroidAudioFormat
import android.media.AudioRecord
import android.media.MediaRecorder
import androidx.annotation.RequiresPermission
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.channels.awaitClose
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.callbackFlow
import kotlinx.coroutines.isActive
import kotlinx.coroutines.withContext
import timber.log.Timber
import java.io.ByteArrayOutputStream

/**
 * Android AudioRecord-based audio capture.
 *
 * Captures PCM 16-bit signed mono at 16kHz, matching the Go WASAPI implementation.
 * Emits frames of [AudioFormat.FRAME_SIZE_BYTES] bytes via [pcmFrames] Flow.
 */
class AndroidAudioSession : AudioSession {

    private var recorder: AudioRecord? = null
    private var buffer: ByteArrayOutputStream? = null

    @Volatile
    override var isRecording: Boolean = false
        private set

    override val pcmFrames: Flow<ByteArray> = callbackFlow {
        val minBufferSize = AudioRecord.getMinBufferSize(
            AudioFormat.SAMPLE_RATE,
            AndroidAudioFormat.CHANNEL_IN_MONO,
            AndroidAudioFormat.ENCODING_PCM_16BIT,
        )
        val bufferSize = maxOf(minBufferSize, AudioFormat.SAMPLE_RATE * AudioFormat.BYTES_PER_SAMPLE)

        val record = recorder ?: run {
            close()
            return@callbackFlow
        }

        val frame = ByteArray(AudioFormat.FRAME_SIZE_BYTES)

        withContext(Dispatchers.IO) {
            while (isActive && isRecording) {
                val bytesRead = record.read(frame, 0, frame.size)
                if (bytesRead > 0) {
                    buffer?.write(frame, 0, bytesRead)
                    trySend(frame.copyOf(bytesRead))
                } else if (bytesRead == AudioRecord.ERROR_BAD_VALUE || bytesRead == AudioRecord.ERROR) {
                    Timber.e("AudioRecord read error: $bytesRead")
                    break
                }
            }
        }

        awaitClose { /* cleanup handled by stop() */ }
    }

    @RequiresPermission(Manifest.permission.RECORD_AUDIO)
    override suspend fun start() {
        if (isRecording) return

        val minBufferSize = AudioRecord.getMinBufferSize(
            AudioFormat.SAMPLE_RATE,
            AndroidAudioFormat.CHANNEL_IN_MONO,
            AndroidAudioFormat.ENCODING_PCM_16BIT,
        )

        if (minBufferSize == AudioRecord.ERROR || minBufferSize == AudioRecord.ERROR_BAD_VALUE) {
            throw IllegalStateException("AudioRecord: unsupported audio configuration")
        }

        val bufferSize = maxOf(minBufferSize, AudioFormat.SAMPLE_RATE * AudioFormat.BYTES_PER_SAMPLE)

        val record = AudioRecord(
            MediaRecorder.AudioSource.MIC,
            AudioFormat.SAMPLE_RATE,
            AndroidAudioFormat.CHANNEL_IN_MONO,
            AndroidAudioFormat.ENCODING_PCM_16BIT,
            bufferSize,
        )

        if (record.state != AudioRecord.STATE_INITIALIZED) {
            record.release()
            throw IllegalStateException("AudioRecord failed to initialize")
        }

        buffer = ByteArrayOutputStream()
        recorder = record
        record.startRecording()
        isRecording = true
        Timber.d("AudioSession started: ${AudioFormat.SAMPLE_RATE}Hz, 16-bit, mono")
    }

    override suspend fun stop(): ByteArray {
        val record = recorder ?: return ByteArray(0)
        val data = buffer?.toByteArray() ?: ByteArray(0)

        isRecording = false
        record.stop()
        record.release()
        recorder = null
        buffer?.close()
        buffer = null

        Timber.d("AudioSession stopped: ${data.size} bytes captured")
        return data
    }
}
