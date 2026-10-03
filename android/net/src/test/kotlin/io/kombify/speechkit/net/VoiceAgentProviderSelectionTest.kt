package io.kombify.speechkit.net

import com.squareup.moshi.Moshi
import io.kombify.speechkit.domain.ConnectionProfile
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertThrows
import org.junit.jupiter.api.Test
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicReference

/**
 * Per-session provider selection, end to end over the real mint + upgrade.
 *
 * Which backend answers is chosen by the client on every session, so the name
 * has to survive the whole way to the start frame — and there is no discovery
 * endpoint listing what a deployment registered, so being refused is the only
 * way a client finds out that it asked for something the server does not have.
 * Both halves are pinned here.
 */
class VoiceAgentProviderSelectionTest {

    private val startAdapter = Moshi.Builder().build().adapter(VoiceAgentStartFrame::class.java)

    @Test
    fun `the requested provider reaches the start frame`() {
        val received = AtomicReference<String>()
        val started = CountDownLatch(1)
        val server = agentServer { socket, text ->
            received.set(text)
            started.countDown()
            socket.send("""{"type":"state","state":"listening","event_type":"session_ready"}""")
        }
        val controller = VoiceAgentController(
            ConnectionProfile.Server(server.url("/").toString()),
        )
        try {
            runBlocking {
                withTimeout(5_000) { controller.start(VoiceAgentStartFrame(provider = "assemblyai")) }
            }
            check(started.await(10, TimeUnit.SECONDS)) { "no start frame reached the server" }

            val start = startAdapter.fromJson(received.get())
            assertEquals(VoiceAgentMsg.START, start?.type)
            assertEquals("assemblyai", start?.provider)

            // The ticket rides in the subprotocol, never in the URL — a mint
            // request and an upgrade request, in that order.
            server.takeRequest(10, TimeUnit.SECONDS)
            val upgrade = server.takeRequest(10, TimeUnit.SECONDS)
            assertEquals("ticket.t-1", upgrade?.getHeader("Sec-WebSocket-Protocol"))
        } finally {
            runCatching { runBlocking { withTimeout(5_000) { controller.stop() } } }
            // MockWebServer.shutdown() races the WebSocket close handshake;
            // teardown hygiene, not part of the contract under test.
            runCatching { server.shutdown() }
        }
    }

    // Without this the panel can only show the raw server prose, which reads
    // like an outage rather than "you asked for a backend this server has not
    // been given keys for".
    @Test
    fun `a refused provider is reported under its own code`() {
        val server = agentServer { socket, _ ->
            socket.send(
                """{"type":"error","code":"provider_unavailable","fatal":true,""" +
                    """"message":"voice agent provider \"assemblyai\" is not available"}""",
            )
        }
        val controller = VoiceAgentController(
            ConnectionProfile.Server(server.url("/").toString()),
        )
        try {
            // Setup now waits for actual readiness. A rejected provider fails start
            // under its own safe code; no usable event flow or capture can begin.
            val failure = assertThrows(VoiceAgentSetupException::class.java) {
                runBlocking {
                    withTimeout(5_000) {
                        controller.start(VoiceAgentStartFrame(provider = "assemblyai"))
                    }
                }
            }

            assertEquals(
                VoiceAgentErrorCodes.PROVIDER_UNAVAILABLE,
                failure.code,
            )
        } finally {
            runCatching { runBlocking { withTimeout(5_000) { controller.stop() } } }
            runCatching { server.shutdown() }
        }
    }

    /** A mint response plus a socket that hands the start frame to [onStart]. */
    private fun agentServer(onStart: (WebSocket, String) -> Unit): MockWebServer {
        val server = MockWebServer()
        val listener = object : WebSocketListener() {
            override fun onMessage(webSocket: WebSocket, text: String) {
                if (startAdapter.fromJson(text)?.type == VoiceAgentMsg.START) onStart(webSocket, text)
            }
        }
        server.start()
        server.enqueue(
            MockResponse()
                .setResponseCode(201)
                .setHeader("Content-Type", "application/json")
                .setBody(
                    """{"session_id":"s1","ws_url":"${
                        server.url("/v1/voiceagent/sessions/s1/ws")
                    }","ws_subprotocol":"ticket.t-1","ticket":"t-1"}""",
                ),
        )
        server.enqueue(MockResponse().withWebSocketUpgrade(listener))
        return server
    }
}
