package io.kombify.speechkit.ime

import android.text.InputType
import android.view.inputmethod.EditorInfo
import android.view.inputmethod.InputConnection
import io.kombify.speechkit.stt.streaming.DictationSegmentOptions
import io.kombify.speechkit.stt.streaming.StreamingSttSession
import io.kombify.speechkit.stt.streaming.TranscriptEvent
import io.mockk.mockk
import io.mockk.verify
import kotlinx.coroutines.awaitCancellation
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flow
import kotlinx.coroutines.flow.receiveAsFlow
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertInstanceOf
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.io.ByteArrayOutputStream

/**
 * FSM tests for the voice panel: permission gate, draft -> commit flow with a
 * fake [StreamingSttSession], the password-field guard, and error recovery.
 * All Android types involved are either interfaces (InputConnection, mocked)
 * or stub-safe (EditorInfo with `returnDefaultValues`).
 */
@OptIn(kotlinx.coroutines.ExperimentalCoroutinesApi::class)
class VoicePanelControllerTest {

    private class FakeSession(
        override val capturesOwnAudio: Boolean = false,
    ) : StreamingSttSession {
        val channel = Channel<TranscriptEvent>(Channel.UNLIMITED)
        override val events: Flow<TranscriptEvent> = channel.receiveAsFlow()

        val startedOptions = mutableListOf<DictationSegmentOptions>()
        val sentAudio = ByteArrayOutputStream()
        var finishCalls = 0
        var closed = false

        override suspend fun startSegment(options: DictationSegmentOptions) {
            startedOptions.add(options)
        }

        override suspend fun sendAudio(pcm: ByteArray) {
            sentAudio.write(pcm)
        }

        override suspend fun finishSegment() {
            finishCalls += 1
        }

        override suspend fun close() {
            closed = true
            channel.trySend(TranscriptEvent.Closed("client"))
            channel.close()
        }
    }

    private class FakeGate(var granted: Boolean) : MicPermissionGate {
        var requests = 0
        override fun isGranted(): Boolean = granted
        override fun request() {
            requests += 1
        }
    }

    /**
     * Emits a fixed burst of chunks, then stays open until cancelled. A
     * delay-looping fake would never let `advanceUntilIdle` terminate on the
     * virtual clock.
     */
    private class FakeCapture(private val chunks: Int = 3) : AudioCapture {
        var collections = 0

        override fun frames(): Flow<ByteArray> = flow {
            collections += 1
            repeat(chunks) { emit(ByteArray(3200)) }
            awaitCancellation()
        }
    }

    private class Harness(
        scope: TestScope,
        granted: Boolean = true,
        capturesOwnAudio: Boolean = false,
        sessionFactory: (suspend () -> StreamingSttSession)? = null,
    ) {
        val session = FakeSession(capturesOwnAudio)
        val gate = FakeGate(granted)
        val capture = FakeCapture()
        var factoryCalls = 0
        val controller = VoicePanelController(
            scope = scope.backgroundScope,
            sessionFactory = sessionFactory ?: { factoryCalls += 1; session },
            audioCapture = capture,
            micPermission = gate,
            initialLanguage = "de",
        )
        val inputConnection = mockk<InputConnection>(relaxed = true)

        fun editor(inputType: Int = InputType.TYPE_CLASS_TEXT): EditorInfo =
            EditorInfo().also { it.inputType = inputType }
    }

    @Test
    fun `showPanel binds an editable field as Idle`() = runTest {
        val h = Harness(this)
        h.controller.showPanel(h.inputConnection, h.editor())
        assertEquals(VoicePanelState.Idle, h.controller.state.value)
    }

    @Test
    fun `password and TYPE_NULL fields are blocked and never start dictation`() = runTest {
        val h = Harness(this)
        val passwordTypes = listOf(
            InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_VARIATION_PASSWORD,
            InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_VARIATION_VISIBLE_PASSWORD,
            InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_VARIATION_WEB_PASSWORD,
            InputType.TYPE_CLASS_NUMBER or InputType.TYPE_NUMBER_VARIATION_PASSWORD,
            InputType.TYPE_NULL,
        )
        for (inputType in passwordTypes) {
            h.controller.showPanel(h.inputConnection, h.editor(inputType))
            assertEquals(VoicePanelState.Blocked, h.controller.state.value, "inputType=$inputType")

            h.controller.toggleMic()
            runCurrent()
            assertEquals(VoicePanelState.Blocked, h.controller.state.value)
        }
        assertEquals(0, h.factoryCalls, "a blocked field must never mint a session")
        assertTrue(h.session.startedOptions.isEmpty())
    }

