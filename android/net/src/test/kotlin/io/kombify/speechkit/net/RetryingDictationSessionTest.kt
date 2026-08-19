package io.kombify.speechkit.net

import io.kombify.speechkit.stt.streaming.DictationSegmentOptions
import io.kombify.speechkit.stt.streaming.StreamingSttSession
import io.kombify.speechkit.stt.streaming.TranscriptEvent
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.receiveAsFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runTest
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.jupiter.api.AfterEach
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.BeforeEach
import org.junit.jupiter.api.Test
import java.io.ByteArrayOutputStream

/**
 * Covers roadmap deferred item #1: after a streaming `Failure(ws_failure)`
 * mid-segment, the controller-level wrapper retries the segment's buffered
 * audio once through the batch tier and emits a Final in place of the error.
 */
@OptIn(kotlinx.coroutines.ExperimentalCoroutinesApi::class)
class RetryingDictationSessionTest {

    /** Channel-backed stand-in for the WS tier. */
    private class FakeStreamSession : StreamingSttSession {
        val channel = Channel<TranscriptEvent>(Channel.UNLIMITED)
        override val events: Flow<TranscriptEvent> = channel.receiveAsFlow()

        val sentAudio = ByteArrayOutputStream()
        var started: DictationSegmentOptions? = null
        var finished = false
        var closed = false

        override suspend fun startSegment(options: DictationSegmentOptions) {
            started = options
        }

        override suspend fun sendAudio(pcm: ByteArray) {
            sentAudio.write(pcm)
        }

        override suspend fun finishSegment() {
            finished = true
        }

        override suspend fun close() {
            closed = true
        }
    }

    /** Batch stand-in that answers every segment with one canned Final. */
    private class FakeBatchSession(private val text: String?) : StreamingSttSession {
        private val channel = Channel<TranscriptEvent>(Channel.UNLIMITED)
        override val events: Flow<TranscriptEvent> = channel.receiveAsFlow()

        val received = ByteArrayOutputStream()
        var started: DictationSegmentOptions? = null
        var closed = false
        private var segmentId = 0L

        override suspend fun startSegment(options: DictationSegmentOptions) {
            started = options
            segmentId += 1
            channel.trySend(TranscriptEvent.SegmentReady(segmentId))
        }

        override suspend fun sendAudio(pcm: ByteArray) {
            received.write(pcm)
        }

        override suspend fun finishSegment() {
            if (text != null) {
                channel.trySend(TranscriptEvent.Final(segmentId, text, confidence = 0.9))
            } else {
                channel.trySend(TranscriptEvent.Failure("batch_failed", "boom"))
            }
            channel.trySend(TranscriptEvent.SegmentDone(segmentId))
        }

        override suspend fun close() {
            closed = true
        }
    }

    private fun wsFailureThenClose(fake: FakeStreamSession) {
        fake.channel.trySend(TranscriptEvent.Failure("ws_failure", "socket reset"))
        fake.channel.trySend(TranscriptEvent.Closed(StreamEndReasons.ERROR))
        fake.channel.close()
    }

    @Test
    fun `rescues a failed segment through the batch tier`() = runTest {
        val fake = FakeStreamSession()
        val batch = FakeBatchSession("rescued sentence")
        var batchMints = 0
        val session = RetryingDictationSession(fake, { batchMints += 1; batch })

        val seen = mutableListOf<TranscriptEvent>()
        val reader = launch { session.events.collect { seen.add(it) } }

        session.startSegment(DictationSegmentOptions(language = "de"))
        session.sendAudio(ByteArray(3200) { 1 })
        session.sendAudio(ByteArray(3200) { 2 })
        fake.channel.trySend(TranscriptEvent.SegmentReady(7))
        fake.channel.trySend(TranscriptEvent.Draft(7, "rescued sen"))
        wsFailureThenClose(fake)
        advanceUntilIdle()
        reader.join()

        assertEquals(1, batchMints)
        assertEquals(6400, batch.received.size(), "batch must get the full buffered PCM")
        assertEquals("de", batch.started?.language, "segment options must carry over")
        assertTrue(batch.closed, "rescue session must be released")
        assertEquals(
            listOf(
                TranscriptEvent.SegmentReady(7),
                TranscriptEvent.Draft(7, "rescued sen"),
                // Failure swallowed; Final re-mapped onto the ws segment id.
                TranscriptEvent.Final(7, "rescued sentence", confidence = 0.9),
                TranscriptEvent.SegmentDone(7),
                TranscriptEvent.Closed(StreamEndReasons.ERROR),
            ),
            seen,
        )
    }

    @Test
    fun `forwards the failure when the batch rescue fails too`() = runTest {
        val fake = FakeStreamSession()
        val session = RetryingDictationSession(fake, { FakeBatchSession(text = null) })

        val seen = mutableListOf<TranscriptEvent>()
        val reader = launch { session.events.collect { seen.add(it) } }

        session.startSegment(DictationSegmentOptions())
        session.sendAudio(ByteArray(320))
        wsFailureThenClose(fake)
        advanceUntilIdle()
        reader.join()

        assertEquals(
            listOf(
                TranscriptEvent.Failure("ws_failure", "socket reset"),
                TranscriptEvent.Closed(StreamEndReasons.ERROR),
            ),
            seen,
        )
    }

