package io.kombify.speechkit.stt.streaming

import kotlinx.coroutines.flow.Flow

/**
 * Transport-agnostic streaming dictation session, mirroring the Go kernel's
 * `DictationStream` (pkg/speechkit/dictation_streaming.go). One session spans
 * one connection; many sequential segments (one per mic press) ride on it.
 *
 * Implementations: system on-device tier (`stt/system`
 * SystemSpeechRecognizerSession, B-M2b), server WebSocket tier (`:net`
 * DictationWsClient), batch fallback (`:net` BatchDictationSession); BYOK
 * (B-M6) and local in-app model (B-M7) tiers implement the same contract
 * later.
 */
interface StreamingSttSession {
    /**
     * Ordered event stream for this session. Single-collector semantics:
     * collect from exactly one coroutine.
     */
    val events: Flow<TranscriptEvent>

    /**
     * True when the implementation records microphone audio itself instead of
     * consuming PCM pushed through [sendAudio] — e.g. the system
     * `SpeechRecognizer` tier, where the platform service owns the mic.
     *
     * Callers MUST skip their own capture pipeline for such sessions and
     * treat [sendAudio] as a no-op. Decorators must forward the delegate's
     * value rather than inherit this default. Android-only contract
     * extension: the Go kernel always receives pushed PCM.
     */
    val capturesOwnAudio: Boolean get() = false

    /** Begins a new segment. Only valid when no segment is active. */
    suspend fun startSegment(options: DictationSegmentOptions = DictationSegmentOptions())

    /**
     * Streams raw PCM (default 16 kHz S16 mono) for the active segment.
     * No-op when [capturesOwnAudio] is true.
     */
    suspend fun sendAudio(pcm: ByteArray)

    /**
     * Flushes the active segment: remaining transcripts arrive as events,
     * terminated by [TranscriptEvent.SegmentDone]. After that a new
     * [startSegment] may begin the next segment on the same session.
     */
    suspend fun finishSegment()

    /** Ends the session. The event flow terminates with [TranscriptEvent.Closed]. */
    suspend fun close()

    /**
     * Application-level keepalive. A warm connection held across mic presses
     * must ping periodically or the server's idle watchdog ends the session.
     *
     * Transport-level WebSocket pings do NOT reset that watchdog: the server
     * only resets it on frames its read loop returns, and a control-frame ping
     * is answered inside the WebSocket library and never surfaces there. Note
     * that OkHttp is already sending transport pings on this connection
     * (SpeechKitServerApi.defaultOkHttpClient), which keeps NAT paths warm and
     * detects half-open sockets but does nothing for the idle watchdog.
     *
     * Any application frame resets it, audio included — so a driver should
     * skip the ping while a segment is actively streaming rather than send
     * redundant traffic. [KeepAliveSession] implements exactly that.
     *
     * @return false when the transport has already rejected the ping, i.e.
     *   the socket is gone and a driver should stop pinging. Sessionless tiers
     *   have no watchdog and return true without doing anything.
     */
    suspend fun keepAlive(): Boolean = true
}

/**
 * Per-segment options, mirroring the server's `start` frame /
 * `speechkit.DictationStreamOptions`. Defaults ask for live partials.
 */
data class DictationSegmentOptions(
    val language: String? = null,
    val model: String? = null,
    val providerProfileId: String? = null,
    /** Live draft transcripts. True is the endpoint's reason to exist. */
    val interimResults: Boolean = true,
    /** Provider endpointing window in milliseconds (0 = provider default). */
    val endpointingMs: Int? = null,
    /** Domain terms boosted during recognition. */
    val keyterms: List<String> = emptyList(),
    /** Short situational context, e.g. "development context, German speaker". */
    val promptHint: String? = null,
)

/** Word-level confidence entry on final transcripts. */
data class TranscriptWord(
    val text: String,
    val confidence: Double? = null,
    val startMs: Long? = null,
    val endMs: Long? = null,
)

/** Events emitted by a [StreamingSttSession]. */
sealed interface TranscriptEvent {
    /** The provider stream is up; audio for this segment may flow. */
    data class SegmentReady(val segmentId: Long) : TranscriptEvent

    /** Live draft — update composing UI state, never commit. */
    data class Draft(
        val segmentId: Long,
        val text: String,
        val sequence: Long = 0,
    ) : TranscriptEvent

    /** Final transcript — safe to commit to the target text field. */
    data class Final(
        val segmentId: Long,
        val text: String,
        val sequence: Long = 0,
        val confidence: Double? = null,
        val words: List<TranscriptWord> = emptyList(),
    ) : TranscriptEvent

    /** Segment flushed; no further transcripts for this segmentId. */
    data class SegmentDone(val segmentId: Long) : TranscriptEvent

    /**
     * Recoverable error. The session may still accept a new segment; stable
     * [code] values mirror the server protocol (e.g. "streaming_unavailable",
     * "provider_stream_failed").
     */
    data class Failure(val code: String, val message: String) : TranscriptEvent

    /** Terminal: the session is gone. Reason mirrors the server's session_end. */
    data class Closed(val reason: String?) : TranscriptEvent
}
