package io.kombify.speechkit.audio

import android.annotation.SuppressLint
import android.media.AudioFormat as AndroidAudioFormat
import android.media.AudioRecord
import android.media.MediaRecorder
import android.media.audiofx.AcousticEchoCanceler
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flow
import kotlinx.coroutines.flow.flowOn
import kotlinx.coroutines.isActive
import io.kombify.speechkit.log.VoiceLog

/**
 * AudioRecord-backed [AudioCapture]: 16 kHz S16 mono in 100 ms chunks.
 *
 * Cold flow: the mic opens on collect and is released on cancel. Callers gate
 * on RECORD_AUDIO before collecting; lint cannot see through that FSM.
 *
 * ## Duplex
 *
 * [duplex] selects the capture chain a full-duplex conversation needs. The
 * default is the one every other mode wants: `VOICE_RECOGNITION`, which the
 * platform documents as *not* carrying automatic gain control or noise
 * suppression, because that preprocessing costs transcription accuracy.
 *
 * A conversation where the loudspeaker is live while the microphone is open
 * needs the opposite trade. `VOICE_COMMUNICATION` is the source the platform
 * routes through its own echo-cancelling chain, and [AcousticEchoCanceler] is
 * attached on top where the device offers it. Cancelling echo in the platform
 * is not something a framework should reimplement in Kotlin: the reference
 * signal and the loop delay live in the audio HAL, not in an app.
 *
 * The result is reported by [echoControl], which is what a
 * `io.kombify.speechkit.turn.TurnEngine` should be constructed with — the host
 * reads it off the capture rather than deciding.
 */
class MicAudioCapture(
    private val sampleRateHz: Int = AudioFormat.SAMPLE_RATE,
    private val chunkBytes: Int = AudioFormat.STREAM_CHUNK_BYTES,
    private val duplex: Boolean = false,
) : AudioCapture {

    /**
     * What the turn engine should assume about loudspeaker leakage in frames
     * from this capture.
     *
     * A prediction, not a measurement: the device advertises the effect here
     * and it is attached when the recorder starts. An attach that fails is
     * logged (`sk.voice audio`) and leaves the engine's guard more permissive
     * than the room warrants for that session, which is a device-evidence
     * item rather than something the host can check up front.
     */
    val echoControl: EchoControl
        get() = if (duplex && runCatching { AcousticEchoCanceler.isAvailable() }.getOrDefault(false)) {
            EchoControl.PlatformAec
        } else {
            EchoControl.None
        }

    @SuppressLint("MissingPermission")
    override fun frames(): Flow<ByteArray> = flow {
        val source = if (duplex) {
            MediaRecorder.AudioSource.VOICE_COMMUNICATION
        } else {
            MediaRecorder.AudioSource.VOICE_RECOGNITION
        }
        val minBuffer = AudioRecord.getMinBufferSize(
            sampleRateHz,
            AndroidAudioFormat.CHANNEL_IN_MONO,
            AndroidAudioFormat.ENCODING_PCM_16BIT,
        )
        val recorder = AudioRecord(
            source,
            sampleRateHz,
            AndroidAudioFormat.CHANNEL_IN_MONO,
            AndroidAudioFormat.ENCODING_PCM_16BIT,
            maxOf(minBuffer, chunkBytes),
        )
        if (recorder.state != AudioRecord.STATE_INITIALIZED) {
            recorder.release()
            VoiceLog.e(VoiceLog.AUDIO, "AudioRecord failed to initialize")
            error("AudioRecord failed to initialize")
        }
        val canceller = if (duplex) attachEchoCanceller(recorder.audioSessionId) else null
        try {
            recorder.startRecording()
            VoiceLog.i(
                VoiceLog.AUDIO,
                "mic capture started rate=$sampleRateHz chunk=$chunkBytes duplex=$duplex " +
                    "aec=${canceller?.enabled ?: false}",
            )
            val chunk = ByteArray(chunkBytes)
            while (currentCoroutineContext().isActive) {
                val read = recorder.read(chunk, 0, chunk.size)
                when {
                    read > 0 -> emit(chunk.copyOf(read))
                    read < 0 -> {
                        VoiceLog.e(VoiceLog.AUDIO, "AudioRecord.read failed code=$read")
                        error("AudioRecord.read failed: $read")
                    }
                }
            }
        } finally {
            runCatching { canceller?.release() }
            runCatching { recorder.stop() }
            recorder.release()
        }
    }.flowOn(Dispatchers.IO)

    private fun attachEchoCanceller(sessionId: Int): AcousticEchoCanceler? {
        if (!runCatching { AcousticEchoCanceler.isAvailable() }.getOrDefault(false)) {
            VoiceLog.w(VoiceLog.AUDIO, "no platform AEC on this device")
            return null
        }
        val canceller = runCatching { AcousticEchoCanceler.create(sessionId) }.getOrNull()
        if (canceller == null) {
            VoiceLog.w(VoiceLog.AUDIO, "platform AEC create failed")
            return null
        }
        val enabled = runCatching { canceller.setEnabled(true) }.getOrNull()
        if (enabled != android.media.audiofx.AudioEffect.SUCCESS) {
            VoiceLog.w(VoiceLog.AUDIO, "platform AEC enable failed code=$enabled")
        }
        return canceller
    }
}