    @Test
    fun `does not retry non-retryable codes`() = runTest {
        val fake = FakeStreamSession()
        var batchMints = 0
        val session = RetryingDictationSession(fake, { batchMints += 1; FakeBatchSession("x") })

        val seen = mutableListOf<TranscriptEvent>()
        val reader = launch { session.events.collect { seen.add(it) } }

        session.startSegment(DictationSegmentOptions())
        session.sendAudio(ByteArray(320))
        // Server-side protocol error: socket stays open, no rescue.
        fake.channel.trySend(TranscriptEvent.Failure(StreamErrorCodes.INVALID_FRAME, "bad frame"))
        fake.channel.close()
        advanceUntilIdle()
        reader.join()

        assertEquals(0, batchMints)
        assertEquals(
            listOf<TranscriptEvent>(TranscriptEvent.Failure(StreamErrorCodes.INVALID_FRAME, "bad frame")),
            seen,
        )
    }

    @Test
    fun `does not retry without buffered audio or outside a segment`() = runTest {
        val fake = FakeStreamSession()
        var batchMints = 0
        val session = RetryingDictationSession(fake, { batchMints += 1; FakeBatchSession("x") })

        val seen = mutableListOf<TranscriptEvent>()
        val reader = launch { session.events.collect { seen.add(it) } }

        // Failure between segments: nothing to rescue.
        wsFailureThenClose(fake)
        advanceUntilIdle()
        reader.join()

        assertEquals(0, batchMints)
        assertEquals("ws_failure", (seen.first() as TranscriptEvent.Failure).code)
    }

    @Test
    fun `a streamed Final clears the buffer so a rescue never duplicates committed text`() = runTest {
        val fake = FakeStreamSession()
        val batch = FakeBatchSession("tail only")
        val session = RetryingDictationSession(fake, { batch })

        val seen = mutableListOf<TranscriptEvent>()
        val reader = launch { session.events.collect { seen.add(it) } }

        session.startSegment(DictationSegmentOptions())
        session.sendAudio(ByteArray(3200) { 1 })
        fake.channel.trySend(TranscriptEvent.Final(3, "already committed"))
        advanceUntilIdle()
        // Audio after the committed Final is the only rescueable tail.
        session.sendAudio(ByteArray(320) { 2 })
        wsFailureThenClose(fake)
        advanceUntilIdle()
        reader.join()

        assertEquals(320, batch.received.size(), "rescue must only cover the uncommitted tail")
        assertEquals(
            TranscriptEvent.Final(3, "tail only", confidence = 0.9),
            seen.first { it is TranscriptEvent.Final && it.text == "tail only" },
        )
    }

    @Test
    fun `buffer overflow disarms the rescue instead of truncating it`() = runTest {
        val fake = FakeStreamSession()
        var batchMints = 0
        val session = RetryingDictationSession(
            fake,
            { batchMints += 1; FakeBatchSession("x") },
            maxBufferBytes = 1000,
        )

        val seen = mutableListOf<TranscriptEvent>()
        val reader = launch { session.events.collect { seen.add(it) } }

        session.startSegment(DictationSegmentOptions())
        session.sendAudio(ByteArray(900))
        session.sendAudio(ByteArray(900)) // exceeds the cap -> overflow
        wsFailureThenClose(fake)
        advanceUntilIdle()
        reader.join()

        assertEquals(0, batchMints, "a partial rescue would silently truncate the dictation")
        assertEquals("ws_failure", (seen.first() as TranscriptEvent.Failure).code)
    }
}

/**
 * End-to-end shape of the rescue against the real [BatchDictationSession] and
 * HTTP client: buffered PCM leaves as one multipart WAV and comes back as the
 * segment's Final.
 */
@OptIn(kotlinx.coroutines.ExperimentalCoroutinesApi::class)
class RetryingDictationSessionBatchHttpTest {

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

    private class FakeStreamSession : StreamingSttSession {
        val channel = Channel<TranscriptEvent>(Channel.UNLIMITED)
        override val events: Flow<TranscriptEvent> = channel.receiveAsFlow()
        override suspend fun startSegment(options: DictationSegmentOptions) = Unit
        override suspend fun sendAudio(pcm: ByteArray) = Unit
        override suspend fun finishSegment() = Unit
        override suspend fun close() = Unit
    }

    @Test
    fun `rescue posts buffered audio to the batch endpoint`() = runTest {
        server.enqueue(
            MockResponse()
                .setHeader("Content-Type", "application/json")
                .setBody("""{"text":"Hallo Welt.","language":"de","confidence":0.91}"""),
        )
        val api = SpeechKitServerApi(ConnectionProfile.Server(server.url("/").toString(), null))
        val fake = FakeStreamSession()
        val session = RetryingDictationSession(fake, { BatchDictationSession(api) })

        val seen = mutableListOf<TranscriptEvent>()
        val reader = launch { session.events.collect { seen.add(it) } }

        session.startSegment(DictationSegmentOptions(language = "de"))
        session.sendAudio(ByteArray(3200))
        fake.channel.trySend(TranscriptEvent.SegmentReady(4))
        fake.channel.trySend(TranscriptEvent.Failure("ws_failure", "gone"))
        fake.channel.trySend(TranscriptEvent.Closed(StreamEndReasons.ERROR))
        fake.channel.close()
        advanceUntilIdle()
        reader.join()

        val request = server.takeRequest()
        assertEquals("/v1/dictation/transcribe", request.path)
        assertTrue(request.getHeader("Content-Type").orEmpty().startsWith("multipart/form-data"))

        assertNull(seen.find { it is TranscriptEvent.Failure }, "failure must be rescued away")
        assertEquals(
            TranscriptEvent.Final(4, "Hallo Welt.", confidence = 0.91),
            seen.first { it is TranscriptEvent.Final },
        )
    }
}
