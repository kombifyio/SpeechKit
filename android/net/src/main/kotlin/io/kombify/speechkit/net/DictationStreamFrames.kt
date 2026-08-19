// Wire protocol for the streaming Dictation WebSocket
// (GET /v1/dictation/stream/sessions/{id}/ws).
//
// Kotlin mirror of the Go SSOT internal/server/dictation/stream_protocol.go.
// Field names and semantics are pinned by the golden fixtures in
// docs/server/fixtures/dictation-stream.v1.json; DictationFrameCodecTest
// replays those fixtures against these codecs (consumer drift-check).
//
// Control frames are JSON text messages with a required "type" field. Audio
// frames are binary messages containing raw PCM (default 16 kHz S16 mono,
// client → server only). The first frame of every segment is "start"; after
// "segment_done" the client may send another "start" on the same socket.
package io.kombify.speechkit.net

import com.squareup.moshi.Json
import com.squareup.moshi.JsonClass
import com.squareup.moshi.Moshi

/** Message types on the dictation stream socket. */
object StreamMsg {
    // client → server
    const val START = "start"
    const val FINALIZE = "finalize"
    const val STOP = "stop"
    const val PING = "ping"

    // server → client
    const val READY = "ready"
    const val TRANSCRIPT = "transcript"
    const val SEGMENT_DONE = "segment_done"
    const val ERROR = "error"
    const val SESSION_END = "session_end"
    const val PONG = "pong"
}

/**
 * Stable error codes on [StreamErrorFrame]. Clients switch on these, never on
 * the human-readable message.
 */
object StreamErrorCodes {
    const val START_REQUIRED = "start_required"
    const val SEGMENT_ACTIVE = "segment_active"
    const val NO_ACTIVE_SEGMENT = "no_active_segment"
    const val STREAMING_UNAVAILABLE = "streaming_unavailable"
    const val PROVIDER_STREAM_FAILED = "provider_stream_failed"
    const val PROVIDER_STREAM_CLOSED = "provider_stream_closed"
    const val FORMAT_UNSUPPORTED = "format_unsupported"
    const val INVALID_FRAME = "invalid_frame"
}

/** Session end reasons carried on [StreamSessionEndFrame]. */
object StreamEndReasons {
    const val CLIENT = "client"
    const val IDLE = "idle"
    const val MAX_DURATION = "max_duration"
    const val MAX_AUDIO = "max_audio"
    const val ERROR = "error"
    const val SHUTDOWN = "shutdown"
}

/**
 * PCM description on the start frame. Nulls normalize server-side to the
 * canonical 16 kHz S16 mono.
 */
@JsonClass(generateAdapter = true)
data class StreamAudioFormat(
    val encoding: String? = null, // "linear16" (default) | "pcm16"
    @Json(name = "sample_rate_hz") val sampleRateHz: Int? = null, // default 16000
    val channels: Int? = null, // default 1
)

/**
 * Mandatory first client frame of every segment. Optional fields stay null so
 * Moshi omits them, matching the Go struct's `omitempty` wire shape.
 */
@JsonClass(generateAdapter = true)
data class StreamStartFrame(
    val type: String = StreamMsg.START,
    val language: String? = null,
    val model: String? = null,
    @Json(name = "provider_profile_id") val providerProfileId: String? = null,
    /** Null means server default (true) — live partials are the point. */
    @Json(name = "interim_results") val interimResults: Boolean? = null,
    @Json(name = "endpointing_ms") val endpointingMs: Int? = null,
    @Json(name = "turn_detection") val turnDetection: Boolean? = null,
    val keyterms: List<String>? = null,
    @Json(name = "prompt_hint") val promptHint: String? = null,
    val diarization: Boolean? = null,
    val format: StreamAudioFormat? = null,
)

/** Body-less client control frames: finalize, stop, ping. */
@JsonClass(generateAdapter = true)
data class StreamControlFrame(val type: String)

/** Marker for decoded server → client frames. */
sealed interface ServerFrame

/** Acknowledges a start frame: binary PCM may flow. */
@JsonClass(generateAdapter = true)
data class StreamReadyFrame(
    val type: String = StreamMsg.READY,
    @Json(name = "segment_id") val segmentId: Long = 0,
) : ServerFrame

