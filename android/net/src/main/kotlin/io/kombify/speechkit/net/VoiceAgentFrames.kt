// Wire protocol for the realtime Voice Agent WebSocket
// (GET /v1/voiceagent/sessions/{id}/ws).
//
// Kotlin mirror of the Voice Agent wire contract. Source of truth: the Go
// structs in internal/server/voiceagent/protocol.go (the producer).
// docs/server/fixtures/voiceagent.v1.json is the interchange artifact all
// consumers verify against — VoiceAgentFrameCodecTest replays it here
// (consumer drift-check); docs/server/asyncapi.v1.yaml documents the channel.
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

    /**
     * Tap-to-interrupt: stops the CURRENT agent reply's downlink audio.
     * Idempotent and safe while nothing plays; the server acknowledges every
     * cancel with an [VoiceAgentInterruptedFrame] so playback state converges.
     */
    const val CANCEL = "cancel"

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
 * PCM rates on this socket, mirroring `CLIENT_SAMPLE_RATE` /
 * `SERVER_SAMPLE_RATE` in protocol.ts. Both directions are S16 LE mono.
 *
 * The two directions are deliberately named apart because they are **not the
 * same rate**: the client uploads at the capture rate and the server answers
 * half again as fast. Anything that plays [VoiceAgentEvent.Audio], or measures
 * how long it stays audible, has to be told [SERVER_SAMPLE_RATE] — reaching
 * for the capture rate instead stretches the agent's voice to 1.5x its length
 * and drops it about a fifth in pitch.
 */
object VoiceAgentAudio {
    /** Rate the client must send microphone PCM at: 16 kHz S16 LE mono. */
    const val CLIENT_SAMPLE_RATE = 16_000

    /** Rate the server sends the agent's answer at: 24 kHz S16 LE mono. */
    const val SERVER_SAMPLE_RATE = 24_000
}

/**
 * Turn-taking policy override for one session. Omitted entirely, the server
 * applies the provider's own defaults.
 */
@JsonClass(generateAdapter = true)
data class VoiceAgentActivityDetection(
    val automatic: Boolean = false,
    @Json(name = "start_sensitivity") val startSensitivity: String? = null,
    @Json(name = "end_sensitivity") val endSensitivity: String? = null,
    @Json(name = "prefix_padding_ms") val prefixPaddingMs: Int? = null,
    @Json(name = "silence_duration_ms") val silenceDurationMs: Int? = null,
    @Json(name = "activity_handling") val activityHandling: String? = null,
    @Json(name = "turn_coverage") val turnCoverage: String? = null,
)

/**
 * Diarization and identification options for one session. Unlike the frame
 * fields around it these keys are camelCase on the wire, mirroring
 * `speaker.Options` on the server.
 */
@JsonClass(generateAdapter = true)
data class VoiceAgentSpeakerOptions(
    val enabled: Boolean? = null,
    val diarization: Boolean? = null,
    val identification: Boolean? = null,
    val attribution: Boolean? = null,
    val providerProfileId: String? = null,
    val model: String? = null,
    val diarizationModel: String? = null,
    val language: String? = null,
    val speakersExpected: Int? = null,
    val minSpeakersExpected: Int? = null,
    val maxSpeakersExpected: Int? = null,
    val speakerType: String? = null,
    val knownValues: List<String>? = null,
    val preferStreaming: Boolean? = null,
    val allowProviderMapping: Boolean? = null,
)

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
    @Json(name = "activity_detection") val activityDetection: VoiceAgentActivityDetection? = null,
    val speaker: VoiceAgentSpeakerOptions? = null,
    @Json(name = "system_prompt_override") val systemPromptOverride: String? = null,
) : VoiceAgentClientFrame

/**
 * Moves the server-side workflow runner from the active sequence step to the
 * next one. `stepId` is reserved for direct jumps; v1 advances linearly.
 */
