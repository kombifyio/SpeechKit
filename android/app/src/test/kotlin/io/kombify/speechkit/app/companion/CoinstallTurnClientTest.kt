package io.kombify.speechkit.app.companion

import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.async
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.runTest
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertInstanceOf
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

@OptIn(ExperimentalCoroutinesApi::class)
class CoinstallTurnClientTest {

    private class RecordingTransport : CoinstallTurnTransport {
        var started: Pair<String, String>? = null
        var cancelled: String? = null
        var callback: CoinstallTurnCallback? = null

        override fun startTurn(turnId: String, text: String, callback: CoinstallTurnCallback) {
            started = turnId to text
            this.callback = callback
        }

        override fun cancelTurn(turnId: String) {
            cancelled = turnId
        }
    }

    @Test
    fun `no Companion session is a typed failure and does not start a turn`() = runTest {
        val transport = RecordingTransport()
        val client = CoinstallTurnClient(sessionPresent = { false }, transport = transport)
        val outcome = client.startTurnResult("hello")
        assertEquals(CoinstallTurnResult.NoSession, outcome)
        assertEquals(null, transport.started)
        assertTrue(!client.startTurn("hello").success)
    }

    @Test
    fun `a complete callback is the terminal result`() = runTest(UnconfinedTestDispatcher()) {
        val transport = RecordingTransport()
        val client = CoinstallTurnClient(sessionPresent = { true }, transport = transport)
        val deferred = async { client.startTurnResult("summarise") }
        val started = transport.started
        assertTrue(started != null)
        assertEquals("summarise", started!!.second)
        transport.callback!!.onComplete(started.first, "three unread")
        val outcome = deferred.await()
        val complete = assertInstanceOf(CoinstallTurnResult.Complete::class.java, outcome)
        assertEquals("three unread", complete.text)
    }

    @Test
    fun `an error callback is the terminal result`() = runTest(UnconfinedTestDispatcher()) {
        val transport = RecordingTransport()
        val client = CoinstallTurnClient(sessionPresent = { true }, transport = transport)
        val deferred = async { client.startTurnResult("hello") }
        val started = transport.started!!
        transport.callback!!.onError(started.first, 7, "denied")
        val outcome = deferred.await()
        val failed = assertInstanceOf(CoinstallTurnResult.Failed::class.java, outcome)
        assertEquals(7, failed.code)
    }

    @Test
    fun `the first terminal callback wins`() = runTest(UnconfinedTestDispatcher()) {
        val transport = RecordingTransport()
        val client = CoinstallTurnClient(sessionPresent = { true }, transport = transport)
        val deferred = async { client.startTurnResult("hello") }
        val started = transport.started!!
        transport.callback!!.onComplete(started.first, "done")
        transport.callback!!.onError(started.first, 1, "late")
        val outcome = deferred.await()
        assertInstanceOf(CoinstallTurnResult.Complete::class.java, outcome)
    }
}
