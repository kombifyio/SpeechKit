// Wire protocol for the realtime Voice Agent WebSocket
// (GET /v1/voiceagent/sessions/{id}/ws).
//
// Kotlin mirror of the contract published in docs/server/asyncapi.v1.yaml and
// of the TypeScript SSOT in clients/typescript/packages/voiceagent-client/
// src/protocol.ts. Field names and semantics are pinned by
// VoiceAgentFrameCodecTest (consumer drift-check).
//
// Control frames are JSON text messages with a required "type" field. Audio
// is binary in BOTH directions on this surface — unlike the dictation stream,
// the server also sends the agent's spoken answer as binary PCM frames.
package io.kombify.speechkit.net

import com.squareup.moshi.Json
import com.squareup.moshi.JsonClass
import com.squareup.moshi.Moshi

/** Message types on the Voice Agent socket. */
object VoiceAgentMsg {
    // client → server
    const val START = "start"
    const val TEXT = "text"
    const val TOOL_RESPONSE = "tool_response"
    const val ADVANCE_STEP = "advance_step"
    const val AUDIO_END = "audio_end"
    const val PING = "ping"
    const val STOP = "stop"

    // server → client
    const val STATE = "state"
    const val INPUT_TRANSCRIPT = "input_transcript"
    const val OUTPUT_TRANSCRIPT = "output_transcript"
    const val TOOL_CALL = "tool_call"
    const val SEQUENCE_STEP = "sequence_step"
    const val EVENT = "event"
    const val INTERRUPTED = "interrupted"
    const val ERROR = "error"
    const val SESSION_END = "session_end"
    const val PONG = "pong"
}

/**
 * Agent lifecycle states carried on [VoiceAgentStateFrame]. These are the
 * session's own states, distinct from the eight orb visual states — the UI
 * maps them, it does not receive them.
 */
object VoiceAgentStates {
    const val CONNECTING = "connecting"
    const val LISTENING = "listening"
    const val PROCESSING = "processing"
    const val SPEAKING = "speaking"
    const val RECOVERING = "recovering"
    const val DEACTIVATING = "deactivating"
    const val INACTIVE = "inactive"
}

/**
 * Error codes on [VoiceAgentErrorFrame] a client has to tell apart. Only the
 * ones a surface renders differently belong here — everything else is prose
 * the user reads as written.
 */
object VoiceAgentErrorCodes {
    /**
     * The requested realtime backend is unknown to this server, or is
     * configured without credentials. There is no discovery endpoint listing
     * what a deployment registered, so a client that offers a provider choice
     * only learns the answer by asking for one and being refused.
     */
    const val PROVIDER_UNAVAILABLE = "provider_unavailable"
}

/** Reasons the server reports on [VoiceAgentSessionEndFrame]. */
object VoiceAgentEndReasons {
    const val IDLE = "idle"
    const val GO_AWAY = "go_away"
    const val CLIENT = "client"
    const val ERROR = "error"
    const val SHUTDOWN = "shutdown"
    const val MAX_DURATION = "max_duration"
}

/**
 * Mandatory first client frame. Every field is optional: the server falls
 * back to its configured defaults, and an unknown provider is rejected at
 * start with a provider_unavailable error rather than silently substituted.
 */
@JsonClass(generateAdapter = true)
data class VoiceAgentStartFrame(
    val type: String = VoiceAgentMsg.START,
    @Json(name = "persona_id") val personaId: String? = null,
    @Json(name = "role_id") val roleId: String? = null,
    @Json(name = "sequence_id") val sequenceId: String? = null,
    val provider: String? = null,
    @Json(name = "media_transport") val mediaTransport: String? = null,
    val voice: String? = null,
    val locale: String? = null,
    val model: String? = null,
    val thinking: String? = null,
    @Json(name = "system_prompt_override") val systemPromptOverride: String? = null,
) : VoiceAgentClientFrame

/** Marker for frames the client sends. */
sealed interface VoiceAgentClientFrame

/** Injects a typed turn into a live voice session. */
@JsonClass(generateAdapter = true)
data class VoiceAgentTextFrame(
    val type: String = VoiceAgentMsg.TEXT,
    val text: String = "",
) : VoiceAgentClientFrame