    @Test
    fun `mic tap without permission gates on the trampoline and resumes on grant`() = runTest {
        val h = Harness(this, granted = false)
        h.controller.showPanel(h.inputConnection, h.editor())

        h.controller.toggleMic()
        assertEquals(VoicePanelState.NeedsMicPermission, h.controller.state.value)
        assertEquals(1, h.gate.requests)
        assertEquals(0, h.factoryCalls)

        h.gate.granted = true
        h.controller.onMicPermissionResult(true)
        runCurrent()

        assertEquals(VoicePanelState.Listening(), h.controller.state.value)
        assertEquals(1, h.factoryCalls)
        assertEquals(listOf("de"), h.session.startedOptions.map { it.language })
    }

    @Test
    fun `denied permission surfaces a retryable error`() = runTest {
        val h = Harness(this, granted = false)
        h.controller.showPanel(h.inputConnection, h.editor())

        h.controller.toggleMic()
        h.controller.onMicPermissionResult(false)

        val state = assertInstanceOf(VoicePanelState.Error::class.java, h.controller.state.value)
        assertEquals(VoicePanelController.ERROR_MIC_DENIED, state.code)
        assertTrue(state.retryable)
    }

    @Test
    fun `drafts become composing text and finals are committed`() = runTest {
        val h = Harness(this)
        h.controller.showPanel(h.inputConnection, h.editor())

        h.controller.toggleMic()
        runCurrent()
        assertEquals(VoicePanelState.Listening(), h.controller.state.value)

        h.session.channel.trySend(TranscriptEvent.SegmentReady(1))
        h.session.channel.trySend(TranscriptEvent.Draft(1, "hallo we"))
        runCurrent()
        verify { h.inputConnection.setComposingText("hallo we", 1) }
        assertEquals(VoicePanelState.Listening("hallo we"), h.controller.state.value)

        // Second tap releases the mic and flushes the segment.
        h.controller.toggleMic()
        runCurrent()
        assertEquals(VoicePanelState.Finishing, h.controller.state.value)
        assertEquals(1, h.session.finishCalls)

        h.session.channel.trySend(TranscriptEvent.Final(1, "Hallo Welt."))
        runCurrent()
        verify { h.inputConnection.commitText("Hallo Welt.", 1) }
        assertEquals(VoicePanelState.Committed("Hallo Welt."), h.controller.state.value)

        h.session.channel.trySend(TranscriptEvent.SegmentDone(1))
        runCurrent()
        assertEquals(VoicePanelState.Idle, h.controller.state.value)
    }

    @Test
    fun `captured audio is streamed into the session`() = runTest {
        val h = Harness(this)
        h.controller.showPanel(h.inputConnection, h.editor())

        h.controller.toggleMic()
        runCurrent()
        // FakeCapture bursts three 100 ms chunks on collect.
        assertEquals(3 * 3200, h.session.sentAudio.size(), "expected the capture burst forwarded")
    }

    @Test
    fun `a session that captures its own audio skips the mic capture pipeline`() = runTest {
        val h = Harness(this, capturesOwnAudio = true)
        h.controller.showPanel(h.inputConnection, h.editor())

        h.controller.toggleMic()
        runCurrent()

        // State machine unchanged — the panel is Listening (the session
        // records the mic in-process) but our AudioRecord path never starts.
        assertEquals(VoicePanelState.Listening(), h.controller.state.value)
        assertEquals(0, h.capture.collections, "own-capture session must not start MicAudioCapture")
        assertEquals(0, h.session.sentAudio.size())

        // Drafts, finals, and the finish flow behave exactly as before.
        h.session.channel.trySend(TranscriptEvent.Draft(1, "hallo"))
        runCurrent()
        assertEquals(VoicePanelState.Listening("hallo"), h.controller.state.value)

        h.controller.toggleMic()
        runCurrent()
        assertEquals(VoicePanelState.Finishing, h.controller.state.value)
        assertEquals(1, h.session.finishCalls)

        h.session.channel.trySend(TranscriptEvent.Final(1, "Hallo Welt."))
        h.session.channel.trySend(TranscriptEvent.SegmentDone(1))
        runCurrent()
        assertEquals(VoicePanelState.Idle, h.controller.state.value)
        assertEquals(0, h.capture.collections)
    }

