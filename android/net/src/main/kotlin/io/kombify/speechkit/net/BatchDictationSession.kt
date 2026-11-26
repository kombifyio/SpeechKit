package io.kombify.speechkit.net

import io.kombify.speechkit.stt.streaming.DictationSegmentOptions
import io.kombify.speechkit.stt.streaming.StreamingSttSession
import io.kombify.speechkit.stt.streaming.TranscriptEvent
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.receiveAsFlow
import java.io.ByteArrayOutputStream

/**
 * Batch fallback tier of [StreamingSttSession]: buffers segment PCM and posts
 * one WAV to POST /v1/dictation/transcribe on [finishSegment]. Used when the
 * server reports `capabilities.streaming=false` at session-create time, or as
 * the retry path after a failed stream. No drafts — one Final per segment.
 */
class BatchDictationSession private constructor(
    private val transcribe: suspend (ByteArray, DictationSegmentOptions) -> BatchTranscribeResponse,
    private val maxBufferBytes: Int = FirebaseAudioTranscriber.MAX_AUDIO_BYTES - 44,
) : StreamingSttSession {

    constructor(api: SpeechKitServerApi, sampleRateHz: Int = 16_000) : this({ pcm, options ->
        api.transcribe(WavEncoder.pcm16ToWav(pcm, sampleRateHz), language = options.language, model = options.model, prompt = options.promptHint, keyterms = options.keyterms)
    })

    companion object {
        fun withTranscriber(transcribe: suspend (ByteArray, DictationSegmentOptions) -> BatchTranscribeResponse): BatchDictationSession =
            BatchDictationSession({ pcm, options -> transcribe(WavEncoder.pcm16ToWav(pcm, 16_000), options) })

        /** Reuses one capture and one final-event lifecycle for local adapters without a WAV round trip. */
        fun withPcmTranscriber(maxBufferBytes: Int, transcribe: suspend (ByteArray, DictationSegmentOptions) -> BatchTranscribeResponse): BatchDictationSession =
            BatchDictationSession(transcribe, maxBufferBytes)
    }

    private val channel = Channel<TranscriptEvent>(Channel.UNLIMITED)
    private val buffer = ByteArrayOutputStream()
    private var options = DictationSegmentOptions()
    private var segmentId = 0L

    @Volatile
    private var active = false
    @Volatile private var closed = false
    @Volatile private var processing: Job? = null

    override val events: Flow<TranscriptEvent> = channel.receiveAsFlow()

    override suspend fun startSegment(options: DictationSegmentOptions) {
        check(!closed && processing == null) { "Audio session is unavailable" }
        this.options = options
        synchronized(buffer) { buffer.reset() }
        segmentId += 1
        active = true
        channel.trySend(TranscriptEvent.SegmentReady(segmentId))
    }

    override suspend fun sendAudio(pcm: ByteArray) {
        if (active) synchronized(buffer) {
            if (buffer.size() + pcm.size > maxBufferBytes) {
                active = false
                buffer.reset()
                channel.trySend(TranscriptEvent.Failure(code = "audio_too_long", message = "Audio recording is too long"))
                channel.trySend(TranscriptEvent.SegmentDone(segmentId))
                return
            }
            buffer.write(pcm)
        }
    }

    override suspend fun finishSegment(): Unit = coroutineScope {
        if (!active || closed) return@coroutineScope
        active = false
        val sid = segmentId
        val pcm = synchronized(buffer) {
            val snapshot = buffer.toByteArray()
            buffer.reset()
            snapshot
        }
        if (pcm.isEmpty()) {
            channel.trySend(TranscriptEvent.SegmentDone(sid))
            return@coroutineScope
        }
        processing = currentCoroutineContext()[Job]
        try {
        runCatching {
            currentCoroutineContext().ensureActive()
            check(!closed)
            transcribe(pcm, options)
        }
            .onSuccess { result ->
                currentCoroutineContext().ensureActive()
                if (closed || segmentId != sid) return@onSuccess
                channel.trySend(
                    TranscriptEvent.Final(
                        segmentId = sid,
                        text = result.text,
                        confidence = result.confidence,
                    ),
                )
            }
            .onFailure { error ->
                if (error is CancellationException) throw error
                if (closed || segmentId != sid) return@onFailure
                channel.trySend(
                    TranscriptEvent.Failure(
                        code = (error as? SpeechKitApiException)?.code ?: "batch_failed",
                        message = "Audio transcription failed",
                    ),
                )
            }
        // Contract: every started segment terminates with SegmentDone, on
        // the failure path too — consumers gate segment completion on it.
        if (!closed && segmentId == sid) channel.trySend(TranscriptEvent.SegmentDone(sid))
        } finally {
            pcm.fill(0)
            processing = null
        }
    }

    override suspend fun close() {
        closed = true
        active = false
        processing?.cancel()
        synchronized(buffer) { buffer.reset() }
        channel.trySend(TranscriptEvent.Closed(StreamEndReasons.CLIENT))
        channel.close()
    }
}
