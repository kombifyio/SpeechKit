package io.kombify.speechkit.net

import io.kombify.speechkit.log.VoiceLog
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.withTimeoutOrNull
import kotlinx.coroutines.flow.Flow
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import okio.ByteString
import okio.ByteString.Companion.toByteString
import java.util.concurrent.atomic.AtomicBoolean

/**
 * What a Voice Agent session reports to its host, normalised away from the
 * wire shape. Hosts render these; they never parse frames themselves.
 */
sealed interface VoiceAgentEvent {
    /** Lifecycle transition (see [VoiceAgentStates]). */
    data class State(val state: String, val eventType: String? = null) : VoiceAgentEvent {
        constructor(state: String) : this(state, null)
        fun copy(state: String = this.state): State = State(state, eventType)
    }

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

    /** A turn error is recoverable; [Failure.fatal] ends the session. */
    data class Failure(
        val code: String,
        val message: String,
        val remediation: String? = null,
        val fatal: Boolean = false,
    ) : VoiceAgentEvent {
        // Preserve the published three-field constructor/copy, including default masks.
        constructor(code: String, message: String, remediation: String? = null) :
            this(code, message, remediation, false)
        fun copy(code: String = this.code, message: String = this.message,
            remediation: String? = this.remediation): Failure = Failure(code, message, remediation, fatal)
    }

    /** Terminal. No further events follow. */
    data class Closed(val reason: String) : VoiceAgentEvent
}

/** Safe setup failure: capture must not start after a failed session.start. */
class VoiceAgentSetupException(val code: String) : IllegalStateException(code)

/** A live Voice Agent conversation. */
interface VoiceAgentSession {
    val events: Flow<VoiceAgentEvent>

