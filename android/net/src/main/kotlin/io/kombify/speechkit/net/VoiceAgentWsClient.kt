package io.kombify.speechkit.net

import io.kombify.speechkit.log.VoiceLog
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.receiveAsFlow
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import okio.ByteString
import okio.ByteString.Companion.toByteString

/**
 * What a Voice Agent session reports to its host, normalised away from the
 * wire shape. Hosts render these; they never parse frames themselves.
 */
sealed interface VoiceAgentEvent {
    /** Lifecycle transition (see [VoiceAgentStates]). */
    data class State(val state: String) : VoiceAgentEvent

    /**
     * Transcript text for one side of the conversation. [text] is already
     * accumulated to the cumulative form, so hosts can render it directly.
     */
    data class Transcript(
        val input: Boolean,
        val text: String,
        val done: Boolean,
    ) : VoiceAgentEvent

    /**
     * Agent audio to play back: S16 LE mono PCM at
     * [VoiceAgentAudio.SERVER_SAMPLE_RATE], which is **not** the rate the
     * microphone captures at. A host that hands these bytes to a player or a
     * duration calculation states that rate explicitly.
     */
    data class Audio(val pcm: ByteArray) : VoiceAgentEvent {
        // Value semantics on a ByteArray field need explicit equals/hashCode;
        // the generated ones compare references and would report two
        // identical chunks as different.
        override fun equals(other: Any?): Boolean =
            this === other || (other is Audio && pcm.contentEquals(other.pcm))

        override fun hashCode(): Int = pcm.contentHashCode()
    }

    /** The user spoke over the agent; its answer was cut. */
    data object Interrupted : VoiceAgentEvent

    /** The agent asks the host to run a tool. */
    data class ToolCall(
        val id: String,
        val name: String,
        val args: Map<String, Any?>,
    ) : VoiceAgentEvent

    /** Recoverable error; the session may continue. */
    data class Failure(
        val code: String,
        val message: String,
        val remediation: String? = null,
    ) : VoiceAgentEvent

    /** Terminal. No further events follow. */
    data class Closed(val reason: String) : VoiceAgentEvent
}

/** A live Voice Agent conversation. */
interface VoiceAgentSession {
    val events: Flow<VoiceAgentEvent>

    /** Opens the conversation. Must be the first call. */
    suspend fun start(options: VoiceAgentStartFrame)

    /** Streams captured microphone PCM to the agent. */
    suspend fun sendAudio(pcm: ByteArray)

    /**
     * Signals the end of the user's turn without ending the session — the
     * hold-to-talk release. The agent answers, then listens again.
     */
    suspend fun endTurn()

    /** Injects a typed turn instead of speech. */
    suspend fun sendText(text: String)

    /** Answers a [VoiceAgentEvent.ToolCall]. */
    suspend fun respondToTool(id: String, name: String, response: Map<String, Any?>)

    /**
     * Tap-to-interrupt: stops the agent reply that is playing right now. Safe
     * to call while nothing plays. The server always answers with
     * [VoiceAgentEvent.Interrupted], so drop queued agent audio when that
     * event arrives rather than here.
     */
    suspend fun cancelReply()

    /**
     * Moves a running sequence to its next step. Only meaningful for
     * sessions started with a `sequenceId`.
     */
    suspend fun advanceStep(reason: String? = null)

    /** Keepalive. Returns false once the socket is closed or closing. */
    suspend fun keepAlive(): Boolean

    /** Ends the conversation. */
    suspend fun close()
}

/**
 * Server tier of the realtime Voice Agent: one ticket-authenticated WebSocket
 * to /v1/voiceagent/sessions/{id}/ws.
 *
 * The ticket rides in the `Sec-WebSocket-Protocol` header ("ticket.<value>"),
 * mirroring internal/server/wssession — never in the URL, so it stays out of
 * proxy access logs.
 *
 * Unlike the dictation stream, audio is bidirectional here: the client sends
 * microphone PCM and the server sends the agent's spoken answer back as
 * binary frames, surfaced as [VoiceAgentEvent.Audio].
 */
