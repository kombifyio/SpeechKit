package io.kombify.speechkit.net

import kotlinx.coroutines.runBlocking
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.jupiter.api.AfterEach
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.BeforeEach
import org.junit.jupiter.api.Test
import org.junit.jupiter.api.assertThrows

class SpeechKitServerApiTest {

    private lateinit var server: MockWebServer

    @BeforeEach
    fun setUp() {
        server = MockWebServer()
        server.start()
    }

    @AfterEach
    fun tearDown() {
        server.shutdown()
    }

    private fun api(token: String? = "svc-token") = SpeechKitServerApi(
        ConnectionProfile.Server(server.url("/").toString(), token),
    )

    @Test
    fun `session mint parses response and sends bearer auth`() {
        server.enqueue(
            MockResponse()
                .setHeader("Content-Type", "application/json")
                .setBody(
                    """
                    {
                      "session_id": "abc123",
                      "ws_url": "wss://speechkit.example.com/v1/dictation/stream/sessions/abc123/ws",
                      "ws_subprotocol": "ticket.tkt-1",
                      "ticket": "tkt-1",
                      "expires_at": "2026-07-15T18:00:00Z",
                      "capabilities": { "streaming": true, "emulation": "off" }
                    }
                    """.trimIndent(),
                ),
        )

        val response = runBlocking { api().createDictationStreamSession() }

        assertEquals("abc123", response.sessionId)
        assertTrue(response.wsUrl.endsWith("/abc123/ws"))
        assertEquals("ticket.tkt-1", response.wsSubprotocol)
        assertTrue(response.capabilities.streaming)

        val request = server.takeRequest()
        assertEquals("POST", request.method)
        assertEquals("/v1/dictation/stream/sessions", request.path)
        assertEquals("Bearer svc-token", request.getHeader("Authorization"))
    }

    @Test
    fun `no auth header without token`() {
        server.enqueue(
            MockResponse()
                .setHeader("Content-Type", "application/json")
                .setBody("""{"session_id":"s","ws_url":"ws://x/ws","ticket":"t","expires_at":"","capabilities":{"streaming":false,"emulation":"off"}}"""),
        )
        runBlocking { api(token = null).createDictationStreamSession() }
        assertEquals(null, server.takeRequest().getHeader("Authorization"))
    }

    @Test
    fun `server error envelope maps onto typed exception`() {
        server.enqueue(
            MockResponse()
                .setResponseCode(401)
                .setHeader("Content-Type", "application/json")
                .setBody("""{"error":{"code":"unauthenticated","message":"identity not available on context"}}"""),
        )

        val ex = assertThrows<SpeechKitApiException> {
            runBlocking { api().createDictationStreamSession() }
        }
        assertEquals(401, ex.httpStatus)
        assertEquals("unauthenticated", ex.code)
    }

    @Test
    fun `batch transcribe posts multipart wav and parses transcript`() {
        server.enqueue(
            MockResponse()
                .setHeader("Content-Type", "application/json")
                .setBody(
                    """
                    {
                      "text": "Hallo Welt.",
                      "language": "de",
                      "duration_ms": 900,
                      "latency_ms": 400,
                      "provider": "assemblyai",
                      "model": "universal-3-5-pro",
                      "confidence": 0.94
                    }
                    """.trimIndent(),
                ),
        )

        val wav = WavEncoder.pcm16ToWav(ByteArray(3200), 16_000)
        val result = runBlocking { api().transcribe(wav, language = "de") }

        assertEquals("Hallo Welt.", result.text)
        assertEquals("assemblyai", result.provider)

        val request = server.takeRequest()
        assertEquals("/v1/dictation/transcribe", request.path)
        assertTrue(request.getHeader("Content-Type").orEmpty().startsWith("multipart/form-data"))
        val body = request.body.readByteString().utf8()
        assertTrue(body.contains("""name="audio""""))
        assertTrue(body.contains("""name="language""""))
    }

    @Test
    fun `assist process posts the selection the summarize codeword reads`() {
        server.enqueue(
            MockResponse()
                .setHeader("Content-Type", "application/json")
                .setBody("""{"text":"Call tomorrow.","speak_text":""}"""),
        )

        val result = runBlocking {
            api().processAssist(
                text = "summarize this",
                locale = "en",
                selection = "We should call tomorrow about the invoice.",
            )
        }

        assertEquals("Call tomorrow.", result.text)
        val request = server.takeRequest()
        assertEquals("/v1/assist/process", request.path)
        val body = request.body.readUtf8()
        assertTrue(body.contains("summarize this"))
        assertTrue(body.contains("We should call tomorrow about the invoice."))
        assertTrue(body.contains("\"locale\":\"en\""))
    }

    @Test
    fun `healthy probes healthz`() {
        server.enqueue(MockResponse().setBody("ok"))
        assertTrue(runBlocking { api().healthy() })
        assertEquals("/healthz", server.takeRequest().path)

        server.enqueue(MockResponse().setResponseCode(503))
        assertFalse(runBlocking { api().healthy() })
    }
}
