package io.kombify.speechkit.net

import io.kombify.speechkit.stt.streaming.DictationSegmentOptions
import io.kombify.speechkit.stt.streaming.StreamingSttSession
import io.kombify.speechkit.stt.streaming.TranscriptEvent
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.receiveAsFlow
import java.io.ByteArrayOutputStream

/**
 * Batch fallback tier of [StreamingSttSession]: buffers segment PCM and posts
 * one WAV to POST /v1/dictation/transcribe on [finishSegment]. Used when the
 * server reports `capabilities.streaming=false` at session-create time, or as
 * the retry path after a failed stream. No drafts — one Final per segment.
 */
class BatchDictationSession(
    private val api: SpeechKitServerApi,
    private val sampleRateHz: Int = 16_000,
) : StreamingSttSession {

    private val channel = Channel<TranscriptEvent>(Channel.UNLIMITED)
    private val buffer = ByteArrayOutputStream()
    private var options = DictationSegmentOptions()
    private var segmentId = 0L

    @Volatile
    private var active = false

    override val events: Flow<TranscriptEvent> = channel.receiveAsFlow()

    override suspend fun startSegment(options: DictationSegmentOptions) {
        this.options = options
        synchronized(buffer) { buffer.reset() }
        segmentId += 1
        active = true
        channel.trySend(TranscriptEvent.SegmentReady(segmentId))
    }

    override suspend fun sendAudio(pcm: ByteArray) {
        if (active) synchronized(buffer) { buffer.write(pcm) }
    }

    override suspend fun finishSegment() {
        if (!active) return
        active = false
        val sid = segmentId
        val pcm = synchronized(buffer) {
            val snapshot = buffer.toByteArray()
            buffer.reset()
            snapshot
        }
        if (pcm.isEmpty()) {
            channel.trySend(TranscriptEvent.SegmentDone(sid))
            return
        }
        runCatching {
            api.transcribe(
                WavEncoder.pcm16ToWav(pcm, sampleRateHz),
                language = options.language,
                model = options.model,
                prompt = options.promptHint,
            )
        }
            .onSuccess { result ->
                channel.trySend(
                    TranscriptEvent.Final(
                        segmentId = sid,
                        text = result.text,
                        confidence = result.confidence,
                    ),
                )
            }
            .onFailure { error ->
                channel.trySend(
                    TranscriptEvent.Failure(
                        code = (error as? SpeechKitApiException)?.code ?: "batch_failed",
                        message = error.message ?: "batch transcription failed",
                    ),
                )
            }
        // Contract: every started segment terminates with SegmentDone, on
        // the failure path too — consumers gate segment completion on it.
        channel.trySend(TranscriptEvent.SegmentDone(sid))
    }

    override suspend fun close() {
        active = false
        channel.trySend(TranscriptEvent.Closed(StreamEndReasons.CLIENT))
        channel.close()
    }
}
