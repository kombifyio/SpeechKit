package io.kombify.speechkit.net

import io.mockk.every
import io.mockk.mockk
import io.mockk.slot
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.async
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.toList
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import okhttp3.OkHttpClient
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import okio.ByteString
import okio.ByteString.Companion.toByteString
import org.junit.jupiter.api.Assertions.assertArrayEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

@OptIn(ExperimentalCoroutinesApi::class)
class VoiceAgentWsClientTest {
    private class Harness {
        val socket = mockk<WebSocket>(relaxed = true)
        private val client = mockk<OkHttpClient>()
        private val listenerSlot = slot<WebSocketListener>()
        val sentAudio = ArrayList<ByteArray>()
        val session: VoiceAgentSession
        val listener: WebSocketListener get() = listenerSlot.captured
        init {
            every { client.newWebSocket(any(), capture(listenerSlot)) } returns socket
            every { socket.send(any<String>()) } returns true
            every { socket.send(any<ByteString>()) } answers { sentAudio += firstArg<ByteString>().toByteArray(); true }
            every { socket.queueSize() } returns 0L
            session = VoiceAgentWsClient(client).connect(CreateVoiceAgentSessionResponse(
                sessionId = "session", wsUrl = "https://localhost/voice", ticket = "test-ticket",
            ))
        }
        fun audio(pcm: ByteArray) = listener.onMessage(socket, pcm.toByteString())
        fun interrupted() = listener.onMessage(socket, """{"type":"interrupted"}""")
    }

    @Test
    fun `overloaded audio fails closed while retaining a usable terminal event`() = runTest {
        val h = Harness()
        h.audio(ByteArray(VoiceAgentAudio.SERVER_SAMPLE_RATE * 2 * 2))
        h.audio(byteArrayOf(1, 2))
        val events = h.session.events.toList()
        assertTrue(events.any { it is VoiceAgentEvent.Failure && it.code == VoiceAgentWsClient.OVERFLOW_CODE })
        assertTrue(events.last() is VoiceAgentEvent.Closed)
        assertFalse(events.any { it is VoiceAgentEvent.Audio })
    }

    @Test
    fun `cancel drops the old reply including in-flight audio until acknowledged`() = runTest {
        val h = Harness()
        h.audio(byteArrayOf(1, 2))
        h.session.cancelReply()
        h.audio(byteArrayOf(3, 4))
        h.interrupted()
        val nextReply = byteArrayOf(5, 6)
        h.audio(nextReply)
        val heard = h.session.events.first { it is VoiceAgentEvent.Audio } as VoiceAgentEvent.Audio
        assertArrayEquals(nextReply, heard.pcm)
        h.session.close()
    }

    @Test
    fun `send rejection wakes a waiting collector with failure and closure`() = runTest {
        val h = Harness()
        every { h.socket.send(any<String>()) } returns false
        val outcome = async { h.session.events.toList() }
        runCurrent()
        val setupFailure = runCatching { h.session.start(VoiceAgentStartFrame()) }.exceptionOrNull()
        assertTrue(setupFailure is VoiceAgentSetupException)
        val events = outcome.await()
        assertTrue(events.any { it is VoiceAgentEvent.Failure && it.code == VoiceAgentWsClient.SEND_FAILURE_CODE })
        assertTrue(events.last() is VoiceAgentEvent.Closed)
    }

    @Test
    fun `start waits for actual provider readiness before capture can send audio`() = runTest {
        val h = Harness()
        val start = async { h.session.start(VoiceAgentStartFrame()); h.session.sendAudio(byteArrayOf(1, 2)) }
        runCurrent()
        h.listener.onMessage(h.socket, """{"type":"state","state":"listening"}""")
        runCurrent()
        assertFalse(start.isCompleted)
        assertTrue(h.sentAudio.isEmpty())
        h.listener.onMessage(h.socket, """{"type":"state","state":"listening","event_type":"session_ready"}""")
        start.await()
        assertArrayEquals(byteArrayOf(1, 2), h.sentAudio.single())
        h.session.close()
    }

    @Test
    fun `silent setup expires without opening a capture path`() = runTest {
        val h = Harness()
        val setup = async { runCatching { h.session.start(VoiceAgentStartFrame()) }.exceptionOrNull() }
        runCurrent()
        advanceTimeBy(20001)
        runCurrent()
        val failure = setup.await()
        assertTrue(failure is VoiceAgentSetupException)
        assertEquals(VoiceAgentWsClient.SETUP_TIMEOUT_CODE, (failure as VoiceAgentSetupException).code)
        assertTrue(h.sentAudio.isEmpty())
        val events = h.session.events.toList()
        assertTrue(events.last() is VoiceAgentEvent.Closed)
        assertTrue(events.any { it is VoiceAgentEvent.Failure && it.fatal })
    }

    @Test
    fun `recoverable turn failure remains usable but fatal authorization failure stops audio`() = runTest {
        val h = Harness()
        h.listener.onMessage(h.socket, """{"type":"error","code":"turn_failed","message":"safe"}""")
        val turnFailure = h.session.events.first() as VoiceAgentEvent.Failure
        assertFalse(turnFailure.fatal)
        h.audio(byteArrayOf(1, 2))
        h.listener.onMessage(h.socket, """{"type":"error","code":"auth_expired","message":"safe","fatal":true}""")
        h.audio(byteArrayOf(3, 4))
        val events = h.session.events.toList()
        val failure = events.filterIsInstance<VoiceAgentEvent.Failure>().first()
        assertTrue(failure.fatal)
        assertEquals("auth_expired", failure.code)
        assertEquals(VoiceAgentEndReasons.AUTHORIZATION_EXPIRED, (events.last() as VoiceAgentEvent.Closed).reason)
        assertFalse(events.any { it is VoiceAgentEvent.Audio })
    }
}
