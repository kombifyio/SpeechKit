package io.kombify.speechkit.net

import io.kombify.speechkit.stt.streaming.DictationSegmentOptions
import io.kombify.speechkit.stt.streaming.StreamingSttSession
import io.kombify.speechkit.stt.streaming.TranscriptEvent
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flow
import kotlinx.coroutines.flow.takeWhile
import java.io.ByteArrayOutputStream

/**
 * Decorates a streaming [StreamingSttSession] with a one-shot batch rescue:
 * when the stream dies mid-segment (the transport-level `ws_failure` the
 * `DictationWsClient` emits before `Closed`), the segment's buffered PCM is
 * retried once through a batch session (`POST /v1/dictation/transcribe`) and
 * a [TranscriptEvent.Final] is emitted in place of the failure. This is the
 * documented "1 retry -> batch POST" UX the Voice IME (B-M2) builds on:
 * a dropped WebSocket costs latency, not the user's sentence.
 *
 * Buffering semantics:
 * - PCM is buffered per segment from `startSegment` on, capped at
 *   [maxBufferBytes] (~2 min at 16 kHz S16 mono by default). On overflow the
 *   rescue is disarmed for that segment — a partial retry would silently
 *   truncate the dictation.
 * - Every streamed [TranscriptEvent.Final] clears the buffer: that audio is
 *   already committed downstream, so a later rescue only re-transcribes the
 *   uncommitted tail instead of duplicating text.
 * - The rescue Final is re-mapped onto the failing stream's segment id so
 *   consumer correlation (composing region per segment) keeps working.
 *
 * On a successful rescue the original Failure is swallowed and replaced by
 * `Final` + `SegmentDone`; the delegate's trailing `Closed` still flows so
 * consumers release the dead session. If the batch retry fails too, the
 * original Failure is forwarded unchanged.
 */
class RetryingDictationSession(
    private val delegate: StreamingSttSession,
    private val batchSessionFactory: suspend () -> StreamingSttSession,
    private val retryableCodes: Set<String> = DEFAULT_RETRYABLE_CODES,
    private val maxBufferBytes: Int = DEFAULT_MAX_BUFFER_BYTES,
) : StreamingSttSession {

    override val capturesOwnAudio: Boolean get() = delegate.capturesOwnAudio

    private val buffer = ByteArrayOutputStream()

    @Volatile
    private var options = DictationSegmentOptions()

    @Volatile
    private var segmentActive = false

    @Volatile
    private var bufferOverflow = false

    @Volatile
    private var retriedThisSegment = false

    /** Last segment id the server acknowledged; rescue Finals re-use it. */
    @Volatile
    private var currentSegmentId = 0L

    override val events: Flow<TranscriptEvent> = flow {
        delegate.events.collect { event ->
            when (event) {
                is TranscriptEvent.SegmentReady -> {
                    currentSegmentId = event.segmentId
                    emit(event)
                }

                is TranscriptEvent.Draft -> {
                    currentSegmentId = event.segmentId
                    emit(event)
                }

                is TranscriptEvent.Final -> {
                    currentSegmentId = event.segmentId
                    // This audio is committed downstream now — a later rescue
                    // must not transcribe it again.
                    synchronized(buffer) { buffer.reset() }
                    emit(event)
                }

                is TranscriptEvent.SegmentDone -> {
                    segmentActive = false
                    synchronized(buffer) { buffer.reset() }
                    emit(event)
                }

                is TranscriptEvent.Failure -> {
                    val rescued = if (shouldRetry(event.code)) runBatchRescue() else null
                    if (rescued != null) {
                        segmentActive = false
                        emit(rescued)
                        emit(TranscriptEvent.SegmentDone(rescued.segmentId))
                    } else {
                        emit(event)
                    }
                }

                is TranscriptEvent.Closed -> emit(event)
            }
        }
    }

    override suspend fun startSegment(options: DictationSegmentOptions) {
        this.options = options
        synchronized(buffer) { buffer.reset() }
        bufferOverflow = false
        retriedThisSegment = false
        segmentActive = true
        delegate.startSegment(options)
    }

    override suspend fun sendAudio(pcm: ByteArray) {
        if (segmentActive) {
            synchronized(buffer) {
                if (buffer.size() + pcm.size <= maxBufferBytes) {
                    buffer.write(pcm)
                } else {
                    bufferOverflow = true
                }
            }
        }
        delegate.sendAudio(pcm)
    }

    override suspend fun finishSegment() {
        // The buffer intentionally survives until SegmentDone: a failure
        // during finalize is exactly the window the rescue exists for.
        delegate.finishSegment()
    }

    override suspend fun keepAlive(): Boolean = delegate.keepAlive()

    override suspend fun close() = delegate.close()

    override fun toString(): String = "RetryingDictationSession($delegate)"

    private fun shouldRetry(code: String): Boolean =
        segmentActive &&
            !retriedThisSegment &&
            !bufferOverflow &&
            code in retryableCodes &&
            synchronized(buffer) { buffer.size() } > 0

    /** Runs the buffered audio through one batch session. Null = no rescue. */
    private suspend fun runBatchRescue(): TranscriptEvent.Final? {
        retriedThisSegment = true
        val pcm = synchronized(buffer) {
            val snapshot = buffer.toByteArray()
            buffer.reset()
            snapshot
        }
        if (pcm.isEmpty()) return null

        return runCatching {
            val batch = batchSessionFactory()
            try {
                batch.startSegment(options)
                batch.sendAudio(pcm)
                batch.finishSegment()
                // BatchDictationSession buffers its events in an unbounded
                // channel, so after finishSegment the terminal events are
                // already queued; SegmentDone bounds the read.
                var rescued: TranscriptEvent.Final? = null
                batch.events
                    .takeWhile { it !is TranscriptEvent.SegmentDone }
                    .collect { event ->
                        if (event is TranscriptEvent.Final) rescued = event
                    }
                rescued?.copy(segmentId = currentSegmentId)
            } finally {
                runCatching { batch.close() }
            }
        }.getOrNull()
    }

    companion object {
        /**
         * Transport-level stream loss only. Server-side error frames (e.g.
         * `provider_stream_failed`) keep the socket open and own their
         * segment lifecycle — double-transcribing on those risks committing
         * the same audio twice.
         */
        val DEFAULT_RETRYABLE_CODES: Set<String> = setOf("ws_failure")

        /** ~2 minutes at 16 kHz S16 mono — far beyond a typical IME segment. */
        const val DEFAULT_MAX_BUFFER_BYTES: Int = 16_000 * 2 * 120
    }
}