    /** Opens the conversation and waits for provider session_ready (at most 20 seconds).
     * Must be the first call. Throws [VoiceAgentSetupException] if setup fails.
     */
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
     * [VoiceAgentEvent.Interrupted]. Queued audio is cut locally as well;
     * incoming audio stays muted until the server acknowledges cancellation.
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
 * mirroring app/internal/server/wssession — never in the URL, so it stays out of
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
        val events = VoiceAgentEventQueue()
        val ready = CompletableDeferred<Unit>()
        val providerReady = AtomicBoolean(false)
        fun failSetup(code: String) {
            providerReady.set(false)
            ready.completeExceptionally(VoiceAgentSetupException(code))
        }
        fun emit(webSocket: WebSocket, event: VoiceAgentEvent) {
            if (!events.offer(event)) {
                failSetup(OVERFLOW_CODE)
                webSocket.cancel()
            }
        }

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
                if (text.length > MAX_CONTROL_CHARS) {
                    failSetup(OVERFLOW_CODE)
                    events.finish(VoiceAgentEndReasons.ERROR, VoiceAgentEvent.Failure(
                        OVERFLOW_CODE, "Voice control frame capacity exceeded", fatal = true,
                    ))
                    webSocket.cancel()
                    return
                }
                when (val frame = codec.decodeServerFrame(text)) {
                    is VoiceAgentStateFrame -> {
                        if (frame.state == VoiceAgentStates.LISTENING && frame.eventType == "session_ready" && !ready.isCompleted) {
                            providerReady.set(true)
                            if (!ready.complete(Unit)) providerReady.set(false)
                        }
                        emit(webSocket, VoiceAgentEvent.State(frame.state, frame.eventType))
                    }

                    is VoiceAgentTranscriptFrame -> {
                        val accumulated = if (frame.isInput) {
                            inputText = accumulateVoiceAgentTranscript(inputText, frame.text).takeLast(MAX_TRANSCRIPT_CHARS)
                            inputText
                        } else {
                            outputText = accumulateVoiceAgentTranscript(outputText, frame.text).takeLast(MAX_TRANSCRIPT_CHARS)
                            outputText
                        }
                        emit(webSocket,
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

                    is VoiceAgentToolCallFrame -> emit(webSocket,
                        VoiceAgentEvent.ToolCall(frame.id, frame.name, frame.args.orEmpty()),
                    )

                    is VoiceAgentInterruptedFrame -> {
                        // The cut answer is gone; a fresh one starts from empty.
                        outputText = ""
                        emit(webSocket, VoiceAgentEvent.Interrupted)
                    }

                    is VoiceAgentErrorFrame -> {
                        val failure = VoiceAgentEvent.Failure(safeVoiceAgentCode(frame.code, "server_error"),
                            frame.message, frame.remediation, frame.fatal)
                        if (frame.fatal) {
                            failSetup(failure.code)
                            events.finish(voiceAgentFatalEndReason(failure.code), failure)
                        } else emit(webSocket, failure)
                    }

                    is VoiceAgentSessionEndFrame -> {
                        failSetup(SETUP_FAILURE_CODE)
                        events.finish(safeVoiceAgentCode(frame.reason, VoiceAgentEndReasons.ERROR))
                        webSocket.close(NORMAL_CLOSURE, "session ended")
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
                if (bytes.size > VoiceAgentEventQueue.MAX_AUDIO_BYTES) {
                    failSetup(OVERFLOW_CODE)
                    events.finish(VoiceAgentEndReasons.ERROR, VoiceAgentEvent.Failure(
                        OVERFLOW_CODE, "Voice audio frame capacity exceeded", fatal = true,
                    ))
                    webSocket.cancel()
                } else emit(webSocket, VoiceAgentEvent.Audio(bytes.toByteArray()))
            }

            override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) {
                // Throwable messages may contain request URLs, credentials or provider bodies.
                VoiceLog.e(VoiceLog.AGENT, "ws_failure http=${response?.code ?: "-"}")
                failSetup(FAILURE_CODE)
                events.finish(VoiceAgentEndReasons.ERROR, VoiceAgentEvent.Failure(
                    code = FAILURE_CODE, message = "Voice transport failed", fatal = true,
                ))
            }

            override fun onClosing(webSocket: WebSocket, code: Int, reason: String) {
                failSetup(SETUP_FAILURE_CODE)
                // Echo only close codes OkHttp will accept sending; anything
                // else — notably 1005 "no status" — clamps to normal closure so
                // a clean shutdown does not degrade into a failure.
                val echoCode = when (code) {
                    in 1000..1003, in 1007..1011, in 3000..4999 -> code
                    else -> NORMAL_CLOSURE
                }
                webSocket.close(echoCode, "peer closed")
            }

            override fun onClosed(webSocket: WebSocket, code: Int, reason: String) {
                failSetup(SETUP_FAILURE_CODE)
                events.finish(safeVoiceAgentCode(reason, VoiceAgentEndReasons.CLIENT))
            }
        }

        val webSocket = client.newWebSocket(requestBuilder.build(), listener)
        return WsVoiceAgentSession(webSocket, codec, events, ready, providerReady)
    }

    companion object {
        /** Transport-level failure, distinct from a server error frame. */
        const val FAILURE_CODE = "ws_failure"
        const val SEND_FAILURE_CODE = "ws_send_failed"
        const val OVERFLOW_CODE = "voice_buffer_overflow"
        const val SETUP_FAILURE_CODE = "ws_setup_failed"
        const val SETUP_TIMEOUT_CODE = "ws_setup_timeout"
        const val NOT_READY_CODE = "ws_not_ready"
        private const val MAX_CONTROL_CHARS = 64 * 1024
        private const val MAX_TRANSCRIPT_CHARS = 16 * 1024
        internal const val NORMAL_CLOSURE = 1000
    }
}