class VoiceAgentWsClient(
    private val client: OkHttpClient = SpeechKitServerApi.defaultOkHttpClient(),
    private val codec: VoiceAgentCodec = VoiceAgentCodec(),
) {

    /**
     * Opens the WebSocket for a minted session. Failures surface as
     * [VoiceAgentEvent.Failure] + [VoiceAgentEvent.Closed] on the event flow,
     * not as exceptions — a dropped conversation is a UI state, not a crash.
     */
    fun connect(session: CreateVoiceAgentSessionResponse): VoiceAgentSession {
        val events = Channel<VoiceAgentEvent>(Channel.UNLIMITED)

        val requestBuilder = Request.Builder().url(DictationWsClient.httpUrlFor(session.wsUrl))
        val subprotocol = session.wsSubprotocol?.takeIf { it.isNotBlank() }
            ?: session.ticket.takeIf { it.isNotBlank() }
                ?.let { "${DictationWsClient.TICKET_SUBPROTOCOL_PREFIX}$it" }
        subprotocol?.let { requestBuilder.header("Sec-WebSocket-Protocol", it) }

        // Transcript accumulation is per side: the provider may stream either
        // cumulative snapshots or deltas, and the two sides interleave.
        var inputText = ""
        var outputText = ""

        val listener = object : WebSocketListener() {
            override fun onMessage(webSocket: WebSocket, text: String) {
                when (val frame = codec.decodeServerFrame(text)) {
                    is VoiceAgentStateFrame ->
                        events.trySend(VoiceAgentEvent.State(frame.state))

                    is VoiceAgentTranscriptFrame -> {
                        val accumulated = if (frame.isInput) {
                            inputText = accumulateVoiceAgentTranscript(inputText, frame.text)
                            inputText
                        } else {
                            outputText = accumulateVoiceAgentTranscript(outputText, frame.text)
                            outputText
                        }
                        events.trySend(
                            VoiceAgentEvent.Transcript(
                                input = frame.isInput,
                                text = accumulated,
                                done = frame.done,
                            ),
                        )
                        if (frame.done) {
                            if (frame.isInput) inputText = "" else outputText = ""
                        }
                    }

                    is VoiceAgentToolCallFrame -> events.trySend(
                        VoiceAgentEvent.ToolCall(frame.id, frame.name, frame.args.orEmpty()),
                    )

                    is VoiceAgentInterruptedFrame -> {
                        // The cut answer is gone; a fresh one starts from empty.
                        outputText = ""
                        events.trySend(VoiceAgentEvent.Interrupted)
                    }

                    is VoiceAgentErrorFrame -> events.trySend(
                        VoiceAgentEvent.Failure(frame.code, frame.message, frame.remediation),
                    )

                    is VoiceAgentSessionEndFrame -> {
                        events.trySend(VoiceAgentEvent.Closed(frame.reason))
                        events.close()
                    }

                    // Sequence progress, provider events, keepalive answers and
                    // forward-compatible unknowns carry no host-visible state.
                    is VoiceAgentSequenceStepFrame,
                    is VoiceAgentEventFrame,
                    is VoiceAgentPongFrame,
                    is VoiceAgentUnknownFrame,
                    -> Unit
                }
            }

            override fun onMessage(webSocket: WebSocket, bytes: ByteString) {
                events.trySend(VoiceAgentEvent.Audio(bytes.toByteArray()))
            }

            override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) {
                VoiceLog.e(
                    VoiceLog.AGENT,
                    "ws_failure http=${response?.code ?: "-"}",
                    t,
                )
                events.trySend(
                    VoiceAgentEvent.Failure(
                        code = FAILURE_CODE,
                        message = t.message ?: t.javaClass.simpleName,
                    ),
                )
                events.trySend(VoiceAgentEvent.Closed(VoiceAgentEndReasons.ERROR))
                events.close()
            }

            override fun onClosing(webSocket: WebSocket, code: Int, reason: String) {
                // Echo only close codes OkHttp will accept sending; anything
                // else — notably 1005 "no status" — clamps to normal closure so
                // a clean shutdown does not degrade into a failure.
                val echoCode = when (code) {
                    in 1000..1003, in 1007..1011, in 3000..4999 -> code
                    else -> NORMAL_CLOSURE
                }
                webSocket.close(echoCode, reason.take(123))
            }

            override fun onClosed(webSocket: WebSocket, code: Int, reason: String) {
                events.trySend(
                    VoiceAgentEvent.Closed(reason.ifEmpty { VoiceAgentEndReasons.CLIENT }),
                )
                events.close()
            }
        }

        val webSocket = client.newWebSocket(requestBuilder.build(), listener)
        return WsVoiceAgentSession(webSocket, codec, events)
    }

    companion object {
        /** Transport-level failure, distinct from a server error frame. */
        const val FAILURE_CODE = "ws_failure"
        internal const val NORMAL_CLOSURE = 1000
    }
}

private class WsVoiceAgentSession(
    private val webSocket: WebSocket,
    private val codec: VoiceAgentCodec,
    private val channel: Channel<VoiceAgentEvent>,
) : VoiceAgentSession {

    override val events: Flow<VoiceAgentEvent> = channel.receiveAsFlow()

    override suspend fun start(options: VoiceAgentStartFrame) {
        webSocket.send(codec.encodeStart(options))
    }

    override suspend fun sendAudio(pcm: ByteArray) {
        webSocket.send(pcm.toByteString(0, pcm.size))
    }

    override suspend fun endTurn() {
        webSocket.send(codec.encodeControl(VoiceAgentMsg.AUDIO_END))
    }

    override suspend fun sendText(text: String) {
        webSocket.send(codec.encodeText(text))
    }

    override suspend fun respondToTool(id: String, name: String, response: Map<String, Any?>) {
        webSocket.send(
            codec.encodeToolResponse(
                VoiceAgentToolResponseFrame(id = id, name = name, response = response),
            ),
        )
    }

    override suspend fun cancelReply() {
        webSocket.send(codec.encodeControl(VoiceAgentMsg.CANCEL))
    }

    override suspend fun advanceStep(reason: String?) {
        webSocket.send(codec.encodeAdvanceStep(VoiceAgentAdvanceStepFrame(reason = reason)))
    }

    // send() returns false when the socket is closed or shutting down — the
    // only synchronous "stop pinging" signal a driver gets, so forward it
    // rather than discard it.
    override suspend fun keepAlive(): Boolean =
        webSocket.send(codec.encodeControl(VoiceAgentMsg.PING))

    override suspend fun close() {
        webSocket.send(codec.encodeControl(VoiceAgentMsg.STOP))
        webSocket.close(VoiceAgentWsClient.NORMAL_CLOSURE, "client")
    }
}
