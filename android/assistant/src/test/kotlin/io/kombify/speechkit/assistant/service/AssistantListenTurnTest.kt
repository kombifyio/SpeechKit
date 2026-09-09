package io.kombify.speechkit.assistant.service

import io.kombify.speechkit.assistant.intent.ActionResult
import io.kombify.speechkit.assistant.intent.IntentRouter
import io.kombify.speechkit.audio.AudioCapture
import io.kombify.speechkit.stt.streaming.DictationSegmentOptions
import io.kombify.speechkit.stt.streaming.StreamingSttSession
import io.kombify.speechkit.stt.streaming.TranscriptEvent
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.receiveAsFlow
import kotlinx.coroutines.test.runTest
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertInstanceOf
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

@OptIn(ExperimentalCoroutinesApi::class)
class AssistantListenTurnTest {

    private class FakeSession(
        override val capturesOwnAudio: Boolean = true,
    ) : StreamingSttSession {
        val channel = Channel<TranscriptEvent>(Channel.UNLIMITED)
        override val events: Flow<TranscriptEvent> = channel.receiveAsFlow()

        override suspend fun startSegment(options: DictationSegmentOptions) = Unit
        override suspend fun sendAudio(pcm: ByteArray) = Unit
        override suspend fun finishSegment() = Unit
        override suspend fun close() {
            channel.trySend(TranscriptEvent.Closed("client"))
            channel.close()
        }
    }

    private val silentCapture = AudioCapture { flowOf() }
    private val router = IntentRouter()

    private class FakeCompanion(
        private val live: Boolean,
        private val start: suspend (String) -> ActionResult,
    ) : io.kombify.speechkit.assistant.intent.CompanionTurnExecutor {
        override fun hasSession(): Boolean = live
        override suspend fun startTurn(text: String): ActionResult = start(text)
    }

    private fun turn(
        session: FakeSession,
        execute: suspend (io.kombify.speechkit.assistant.intent.AssistantIntent) -> ActionResult =
            { ActionResult(success = true, responseText = "assist:${it.rawText}") },
        companion: FakeCompanion? = null,
    ) = AssistantListenTurn(
        sessionFactory = { session },
        audioCapture = silentCapture,
        intentRouter = router,
        executeIntent = execute,
        companionTurn = companion,
    )

    @Test
    fun `a recognizer final yields a transcript and an Assist result`() = runTest {
        val session = FakeSession()
        session.channel.trySend(TranscriptEvent.Final(segmentId = 1, text = "what time is it"))
        val outcome = turn(session).run()
        val heard = assertInstanceOf(AssistantTurnOutcome.Heard::class.java, outcome)
        assertEquals("what time is it", heard.text)
        assertTrue(heard.assist.success)
        assertTrue(heard.assist.responseText.contains("what time is it"))
    }

    @Test
    fun `an empty recognizer final is empty-final not mic-unavailable`() = runTest {
        val session = FakeSession()
        session.channel.trySend(TranscriptEvent.Final(segmentId = 1, text = "  "))
        val outcome = turn(session).run()
        assertEquals(AssistantTurnOutcome.EmptyFinal, outcome)
    }

    @Test
    fun `mic permission failure is typed`() = runTest {
        val session = FakeSession()
        session.channel.trySend(
            TranscriptEvent.Failure(code = "mic_permission_denied", message = "denied"),
        )
        val outcome = turn(session).run()
        assertEquals(AssistantTurnOutcome.MicPermissionDenied, outcome)
    }

    @Test
    fun `capture failure is mic-unavailable not a silent no-op`() = runTest {
        val session = FakeSession()
        session.channel.trySend(
            TranscriptEvent.Failure(code = "audio_capture_failed", message = "audio"),
        )
        val outcome = turn(session).run()
        assertEquals(AssistantTurnOutcome.MicUnavailable, outcome)
    }

    @Test
    fun `a general query uses Companion startTurn when a session exists`() = runTest {
        val session = FakeSession()
        session.channel.trySend(TranscriptEvent.Final(segmentId = 1, text = "summarise my inbox"))
        var started: String? = null
        val outcome = turn(
            session,
            execute = { ActionResult(success = false, errorMessage = "local unused") },
            companion = FakeCompanion(live = true) { text ->
                started = text
                ActionResult(success = true, responseText = "three unread")
            },
        ).run()
        val heard = assertInstanceOf(AssistantTurnOutcome.Heard::class.java, outcome)
        assertEquals("summarise my inbox", started)
        assertTrue(heard.assist.success)
        assertEquals("three unread", heard.assist.responseText)
    }

    @Test
    fun `injected Companion without a session uses local Assist`() = runTest {
        var companionCalls = 0
        val session = FakeSession()
        session.channel.trySend(TranscriptEvent.Final(segmentId = 1, text = "hello"))
        val outcome = turn(
            session,
            execute = { ActionResult(success = true, responseText = "assist:${it.rawText}") },
            companion = FakeCompanion(live = false) {
                companionCalls += 1
                ActionResult(success = false, errorMessage = "Companion is signed out")
            },
        ).run()
        val heard = assertInstanceOf(AssistantTurnOutcome.Heard::class.java, outcome)
        assertEquals(0, companionCalls)
        assertTrue(heard.assist.success)
        assertTrue(heard.assist.responseText.contains("hello"))
    }
}