/** Word-level confidence entry on transcript frames. */
@JsonClass(generateAdapter = true)
data class StreamWord(
    val text: String,
    val confidence: Double? = null,
    @Json(name = "start_ms") val startMs: Long? = null,
    @Json(name = "end_ms") val endMs: Long? = null,
)

/** Draft (done=false) or final (done=true) transcript for the active segment. */
@JsonClass(generateAdapter = true)
data class StreamTranscriptFrame(
    val type: String = StreamMsg.TRANSCRIPT,
    @Json(name = "segment_id") val segmentId: Long = 0,
    val sequence: Long? = null,
    val text: String = "",
    val done: Boolean = false,
    val language: String? = null,
    val confidence: Double? = null,
    val provider: String? = null,
    val model: String? = null,
    val words: List<StreamWord>? = null,
    /** Opaque diarization payload; typed consumers land with diarization UI. */
    val speakers: Map<String, Any?>? = null,
) : ServerFrame

/** Segment flushed; the client may send the next "start". */
@JsonClass(generateAdapter = true)
data class StreamSegmentDoneFrame(
    val type: String = StreamMsg.SEGMENT_DONE,
    @Json(name = "segment_id") val segmentId: Long = 0,
) : ServerFrame

/** Recoverable protocol or provider error; socket stays open unless session_end follows. */
@JsonClass(generateAdapter = true)
data class StreamErrorFrame(
    val type: String = StreamMsg.ERROR,
    val code: String = "",
    val message: String = "",
) : ServerFrame

/** Terminal; the server closes the socket after sending it. */
@JsonClass(generateAdapter = true)
data class StreamSessionEndFrame(
    val type: String = StreamMsg.SESSION_END,
    val reason: String = "",
) : ServerFrame

/** Answers a client ping. */
@JsonClass(generateAdapter = true)
data class StreamPongFrame(val type: String = StreamMsg.PONG) : ServerFrame

/** Forward-compat: unrecognized frame types are surfaced, never crash. */
data class UnknownFrame(val type: String) : ServerFrame

@JsonClass(generateAdapter = true)
internal data class StreamEnvelope(val type: String = "")

/**
 * JSON codec for the dictation stream frames. Unknown JSON keys are ignored
 * (Moshi default), unknown frame types decode to [UnknownFrame] — the
 * contract may grow additively without breaking deployed clients.
 */
class DictationStreamCodec(moshi: Moshi = defaultMoshi()) {

    private val envelopeAdapter = moshi.adapter(StreamEnvelope::class.java)
    private val startAdapter = moshi.adapter(StreamStartFrame::class.java)
    private val controlAdapter = moshi.adapter(StreamControlFrame::class.java)
    private val readyAdapter = moshi.adapter(StreamReadyFrame::class.java)
    private val transcriptAdapter = moshi.adapter(StreamTranscriptFrame::class.java)
    private val segmentDoneAdapter = moshi.adapter(StreamSegmentDoneFrame::class.java)
    private val errorAdapter = moshi.adapter(StreamErrorFrame::class.java)
    private val sessionEndAdapter = moshi.adapter(StreamSessionEndFrame::class.java)
    private val pongAdapter = moshi.adapter(StreamPongFrame::class.java)

    fun encodeStart(frame: StreamStartFrame): String =
        startAdapter.toJson(frame.copy(type = StreamMsg.START))

    fun encodeControl(type: String): String =
        controlAdapter.toJson(StreamControlFrame(type))

    fun decodeStart(json: String): StreamStartFrame? = startAdapter.fromJson(json)

    fun decodeServerFrame(json: String): ServerFrame {
        val type = runCatching { envelopeAdapter.fromJson(json)?.type }.getOrNull().orEmpty()
        return runCatching {
            when (type) {
                StreamMsg.READY -> readyAdapter.fromJson(json)
                StreamMsg.TRANSCRIPT -> transcriptAdapter.fromJson(json)
                StreamMsg.SEGMENT_DONE -> segmentDoneAdapter.fromJson(json)
                StreamMsg.ERROR -> errorAdapter.fromJson(json)
                StreamMsg.SESSION_END -> sessionEndAdapter.fromJson(json)
                StreamMsg.PONG -> pongAdapter.fromJson(json)
                else -> null
            }
        }.getOrNull() ?: UnknownFrame(type)
    }

    companion object {
        fun defaultMoshi(): Moshi = Moshi.Builder().build()
    }
}
