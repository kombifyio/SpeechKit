package io.kombify.speechkit.stt.parakeet

import io.kombify.speechkit.log.VoiceLog
import io.kombify.speechkit.stt.streaming.DictationSegmentOptions
import io.kombify.speechkit.stt.streaming.StreamingSttSession
import io.kombify.speechkit.stt.streaming.TranscriptEvent
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.receiveAsFlow
import kotlinx.coroutines.withContext
import java.io.ByteArrayOutputStream

/**
 * The on-device tier of [StreamingSttSession] (milestone B-M7): Parakeet transcribes each segment on the phone
 * after [finishSegment], with no server, key or network. The caller pushes 16 kHz S16 mono PCM through
 * [sendAudio], like for the batch server tier; there are no drafts, one [TranscriptEvent.Final] per segment.
 *
 * Starting a segment leases the model, which starts loading it in the background, so the seconds a cold load
 * takes overlap with the user speaking; the lease keeps the model open until the segment's transcript is out.
 * A segment pinned to a language Parakeet does not transcribe ([ParakeetLanguages]) fails at once with code
 * `language_unsupported`. Starting a segment while one is still open (a host that abandoned it after a
 * microphone failure) ends the stale one with [TranscriptEvent.SegmentDone] first.
 *
 * Failure codes: `language_unsupported`, `local_model_unavailable` (the model failed to load),
 * `local_transcription_failed`, `audio_too_long`.
 */
class LocalParakeetSession(
    private val files: ParakeetModelFiles,
    private val recognizers: ParakeetRecognizers = ParakeetOnDevice.recognizers,
) : StreamingSttSession {

    private val channel = Channel<TranscriptEvent>(Channel.UNLIMITED)
    private val buffer = ByteArrayOutputStream()
    private var segmentId = 0L
    private var lease: ParakeetLease? = null
    private var overflow = false

    @Volatile
    private var active = false

    @Volatile
    private var closed = false

    override val events: Flow<TranscriptEvent> = channel.receiveAsFlow()

    override suspend fun startSegment(options: DictationSegmentOptions) {
        if (closed) return
        if (active) abandonSegment()
        segmentId += 1
        val sid = segmentId
        if (!ParakeetLanguages.supports(options.language)) {
            channel.trySend(
                TranscriptEvent.Failure(
                    code = "language_unsupported",
                    message = "on-device Parakeet does not transcribe ${options.language}",
                ),
            )
            channel.trySend(TranscriptEvent.SegmentDone(sid))
            return
        }
        synchronized(buffer) { buffer.reset() }
        overflow = false
        lease?.release()
        lease = recognizers.lease(files)
        active = true
        channel.trySend(TranscriptEvent.SegmentReady(sid))
    }

    override suspend fun sendAudio(pcm: ByteArray) {
        if (!active) return
        synchronized(buffer) {
            if (buffer.size() + pcm.size > MAX_SEGMENT_BYTES) {
                overflow = true
            } else {
                buffer.write(pcm)
            }
        }
    }

    override suspend fun finishSegment() {
        if (!active) return
        active = false
        val sid = segmentId
        val pcm = synchronized(buffer) { buffer.toByteArray().also { buffer.reset() } }
        val held = lease
        lease = null
        try {
            if (overflow) {
                channel.trySend(
                    TranscriptEvent.Failure(
                        code = "audio_too_long",
                        message = "on-device dictation takes up to ${MAX_SEGMENT_BYTES / BYTES_PER_MINUTE} minutes per recording",
                    ),
                )
            }
            if (pcm.size < 2 || held == null) {
                channel.trySend(TranscriptEvent.Final(segmentId = sid, text = ""))
            } else {
                transcribe(sid, pcm, held)
            }
        } finally {
            held?.release()
            // Contract: every started segment terminates with SegmentDone.
            channel.trySend(TranscriptEvent.SegmentDone(sid))
        }
    }

    private suspend fun transcribe(sid: Long, pcm: ByteArray, held: ParakeetLease) {
        val model = try {
            held.recognizer()
        } catch (e: CancellationException) {
            throw e
        } catch (e: Throwable) {
            VoiceLog.e(VoiceLog.DICTATION, "parakeet load failed model=${files.dir.name}", e)
            channel.trySend(
                TranscriptEvent.Failure("local_model_unavailable", e.message ?: "the on-device model failed to load"),
            )
            return
        }
        try {
            val started = System.nanoTime()
            val text = withContext(Dispatchers.Default) { model.transcribe(pcm16ToFloat(pcm)) }
            val elapsedMs = (System.nanoTime() - started) / 1_000_000
            VoiceLog.i(
                VoiceLog.DICTATION,
                "parakeet segment=$sid audio_ms=${pcm.size / 32} decode_ms=$elapsedMs chars=${text.length}",
            )
            channel.trySend(TranscriptEvent.Final(segmentId = sid, text = text))
        } catch (e: CancellationException) {
            throw e
        } catch (e: Throwable) {
            VoiceLog.e(VoiceLog.DICTATION, "parakeet transcription failed segment=$sid", e)
            channel.trySend(
                TranscriptEvent.Failure("local_transcription_failed", e.message ?: "on-device transcription failed"),
            )
        }
    }

    /** Ends a segment the host never finished: its audio is dropped, its lease returned. */
    private fun abandonSegment() {
        active = false
        synchronized(buffer) { buffer.reset() }
        lease?.release()
        lease = null
        channel.trySend(TranscriptEvent.SegmentDone(segmentId))
    }

    override suspend fun close() {
        if (closed) return
        closed = true
        active = false
        lease?.release()
        lease = null
        channel.trySend(TranscriptEvent.Closed(CLOSE_REASON_CLIENT))
        channel.close()
    }

    companion object {
        // Same value as the server protocol's client end reason.
        private const val CLOSE_REASON_CLIENT = "client"
        private const val BYTES_PER_MINUTE = 16_000 * 2 * 60

        /**
         * One segment buffers at most 10 minutes of 16 kHz S16 audio (about 19 MB). Its PCM, copy and float
         * samples then stay well inside an app's Java heap.
         */
        private const val MAX_SEGMENT_BYTES = 10 * BYTES_PER_MINUTE

        /** Little-endian 16-bit PCM to samples in [-1, 1]; a trailing odd byte is dropped. */
        fun pcm16ToFloat(pcm: ByteArray): FloatArray = FloatArray(pcm.size / 2) { i ->
            val lo = pcm[2 * i].toInt() and 0xff
            val hi = pcm[2 * i + 1].toInt()
            ((hi shl 8) or lo).toShort() / 32768f
        }
    }
}
