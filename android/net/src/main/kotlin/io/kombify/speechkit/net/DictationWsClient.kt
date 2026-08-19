package io.kombify.speechkit.net

import io.kombify.speechkit.stt.streaming.DictationSegmentOptions
import io.kombify.speechkit.stt.streaming.StreamingSttSession
import io.kombify.speechkit.stt.streaming.TranscriptEvent
import io.kombify.speechkit.stt.streaming.TranscriptWord
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
 * Server tier of [StreamingSttSession]: one ticket-authenticated WebSocket to
 * the speechkit-server dictation stream endpoint, many sequential segments.
 *
 * The ticket rides in the `Sec-WebSocket-Protocol` header ("ticket.<value>"),
 * mirroring internal/server/wssession — never in the URL, so it stays out of
 * proxy access logs.
 */
class DictationWsClient(
    private val client: OkHttpClient = SpeechKitServerApi.defaultOkHttpClient(),
    private val codec: DictationStreamCodec = DictationStreamCodec(),
) {

    /**
     * Opens the WebSocket for a minted session and returns the live session.
     * Failures surface as [TranscriptEvent.Failure] + [TranscriptEvent.Closed]
     * on the event flow, not as exceptions.
     */
    fun connect(session: CreateStreamSessionResponse): StreamingSttSession {
        val events = Channel<TranscriptEvent>(Channel.UNLIMITED)

        val requestBuilder = Request.Builder().url(httpUrlFor(session.wsUrl))
        val subprotocol = session.wsSubprotocol?.takeIf { it.isNotBlank() }
            ?: session.ticket.takeIf { it.isNotBlank() }?.let { "$TICKET_SUBPROTOCOL_PREFIX$it" }
        subprotocol?.let { requestBuilder.header("Sec-WebSocket-Protocol", it) }

        val listener = object : WebSocketListener() {
            override fun onMessage(webSocket: WebSocket, text: String) {
                when (val frame = codec.decodeServerFrame(text)) {
                    is StreamReadyFrame ->
                        events.trySend(TranscriptEvent.SegmentReady(frame.segmentId))

                    is StreamTranscriptFrame -> events.trySend(
                        if (frame.done) {
                            TranscriptEvent.Final(
                                segmentId = frame.segmentId,
                                text = frame.text,
                                sequence = frame.sequence ?: 0,
                                confidence = frame.confidence,
                                words = frame.words.orEmpty().map {
                                    TranscriptWord(it.text, it.confidence, it.startMs, it.endMs)
                                },
                            )
                        } else {
                            TranscriptEvent.Draft(
                                segmentId = frame.segmentId,
                                text = frame.text,
                                sequence = frame.sequence ?: 0,
                            )
                        },
                    )

                    is StreamSegmentDoneFrame ->
                        events.trySend(TranscriptEvent.SegmentDone(frame.segmentId))

                    is StreamErrorFrame ->
                        events.trySend(TranscriptEvent.Failure(frame.code, frame.message))

                    is StreamSessionEndFrame -> {
                        events.trySend(TranscriptEvent.Closed(frame.reason))
                        events.close()
                    }

                    is StreamPongFrame, is UnknownFrame -> Unit // keepalive / forward-compat
                }
            }

            override fun onMessage(webSocket: WebSocket, bytes: ByteString) {
                // The server never sends audio on this surface; ignore.
            }

            override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) {
                events.trySend(
                    TranscriptEvent.Failure(
                        code = "ws_failure",
                        message = t.message ?: t.javaClass.simpleName,
                    ),
                )
                events.trySend(TranscriptEvent.Closed(StreamEndReasons.ERROR))
                events.close()
            }

            override fun onClosing(webSocket: WebSocket, code: Int, reason: String) {
                // Echo only close codes OkHttp will accept sending (RFC 6455
                // valid, non-reserved); anything else — notably 1005 "no
                // status" — clamps to normal closure so a clean shutdown does
                // not degrade into ws_failure.
                val echoCode = when (code) {
                    in 1000..1003, in 1007..1011, in 3000..4999 -> code
                    else -> 1000
                }
                webSocket.close(echoCode, reason.take(123))
            }

            override fun onClosed(webSocket: WebSocket, code: Int, reason: String) {
                events.trySend(TranscriptEvent.Closed(reason.ifEmpty { StreamEndReasons.CLIENT }))
                events.close()
            }
        }

        val webSocket = client.newWebSocket(requestBuilder.build(), listener)
        return WsStreamingSession(webSocket, codec, events)
    }

    companion object {
        /** Mirrors wssession.TicketSubprotocolPrefix. */
        const val TICKET_SUBPROTOCOL_PREFIX = "ticket."

        /**
         * OkHttp validates request URLs as http/https; ws/wss map 1:1.
         * Server-issued ws_url values are ws(s), test URLs may be http.
         */
        internal fun httpUrlFor(wsUrl: String): String = when {
            wsUrl.startsWith("ws://") -> "http://" + wsUrl.removePrefix("ws://")
            wsUrl.startsWith("wss://") -> "https://" + wsUrl.removePrefix("wss://")
            else -> wsUrl
        }
    }
}

private class WsStreamingSession(
    private val webSocket: WebSocket,
    private val codec: DictationStreamCodec,
    private val channel: Channel<TranscriptEvent>,
) : StreamingSttSession {

    override val events: Flow<TranscriptEvent> = channel.receiveAsFlow()

    override suspend fun startSegment(options: DictationSegmentOptions) {
        webSocket.send(codec.encodeStart(options.toStartFrame()))
    }

    override suspend fun sendAudio(pcm: ByteArray) {
        webSocket.send(pcm.toByteString(0, pcm.size))
    }

    override suspend fun finishSegment() {
        webSocket.send(codec.encodeControl(StreamMsg.FINALIZE))
    }

    // send() returns false when the socket is closed or shutting down — the
    // only synchronous "stop pinging" signal a driver gets, so forward it
    // rather than discard it.
    override suspend fun keepAlive(): Boolean =
        webSocket.send(codec.encodeControl(StreamMsg.PING))

    override suspend fun close() {
        webSocket.send(codec.encodeControl(StreamMsg.STOP))
        webSocket.close(NORMAL_CLOSURE, "client")
    }

    private companion object {
        const val NORMAL_CLOSURE = 1000
    }
}

/**
 * Maps the transport-agnostic segment options onto the wire start frame.
 * `interimResults=true` is the server default and stays omitted.
 */
internal fun DictationSegmentOptions.toStartFrame(
    format: StreamAudioFormat? = null,
): StreamStartFrame = StreamStartFrame(
    language = language,
    model = model,
    providerProfileId = providerProfileId,
    interimResults = if (interimResults) null else false,
    endpointingMs = endpointingMs,
    keyterms = keyterms.takeIf { it.isNotEmpty() },
    promptHint = promptHint,
    format = format,
)
