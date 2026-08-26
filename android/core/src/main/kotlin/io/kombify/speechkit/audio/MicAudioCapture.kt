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
     * Set from the attach in [frames], cleared when capture ends. Volatile
     * because the turn engine reads it from the collector that drains capture
     * while the recorder thread writes it.
     */
    @Volatile
    private var attachedEchoControl: EchoControl = EchoControl.None

    /**
     * What the canceller on *this* capture session is actually doing.
     *
     * A measurement, not a prediction. `AcousticEchoCanceler.isAvailable()` —
     * which this used to return — is a static device-capability query,
     * answerable before any recorder exists, and it says nothing about the
     * session in front of it: the platform documents that an AEC "can be
     * inserted by default in the capture path according to the
     * `MediaRecorder.AudioSource` used" and that an application "should call
     * `getEnabled()` after creating the AEC to check the default activation
     * state on a particular `AudioRecord` session".
     *
     * [EchoControl.None] until an attach has succeeded, and `None` again once
     * capture closes. Claiming [EchoControl.PlatformAec] on a device that
     * merely advertises the effect buys the *lenient* barge-in gate on a
     * microphone that is hearing the loudspeaker unaided, which is how an
     * agent comes to interrupt itself with its own answer.
     *
     * Read it whenever frames flow and forward it to
     * `io.kombify.speechkit.turn.TurnEngine.noteEchoControl`. Reading it once,
     * before collecting, only ever returns `None`.
     *
     * ## What [EchoControl.PlatformAec] does not promise
     *
     * That the effect attached and enabled on this session — not that it is
     * removing the sound coming out of the loudspeaker. The platform canceller
     * is specified against "the signal received from the remote party", the
     * voice-communication downlink, so a host that captures here while playing
     * the far end back through anything else gives it nothing to subtract: the
     * effect reports itself enabled and the microphone still hears the
     * loudspeaker at full volume. A duplex host owes the session the whole
     * arrangement — `AudioManager.MODE_IN_COMMUNICATION`, playback on
     * `AudioAttributes.USAGE_VOICE_COMMUNICATION`, and an output device the
     * person can hear — and this class cannot check that from where it stands.
     */
    val echoControl: EchoControl
        get() = attachedEchoControl

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
            attachedEchoControl = EchoControl.None
            runCatching { canceller?.release() }
            runCatching { recorder.stop() }
            recorder.release()
        }
    }.flowOn(Dispatchers.IO)

    /**
     * Attaches the platform canceller and records what actually happened.
     *
     * Every way this can fall short ends at [EchoControl.None] and says so in
     * the log, because the turn engine spends that answer: `None` picks the
     * strict barge-in gate, and a silent failure here is an agent that cuts
     * itself off on its own voice with nothing on the device to explain why.
     */
    private fun attachEchoCanceller(sessionId: Int): AcousticEchoCanceler? {
        if (!runCatching { AcousticEchoCanceler.isAvailable() }.getOrDefault(false)) {
            VoiceLog.w(VoiceLog.AUDIO, "no platform AEC on this device; strict barge-in gate")
            return null
        }
        val canceller = runCatching { AcousticEchoCanceler.create(sessionId) }.getOrNull()
        if (canceller == null) {
            VoiceLog.w(VoiceLog.AUDIO, "platform AEC create failed; strict barge-in gate")
            return null
        }
        val code = runCatching { canceller.setEnabled(true) }.getOrNull()
        // Read back rather than trust the call: the platform may have inserted
        // the effect itself, or refused to enable the one just created.
        val enabled = runCatching { canceller.enabled }.getOrDefault(false)
        if (enabled) {
            attachedEchoControl = EchoControl.PlatformAec
            VoiceLog.i(VoiceLog.AUDIO, "platform AEC attached and enabled on session $sessionId")
        } else {
            VoiceLog.w(
                VoiceLog.AUDIO,
                "platform AEC attached but not enabled (setEnabled=$code, " +
                    "expected ${android.media.audiofx.AudioEffect.SUCCESS}); strict barge-in gate",
            )
        }
        return canceller
    }
}
