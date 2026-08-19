package io.kombify.speechkit.stt.streaming

import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.receiveAsFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.runTest
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

/**
 * Virtual-time tests for the keepalive driver. Real delays would make these
 * either slow (30 s per tick) or flaky, so the session's clock is bound to the
 * test scheduler and the driver runs on the test's backgroundScope.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class KeepAliveSessionTest {

    private class FakeSession : StreamingSttSession {
        val channel = Channel<TranscriptEvent>(Channel.UNLIMITED)
        override val events: Flow<TranscriptEvent> = channel.receiveAsFlow()

        var pings = 0
        var pingResult = true
        var closed = false

        override suspend fun startSegment(options: DictationSegmentOptions) = Unit
        override suspend fun sendAudio(pcm: ByteArray) = Unit
        override suspend fun finishSegment() = Unit
        override suspend fun keepAlive(): Boolean {
            pings += 1
            return pingResult
        }

        override suspend fun close() {
            closed = true
        }
    }

    private fun TestScope.session(
        fake: FakeSession,
        intervalMs: Long = KeepAliveSession.DEFAULT_INTERVAL_MS,
    ) = KeepAliveSession(
        delegate = fake,
        intervalMs = intervalMs,
        clockMs = { testScheduler.currentTime },
        parentScope = backgroundScope,
    )

    @Test
    fun `does not ping before the interval elapses`() = runTest {
        val fake = FakeSession()
        session(fake)

        advanceTimeBy(29_000)

        assertEquals(0, fake.pings, "pinged before the quiet window closed")
    }

    @Test
    fun `pings once the session has been quiet for a full interval`() = runTest {
        val fake = FakeSession()
        session(fake)

        advanceTimeBy(31_000)

        assertEquals(1, fake.pings)
    }

    @Test
    fun `keeps pinging on an idle session`() = runTest {
        val fake = FakeSession()
        session(fake)

        advanceTimeBy(95_000)

        // 30s, 60s, 90s — three pings inside the 120 s server window.
        assertEquals(3, fake.pings)
    }

    @Test
    fun `streaming audio suppresses pings entirely`() = runTest {
        val fake = FakeSession()
        val s = session(fake)

        // 60 s of audio at one chunk per 100 ms. Audio resets the server's
        // idle watchdog too, so a ping here would be pure waste.
        repeat(600) {
            s.sendAudio(ByteArray(3200))
            advanceTimeBy(100)
        }

        assertEquals(0, fake.pings, "pinged while audio was already resetting the watchdog")
    }

    @Test
    fun `first ping lands one full interval after the last audio chunk`() = runTest {
        val fake = FakeSession()
        val s = session(fake)

        advanceTimeBy(20_000)
        s.sendAudio(ByteArray(3200)) // resets the quiet window at t=20s
        advanceTimeBy(29_000) // t=49s — only 29 s of quiet
        assertEquals(0, fake.pings)

        advanceTimeBy(2_000) // t=51s — 31 s since the chunk
        assertEquals(1, fake.pings)
    }

    @Test
    fun `stops pinging once the transport rejects the ping`() = runTest {
        val fake = FakeSession()
        fake.pingResult = false
        session(fake)

        advanceTimeBy(200_000)

        assertEquals(1, fake.pings, "kept pinging a socket that already said no")
    }

    @Test
    fun `close stops the driver`() = runTest {
        val fake = FakeSession()
        val s = session(fake)

        s.close()
        advanceTimeBy(200_000)

        assertTrue(fake.closed)
        assertEquals(0, fake.pings)
    }

    @Test
    fun `a Closed event stops the driver`() = runTest {
        val fake = FakeSession()
        val s = session(fake)
        val seen = mutableListOf<TranscriptEvent>()
        backgroundScope.launch { s.events.collect { seen.add(it) } }

        fake.channel.send(TranscriptEvent.Closed("max_duration"))
        advanceTimeBy(200_000)

        assertEquals(listOf<TranscriptEvent>(TranscriptEvent.Closed("max_duration")), seen)
        assertEquals(0, fake.pings, "kept pinging after the session ended")
    }

    @Test
    fun `forwards events unchanged`() = runTest {
        val fake = FakeSession()
        val s = session(fake)
        val seen = mutableListOf<TranscriptEvent>()
        backgroundScope.launch { s.events.collect { seen.add(it) } }

        fake.channel.send(TranscriptEvent.SegmentReady(1))
        fake.channel.send(TranscriptEvent.Draft(1, "hallo"))
        fake.channel.send(TranscriptEvent.Final(1, "Hallo Welt."))
        fake.channel.send(TranscriptEvent.SegmentDone(1))
        advanceTimeBy(1)

        assertEquals(
            listOf(
                TranscriptEvent.SegmentReady(1),
                TranscriptEvent.Draft(1, "hallo"),
                TranscriptEvent.Final(1, "Hallo Welt."),
                TranscriptEvent.SegmentDone(1),
            ),
            seen,
        )
    }

    @Test
    fun `an explicit keepAlive call resets the quiet window`() = runTest {
        val fake = FakeSession()
        val s = session(fake)

        advanceTimeBy(20_000)
        s.keepAlive() // manual ping at t=20s, counts as activity
        assertEquals(1, fake.pings)

        advanceTimeBy(29_000) // t=49s, 29 s since that ping
        assertEquals(1, fake.pings, "driver pinged despite recent activity")
    }
}
