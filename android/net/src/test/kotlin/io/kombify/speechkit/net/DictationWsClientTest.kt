package io.kombify.speechkit.net

import io.kombify.speechkit.stt.streaming.DictationSegmentOptions
import io.kombify.speechkit.stt.streaming.TranscriptEvent
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import okio.ByteString
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.util.concurrent.CopyOnWriteArrayList
import java.util.concurrent.atomic.AtomicInteger

/**
 * Drives a full segment lifecycle against an in-process WebSocket server:
 * start → ready → PCM → finalize → draft/final transcripts → segment_done →
 * close. Also pins the ticket subprotocol on the upgrade request.
 */
class DictationWsClientTest {

    private val codec = DictationStreamCodec()

    @Test
    fun `segment lifecycle end to end`() {
        val server = okhttp3.mockwebserver.MockWebServer()
        val audioBytes = AtomicInteger(0)
        val receivedTexts = CopyOnWriteArrayList<String>()

        val serverListener = object : WebSocketListener() {
            override fun onMessage(webSocket: WebSocket, text: String) {
                receivedTexts.add(text)
                when (codec.decodeStart(text)?.type) {
                    StreamMsg.START ->
                        webSocket.send("""{"type":"ready","segment_id":1}""")
                    StreamMsg.FINALIZE -> {
                        webSocket.send(
                            """{"type":"transcript","segment_id":1,"sequence":3,"text":"hallo wel","done":false}""",
                        )
                        webSocket.send(
                            """{"type":"transcript","segment_id":1,"sequence":4,"text":"Hallo Welt.","done":true,"confidence":0.94}""",
                        )
                        webSocket.send("""{"type":"segment_done","segment_id":1}""")
                    }
                }
            }

            override fun onMessage(webSocket: WebSocket, bytes: ByteString) {
                audioBytes.addAndGet(bytes.size)
            }
        }

        server.enqueue(
            okhttp3.mockwebserver.MockResponse().withWebSocketUpgrade(serverListener),
        )
        server.start()

        try {
            val mint = CreateStreamSessionResponse(
                sessionId = "s1",
                wsUrl = server.url("/v1/dictation/stream/sessions/s1/ws").toString(),
                wsSubprotocol = "ticket.test-ticket",
                ticket = "test-ticket",
                capabilities = StreamCapabilities(streaming = true),
            )
            val session = DictationWsClient().connect(mint)
            val events = mutableListOf<TranscriptEvent>()

            // Collect exactly until segment_done — the contract under test is
            // the segment lifecycle. Socket teardown timing (Closed delivery
            // after close()) is not asserted; it races the test harness.
            runBlocking {
                withTimeout(20_000) {
                    session.startSegment(DictationSegmentOptions(language = "de"))
                    session.events.first { event ->
                        events.add(event)
                        when (event) {
                            is TranscriptEvent.SegmentReady -> {
                                session.sendAudio(ByteArray(3200))
                                session.finishSegment()
                                false
                            }
                            is TranscriptEvent.Failure ->
                                error("unexpected failure event: $event")
                            is TranscriptEvent.Closed ->
                                error("session closed before segment_done: $event")
                            is TranscriptEvent.SegmentDone -> true
                            else -> false
                        }
                    }
                }
                session.close()
            }

            val ready = events.filterIsInstance<TranscriptEvent.SegmentReady>()
            assertEquals(listOf(1L), ready.map { it.segmentId })

            val draft = events.filterIsInstance<TranscriptEvent.Draft>().single()
            assertEquals("hallo wel", draft.text)

            val final = events.filterIsInstance<TranscriptEvent.Final>().single()
            assertEquals("Hallo Welt.", final.text)
            assertEquals(0.94, final.confidence!!, 1e-9)

            assertEquals(3200, audioBytes.get())

            // The first server-received text frame is the start frame.
            val start = codec.decodeStart(receivedTexts.first())
            assertEquals(StreamMsg.START, start?.type)
            assertEquals("de", start?.language)

            // Ticket travels as subprotocol, never in the URL.
            val upgrade = server.takeRequest()
            assertEquals("ticket.test-ticket", upgrade.getHeader("Sec-WebSocket-Protocol"))
            assertTrue(!upgrade.path.orEmpty().contains("test-ticket"))
        } finally {
            // MockWebServer.shutdown() can race the WebSocket close handshake
            // and throw "Gave up waiting for queue to shut down" — teardown
            // hygiene, not part of the contract under test.
            runCatching { server.shutdown() }
        }
    }

    @Test
    fun `ws url scheme maps onto http for okhttp`() {
        assertEquals("http://h:1/p", DictationWsClient.httpUrlFor("ws://h:1/p"))
        assertEquals("https://h/p", DictationWsClient.httpUrlFor("wss://h/p"))
        assertEquals("https://h/p", DictationWsClient.httpUrlFor("https://h/p"))
    }
}