/** Answers a [VoiceAgentToolCallFrame]. */
@JsonClass(generateAdapter = true)
data class VoiceAgentToolResponseFrame(
    val type: String = VoiceAgentMsg.TOOL_RESPONSE,
    val id: String = "",
    val name: String = "",
    val response: Map<String, Any?> = emptyMap(),
) : VoiceAgentClientFrame

/** Body-less client control frames: audio_end, ping, stop. */
@JsonClass(generateAdapter = true)
data class VoiceAgentControlFrame(val type: String) : VoiceAgentClientFrame

/** Marker for decoded server → client frames. */
sealed interface VoiceAgentServerFrame

/** Lifecycle transition; drives the caller's UI state. */
@JsonClass(generateAdapter = true)
data class VoiceAgentStateFrame(
    val type: String = VoiceAgentMsg.STATE,
    val state: String = "",
    @Json(name = "event_type") val eventType: String? = null,
) : VoiceAgentServerFrame

/**
 * Streaming transcript for either side of the conversation.
 *
 * Granularity is provider-dependent within v1: some backends send the
 * CUMULATIVE turn text on every frame, others send deltas. Callers that need
 * cumulative text must normalise with [accumulateVoiceAgentTranscript]
 * instead of assuming one form.
 */
@JsonClass(generateAdapter = true)
data class VoiceAgentTranscriptFrame(
    val type: String = VoiceAgentMsg.INPUT_TRANSCRIPT,
    val text: String = "",
    val done: Boolean = false,
    @Json(name = "event_type") val eventType: String? = null,
) : VoiceAgentServerFrame {
    /** True for the user's own speech, false for the agent's answer. */
    val isInput: Boolean get() = type == VoiceAgentMsg.INPUT_TRANSCRIPT
}

/** The agent asks the host to run a tool. */
@JsonClass(generateAdapter = true)
data class VoiceAgentToolCallFrame(
    val type: String = VoiceAgentMsg.TOOL_CALL,
    val id: String = "",
    val name: String = "",
    val args: Map<String, Any?>? = null,
) : VoiceAgentServerFrame

/** Progress through a configured agent sequence. */
@JsonClass(generateAdapter = true)
data class VoiceAgentSequenceStepFrame(
    val type: String = VoiceAgentMsg.SEQUENCE_STEP,
    @Json(name = "sequence_id") val sequenceId: String? = null,
    @Json(name = "step_id") val stepId: String = "",
    @Json(name = "step_index") val stepIndex: Int? = null,
    val status: String = "",
    val reason: String? = null,
) : VoiceAgentServerFrame

/** Provider event without a more specific v1 mapping. */
@JsonClass(generateAdapter = true)
data class VoiceAgentEventFrame(
    val type: String = VoiceAgentMsg.EVENT,
    @Json(name = "event_type") val eventType: String = "",
) : VoiceAgentServerFrame

/** Barge-in: the user spoke over the agent and its answer was cut. */
@JsonClass(generateAdapter = true)
data class VoiceAgentInterruptedFrame(
    val type: String = VoiceAgentMsg.INTERRUPTED,
    @Json(name = "event_type") val eventType: String? = null,
) : VoiceAgentServerFrame

/** Recoverable error; the socket stays open unless session_end follows. */
@JsonClass(generateAdapter = true)
data class VoiceAgentErrorFrame(
    val type: String = VoiceAgentMsg.ERROR,
    val code: String = "",
    val message: String = "",
    val remediation: String? = null,
    @Json(name = "request_id") val requestId: String? = null,
) : VoiceAgentServerFrame

/** Terminal; the server closes the socket after sending it. */
@JsonClass(generateAdapter = true)
data class VoiceAgentSessionEndFrame(
    val type: String = VoiceAgentMsg.SESSION_END,
    val reason: String = "",
) : VoiceAgentServerFrame

/** Answers a client ping. */
@JsonClass(generateAdapter = true)
data class VoiceAgentPongFrame(
    val type: String = VoiceAgentMsg.PONG,
) : VoiceAgentServerFrame

/** Forward-compat: unrecognised frame types are surfaced, never fatal. */
data class VoiceAgentUnknownFrame(val type: String) : VoiceAgentServerFrame

@JsonClass(generateAdapter = true)
internal data class VoiceAgentEnvelope(val type: String = "")