    @Test
    fun `a failure clears the composing draft and lands in a retryable error`() = runTest {
        val h = Harness(this)
        h.controller.showPanel(h.inputConnection, h.editor())
        h.controller.toggleMic()
        runCurrent()

        h.session.channel.trySend(TranscriptEvent.Draft(1, "hal"))
        h.session.channel.trySend(TranscriptEvent.Failure("ws_failure", "socket reset"))
        runCurrent()

        verify { h.inputConnection.finishComposingText() }
        val state = assertInstanceOf(VoicePanelState.Error::class.java, h.controller.state.value)
        assertEquals("ws_failure", state.code)
        assertTrue(state.retryable)
    }

    @Test
    fun `retry after a dead session mints a fresh one`() = runTest {
        val sessions = mutableListOf<FakeSession>()
        val h = Harness(this, sessionFactory = { FakeSession().also { sessions.add(it) } })
        h.controller.showPanel(h.inputConnection, h.editor())

        h.controller.toggleMic()
        runCurrent()
        sessions.single().channel.trySend(TranscriptEvent.Failure("ws_failure", "gone"))
        sessions.single().channel.trySend(TranscriptEvent.Closed("error"))
        sessions.single().channel.close()
        runCurrent()
        assertInstanceOf(VoicePanelState.Error::class.java, h.controller.state.value)

        h.controller.retry()
        runCurrent()

        assertEquals(2, sessions.size, "retry after Closed must open a new session")
        assertEquals(VoicePanelState.Listening(), h.controller.state.value)
        assertEquals(1, sessions[1].startedOptions.size)
    }

    @Test
    fun `session open failure maps its api code into the error state`() = runTest {
        val h = Harness(this, sessionFactory = {
            throw io.kombify.speechkit.net.SpeechKitApiException(
                httpStatus = 0,
                code = VoicePanelController.ERROR_NOT_CONFIGURED,
                message = "no server",
            )
        })
        h.controller.showPanel(h.inputConnection, h.editor())

        h.controller.toggleMic()
        runCurrent()

        val state = assertInstanceOf(VoicePanelState.Error::class.java, h.controller.state.value)
        assertEquals(VoicePanelController.ERROR_NOT_CONFIGURED, state.code)
    }

    @Test
    fun `language chip changes apply from the next segment`() = runTest {
        val h = Harness(this)
        h.controller.showPanel(h.inputConnection, h.editor())
        h.controller.setLanguage("en")

        h.controller.toggleMic()
        runCurrent()

        assertEquals(listOf("en"), h.session.startedOptions.map { it.language })
    }

    @Test
    fun `hidePanel releases the warm session and stops capture`() = runTest {
        val h = Harness(this)
        h.controller.showPanel(h.inputConnection, h.editor())
        h.controller.toggleMic()
        runCurrent()
        val streamedBeforeHide = h.session.sentAudio.size()

        h.controller.hidePanel()
        runCurrent()

        assertTrue(h.session.closed, "hidePanel must release the per-identity session slot")
        assertEquals(VoicePanelState.Idle, h.controller.state.value)
        assertEquals(
            streamedBeforeHide, h.session.sentAudio.size(),
            "capture must stop with the panel",
        )
    }

    @Test
    fun `a mid-dictation focus change abandons the old segment cleanly`() = runTest {
        val h = Harness(this)
        h.controller.showPanel(h.inputConnection, h.editor())
        h.controller.toggleMic()
        runCurrent()
        h.session.channel.trySend(TranscriptEvent.Draft(1, "unfinished"))
        runCurrent()

        val nextField = mockk<InputConnection>(relaxed = true)
        h.controller.showPanel(nextField, h.editor())
        runCurrent()

        // The old field's composing text is finished, the segment flushed,
        // and the panel is ready for the new field.
        verify { h.inputConnection.finishComposingText() }
        assertEquals(1, h.session.finishCalls)
        assertEquals(VoicePanelState.Idle, h.controller.state.value)
    }
}