private class WsVoiceAgentSession(
    private val webSocket: WebSocket,
    private val codec: VoiceAgentCodec,
    private val channel: VoiceAgentEventQueue,
    private val ready: CompletableDeferred<Unit>,
    private val providerReady: AtomicBoolean,
) : VoiceAgentSession {

    override val events: Flow<VoiceAgentEvent> = channel.events
    private val sendLock = Any()

    override suspend fun start(options: VoiceAgentStartFrame) {
        if (!send(codec.encodeStart(options))) throw VoiceAgentSetupException(VoiceAgentWsClient.SEND_FAILURE_CODE)
        try {
            if (withTimeoutOrNull(SETUP_TIMEOUT_MILLIS) { ready.await(); true } == null) {
                failSend(VoiceAgentWsClient.SETUP_TIMEOUT_CODE)
                throw VoiceAgentSetupException(VoiceAgentWsClient.SETUP_TIMEOUT_CODE)
            }
        } catch (cancelled: CancellationException) {
            channel.finish(VoiceAgentEndReasons.CLIENT)
            webSocket.cancel()
            throw cancelled
        }
    }

    override suspend fun sendAudio(pcm: ByteArray) {
        sendAudioFrame(pcm)
    }

    override suspend fun endTurn() {
        send(codec.encodeControl(VoiceAgentMsg.AUDIO_END))
    }

    override suspend fun sendText(text: String) {
        send(codec.encodeText(text))
    }

    override suspend fun respondToTool(id: String, name: String, response: Map<String, Any?>) {
        send(
            codec.encodeToolResponse(
                VoiceAgentToolResponseFrame(id = id, name = name, response = response),
            ),
        )
    }

    override suspend fun cancelReply() {
        if (channel.interrupt()) send(codec.encodeControl(VoiceAgentMsg.CANCEL))
    }

    override suspend fun advanceStep(reason: String?) {
        send(codec.encodeAdvanceStep(VoiceAgentAdvanceStepFrame(reason = reason)))
    }

    // send() returns false when the socket is closed or shutting down — the
    // only synchronous "stop pinging" signal a driver gets, so forward it
    // rather than discard it.
    override suspend fun keepAlive(): Boolean = send(codec.encodeControl(VoiceAgentMsg.PING))

    private fun send(text: String): Boolean = synchronized(sendLock) {
        if (text.length > 64 * 1024 || webSocket.queueSize() > MAX_UPLINK_BYTES) {
            failSend()
            return@synchronized false
        }
        accepted(webSocket.send(text))
    }

    private fun sendAudioFrame(pcm: ByteArray) = synchronized(sendLock) {
        // Bound OkHttp's outbound queue by capture duration as well as its own byte ceiling.
        if (!providerReady.get()) {
            failSend(VoiceAgentWsClient.NOT_READY_CODE)
        } else if (pcm.size > MAX_UPLINK_BYTES || pcm.size % 2 != 0 ||
            webSocket.queueSize() > MAX_UPLINK_BYTES - pcm.size) {
            failSend()
        } else accepted(webSocket.send(pcm.toByteString()))
    }

    private fun accepted(sent: Boolean): Boolean {
        if (!sent) failSend()
        return sent
    }

    private fun failSend(code: String = VoiceAgentWsClient.SEND_FAILURE_CODE) {
        providerReady.set(false)
        ready.completeExceptionally(VoiceAgentSetupException(code))
        channel.finish(VoiceAgentEndReasons.ERROR, VoiceAgentEvent.Failure(
            code, "Voice transport could not continue", fatal = true,
        ))
        webSocket.cancel()
    }

    private companion object {
        const val SETUP_TIMEOUT_MILLIS = 20000L
        const val MAX_UPLINK_BYTES = VoiceAgentAudio.CLIENT_SAMPLE_RATE * 2 * 2L
    }

    override suspend fun close() {
        providerReady.set(false)
        ready.completeExceptionally(VoiceAgentSetupException(VoiceAgentWsClient.SETUP_FAILURE_CODE))
        channel.finish(VoiceAgentEndReasons.CLIENT)
        synchronized(sendLock) {
            if (!webSocket.send(codec.encodeControl(VoiceAgentMsg.STOP)) ||
                !webSocket.close(VoiceAgentWsClient.NORMAL_CLOSURE, "client")) webSocket.cancel()
        }
    }
}

/** Only bounded protocol identifiers may cross diagnostic and Binder boundaries as codes. */
fun safeVoiceAgentCode(code: String, fallback: String): String =
    code.takeIf { it.length in 1..64 && it.all { char -> char in 'a'..'z' || char in '0'..'9' || char == '_' } }
        ?: fallback

/** Matches the producer's authorization termination taxonomy. No renewal or replay. */
fun voiceAgentFatalEndReason(code: String): String = when (code) {
    "auth_expired" ->
        VoiceAgentEndReasons.AUTHORIZATION_EXPIRED
    else -> VoiceAgentEndReasons.ERROR
}
