package io.kombify.speechkit.assistant.service

import io.kombify.speechkit.audio.AudioCapture
import io.kombify.speechkit.audio.MicLevelMeter
import io.kombify.speechkit.audio.frameDurationMillis
import io.kombify.speechkit.audio.toPcm16Samples
import io.kombify.speechkit.log.VoiceLog
import io.kombify.speechkit.stt.streaming.DictationSegmentOptions
import io.kombify.speechkit.stt.streaming.StreamingSttSession
import io.kombify.speechkit.stt.streaming.TranscriptEvent
import io.kombify.speechkit.vad.LevelVadDetector
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.flow.collect
import kotlinx.coroutines.flow.onEach
import kotlinx.coroutines.flow.takeWhile
import kotlinx.coroutines.launch
import kotlinx.coroutines.withTimeoutOrNull

data class UtteranceResult(
    val text: String,
    val reason: Reason,
    val detail: String? = null,
) {
    enum class Reason {
        HEARD,
        EMPTY_FINAL,
        NO_SPEECH,
        TIMEOUT,
        CLOSED,
        STREAM_FAILED,
    }
}

/**
 * One spoken turn for the system assistant, using the same
 * [StreamingSttSession] factory as the keyboard. On-device sessions own the
 * microphone (`capturesOwnAudio`); server sessions are fed by [audioCapture]
 * until the shared level VAD endpoints.
 */
class UtteranceTranscriber(
    private val sessionFactory: suspend () -> StreamingSttSession,
    private val audioCapture: AudioCapture,
    private val onLevel: (Float) -> Unit = {},
) {
    suspend fun transcribe(): UtteranceResult = coroutineScope {
        val session = sessionFactory()
        try {
            val result = CompletableDeferred<UtteranceResult>()
            val events = launch {
                session.events.collect { event ->
                    when (event) {
                        is TranscriptEvent.Final ->
                            if (!result.isCompleted) {
                                val reason = if (event.text.isBlank()) {
                                    UtteranceResult.Reason.EMPTY_FINAL
                                } else {
                                    UtteranceResult.Reason.HEARD
                                }
                                result.complete(UtteranceResult(event.text, reason))
                            }
                        is TranscriptEvent.Failure ->
                            if (!result.isCompleted) {
                                result.complete(
                                    UtteranceResult(
                                        text = "",
                                        reason = UtteranceResult.Reason.STREAM_FAILED,
                                        detail = event.code,
                                    ),
                                )
                            }
                        is TranscriptEvent.Closed ->
                            if (!result.isCompleted) {
                                result.complete(
                                    UtteranceResult(
                                        text = "",
                                        reason = UtteranceResult.Reason.CLOSED,
                                        detail = event.reason,
                                    ),
                                )
                            }
                        else -> Unit
                    }
                }
            }
            session.startSegment(DictationSegmentOptions())
            VoiceLog.i(
                VoiceLog.ASSIST,
                "segment started capturesOwnAudio=${session.capturesOwnAudio}",
            )
            if (!session.capturesOwnAudio) {
                val capture = captureUntilEndpoint(session)
                if (capture.timedOut && !capture.sawSpeech) {
                    events.cancel()
                    VoiceLog.w(
                        VoiceLog.ASSIST,
                        "no speech in ${MAX_CAPTURE_MILLIS}ms bytes=${capture.bytes}",
                    )
                    return@coroutineScope UtteranceResult("", UtteranceResult.Reason.NO_SPEECH)
                }
                session.finishSegment()
            }
            val outcome = withTimeoutOrNull(MAX_RESULT_MILLIS) { result.await() }
                ?: UtteranceResult("", UtteranceResult.Reason.TIMEOUT)
            events.cancel()
            VoiceLog.i(
                VoiceLog.ASSIST,
                "utterance reason=${outcome.reason} chars=${outcome.text.length} detail=${outcome.detail ?: "-"}",
            )
            outcome
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            VoiceLog.e(VoiceLog.ASSIST, "utterance failed", e)
            throw e
        } finally {
            runCatching { session.close() }
        }
    }

    private suspend fun captureUntilEndpoint(session: StreamingSttSession): CaptureEnd {
        val meter = MicLevelMeter()
        val vad = LevelVadDetector()
        var lastFrameAt = System.nanoTime()
        var lastPublishAt = 0L
        var speechMillis = 0L
        var silenceMillis = 0L
        var sawSpeech = false
        var endpointReached = false
        var bytes = 0

        val timedOut = withTimeoutOrNull(MAX_CAPTURE_MILLIS) {
            audioCapture.frames()
                .onEach { frame ->
                    bytes += frame.size
                    session.sendAudio(frame)
                    val now = System.nanoTime()
                    val elapsedMillis = (now - lastFrameAt) / 1_000_000
                    lastFrameAt = now
                    val level = meter.accept(frame, elapsedMillis)
                    if ((now - lastPublishAt) / 1_000_000 >= LEVEL_PUBLISH_INTERVAL_MS) {
                        lastPublishAt = now
                        onLevel(level)
                    }
                    val frameMillis = frame.frameDurationMillis()
                    if (vad.processFrame(frame.toPcm16Samples()) >= SPEECH_PROBABILITY) {
                        speechMillis += frameMillis
                        silenceMillis = 0
                        if (speechMillis >= MIN_SPEECH_MILLIS) sawSpeech = true
                    } else {
                        silenceMillis += frameMillis
                        if (sawSpeech && silenceMillis >= MIN_SILENCE_MILLIS) {
                            endpointReached = true
                        }
                    }
                }
                .takeWhile { !endpointReached }
                .collect()
            false
        } ?: true

        return CaptureEnd(sawSpeech = sawSpeech, timedOut = timedOut, bytes = bytes)
    }

    private data class CaptureEnd(
        val sawSpeech: Boolean,
        val timedOut: Boolean,
        val bytes: Int,
    )

    private companion object {
        const val LEVEL_PUBLISH_INTERVAL_MS = 50L
        const val SPEECH_PROBABILITY = 0.5f
        const val MIN_SILENCE_MILLIS = 700L
        const val MIN_SPEECH_MILLIS = 250L
        const val MAX_CAPTURE_MILLIS = 30_000L
        const val MAX_RESULT_MILLIS = 15_000L
    }
}