@JsonClass(generateAdapter = true)
data class VoiceAgentAdvanceStepFrame(
    val type: String = VoiceAgentMsg.ADVANCE_STEP,
    @Json(name = "step_id") val stepId: String? = null,
    val reason: String? = null,
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

/**
 * Event metadata every server frame may carry (additive within v1).
 * `eventTypes` lists the SpeechKit-normalized meanings when one provider
 * event maps to several; `providerMetadata` exposes the provider-native
 * details for diagnostics.
 */
interface VoiceAgentFrameMeta {
    val eventType: String?
    val eventTypes: List<String>?
    val providerMetadata: Map<String, Any?>?
}

/** Lifecycle transition; drives the caller's UI state. */
@JsonClass(generateAdapter = true)
data class VoiceAgentStateFrame(
    val type: String = VoiceAgentMsg.STATE,
    val state: String = "",
    /**
     * Realtime backend actually serving this session, and the media
     * transport actually applied. Both arrive on the `session_ready` frame
     * only; a client gates provider-dependent controls on them.
     */
    val provider: String? = null,
    @Json(name = "media_transport") val mediaTransport: String? = null,
    @Json(name = "event_type") override val eventType: String? = null,
    @Json(name = "event_types") override val eventTypes: List<String>? = null,
    @Json(name = "provider_metadata") override val providerMetadata: Map<String, Any?>? = null,
) : VoiceAgentServerFrame, VoiceAgentFrameMeta

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
    /**
     * Speaker attribution for this text. Present only when the session
     * started with [VoiceAgentSpeakerOptions] and the provider returned one.
     */
    @Json(name = "speaker_label") val speakerLabel: String? = null,
    @Json(name = "person_id") val personId: String? = null,
    @Json(name = "display_name") val displayName: String? = null,
    @Json(name = "speaker_confidence") val speakerConfidence: Double? = null,
    @Json(name = "event_type") override val eventType: String? = null,
    @Json(name = "event_types") override val eventTypes: List<String>? = null,
    @Json(name = "provider_metadata") override val providerMetadata: Map<String, Any?>? = null,
) : VoiceAgentServerFrame, VoiceAgentFrameMeta {
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
    @Json(name = "event_type") override val eventType: String? = null,
    @Json(name = "event_types") override val eventTypes: List<String>? = null,
    @Json(name = "provider_metadata") override val providerMetadata: Map<String, Any?>? = null,
) : VoiceAgentServerFrame, VoiceAgentFrameMeta

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
    @Json(name = "event_type") override val eventType: String? = null,
    @Json(name = "event_types") override val eventTypes: List<String>? = null,
    @Json(name = "provider_metadata") override val providerMetadata: Map<String, Any?>? = null,
) : VoiceAgentServerFrame, VoiceAgentFrameMeta

/** Barge-in: the user spoke over the agent and its answer was cut. */
@JsonClass(generateAdapter = true)
data class VoiceAgentInterruptedFrame(
    val type: String = VoiceAgentMsg.INTERRUPTED,
    @Json(name = "event_type") override val eventType: String? = null,
    @Json(name = "event_types") override val eventTypes: List<String>? = null,
    @Json(name = "provider_metadata") override val providerMetadata: Map<String, Any?>? = null,
) : VoiceAgentServerFrame, VoiceAgentFrameMeta

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
    @Json(name = "event_type") override val eventType: String? = null,
    @Json(name = "event_types") override val eventTypes: List<String>? = null,
    @Json(name = "provider_metadata") override val providerMetadata: Map<String, Any?>? = null,
) : VoiceAgentServerFrame, VoiceAgentFrameMeta

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
    private val advanceStepAdapter = moshi.adapter(VoiceAgentAdvanceStepFrame::class.java)
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

    fun encodeAdvanceStep(frame: VoiceAgentAdvanceStepFrame): String =
        advanceStepAdapter.toJson(frame.copy(type = VoiceAgentMsg.ADVANCE_STEP))

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