/**
 * Normalises streaming transcript text to the cumulative form regardless of
 * whether the provider streams cumulative snapshots or deltas. Mirrors
 * accumulateTranscript() in the TypeScript client so both clients render the
 * same turn text from the same stream. Reset to "" after a done frame.
 */
fun accumulateVoiceAgentTranscript(previous: String, next: String): String = when {
    previous.isEmpty() -> next
    next.isEmpty() -> previous
    next.startsWith(previous) -> next // cumulative stream
    previous.endsWith(next) -> previous // duplicate tail
    else -> previous + next // delta stream
}

/**
 * JSON codec for the Voice Agent frames. Unknown JSON keys are ignored
 * (Moshi default) and unknown frame types decode to [VoiceAgentUnknownFrame],
 * so the contract may grow additively without breaking deployed clients.
 */
class VoiceAgentCodec(moshi: Moshi = defaultMoshi()) {

    private val envelopeAdapter = moshi.adapter(VoiceAgentEnvelope::class.java)
    private val startAdapter = moshi.adapter(VoiceAgentStartFrame::class.java)
    private val textAdapter = moshi.adapter(VoiceAgentTextFrame::class.java)
    private val toolResponseAdapter = moshi.adapter(VoiceAgentToolResponseFrame::class.java)
    private val controlAdapter = moshi.adapter(VoiceAgentControlFrame::class.java)
    private val stateAdapter = moshi.adapter(VoiceAgentStateFrame::class.java)
    private val transcriptAdapter = moshi.adapter(VoiceAgentTranscriptFrame::class.java)
    private val toolCallAdapter = moshi.adapter(VoiceAgentToolCallFrame::class.java)
    private val sequenceStepAdapter = moshi.adapter(VoiceAgentSequenceStepFrame::class.java)
    private val eventAdapter = moshi.adapter(VoiceAgentEventFrame::class.java)
    private val interruptedAdapter = moshi.adapter(VoiceAgentInterruptedFrame::class.java)
    private val errorAdapter = moshi.adapter(VoiceAgentErrorFrame::class.java)
    private val sessionEndAdapter = moshi.adapter(VoiceAgentSessionEndFrame::class.java)
    private val pongAdapter = moshi.adapter(VoiceAgentPongFrame::class.java)

    fun encodeStart(frame: VoiceAgentStartFrame): String =
        startAdapter.toJson(frame.copy(type = VoiceAgentMsg.START))

    fun encodeText(text: String): String =
        textAdapter.toJson(VoiceAgentTextFrame(text = text))

    fun encodeToolResponse(frame: VoiceAgentToolResponseFrame): String =
        toolResponseAdapter.toJson(frame.copy(type = VoiceAgentMsg.TOOL_RESPONSE))

    fun encodeControl(type: String): String =
        controlAdapter.toJson(VoiceAgentControlFrame(type))

    fun decodeServerFrame(json: String): VoiceAgentServerFrame {
        val type = runCatching { envelopeAdapter.fromJson(json)?.type }.getOrNull().orEmpty()
        return runCatching {
            when (type) {
                VoiceAgentMsg.STATE -> stateAdapter.fromJson(json)
                VoiceAgentMsg.INPUT_TRANSCRIPT, VoiceAgentMsg.OUTPUT_TRANSCRIPT ->
                    transcriptAdapter.fromJson(json)

                VoiceAgentMsg.TOOL_CALL -> toolCallAdapter.fromJson(json)
                VoiceAgentMsg.SEQUENCE_STEP -> sequenceStepAdapter.fromJson(json)
                VoiceAgentMsg.EVENT -> eventAdapter.fromJson(json)
                VoiceAgentMsg.INTERRUPTED -> interruptedAdapter.fromJson(json)
                VoiceAgentMsg.ERROR -> errorAdapter.fromJson(json)
                VoiceAgentMsg.SESSION_END -> sessionEndAdapter.fromJson(json)
                VoiceAgentMsg.PONG -> pongAdapter.fromJson(json)
                else -> null
            }
        }.getOrNull() ?: VoiceAgentUnknownFrame(type)
    }

    companion object {
        /** Matches the dictation codec: adapters are generated, no reflection. */
        fun defaultMoshi(): Moshi = Moshi.Builder().build()
    }
}
