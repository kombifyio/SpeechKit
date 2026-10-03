package io.kombify.speechkit.ime

import io.kombify.speechkit.audio.AudioCapture
import io.kombify.speechkit.audio.PcmPlaybackException
import io.kombify.speechkit.domain.ConnectionProfile
import io.kombify.speechkit.net.VoiceAgentController
import io.kombify.speechkit.net.VoiceAgentEvent
import io.kombify.speechkit.net.VoiceAgentSessionDriver
import io.kombify.speechkit.net.VoiceAgentStartFrame
import io.kombify.speechkit.net.VoiceAgentUiState
import io.kombify.speechkit.net.VoiceAgentSetupException
import io.kombify.speechkit.net.VoiceAgentWsClient
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.receiveAsFlow
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.launch
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.io.ByteArrayOutputStream

@OptIn(ExperimentalCoroutinesApi::class)
class ImeVoiceAgentControllerTest {

    private class RecordingGate(private val granted: Boolean) : MicPermissionGate {
        var requested = false
        override fun isGranted(): Boolean = granted
        override fun request() {
            requested = true
        }
    }

    private val silentCapture = AudioCapture { flowOf<ByteArray>() }

    private fun controller(
        scope: TestScope,
        gate: MicPermissionGate,
    ) = ImeVoiceAgentController(
        scope = scope,
        // ConnectionProfile.Local has no server, so start() fails fast without
        // any network: these tests cover the gating, not the transport.
        controllerFactory = { VoiceAgentController(ConnectionProfile.Local) },
        audioCapture = silentCapture,
        micPermission = gate,
    )

    // Opening a conversation without the microphone would connect a session
    // that can never hear anything; the user gets the permission prompt.
    @Test
    fun `asks for the microphone instead of opening a deaf session`() = runTest(
        StandardTestDispatcher(),
    ) {
        val gate = RecordingGate(granted = false)
        val ime = controller(this, gate)

        ime.start()
        advanceUntilIdle()

        assertTrue(gate.requested)
        assertFalse(ime.isLive)
    }

    @Test
    fun `a failed start leaves no live session behind`() = runTest(StandardTestDispatcher()) {
        val ime = controller(this, RecordingGate(granted = true))

        ime.start()
        advanceUntilIdle()

        // ConnectionProfile.Local cannot host a realtime conversation, so the
        // start throws and must clean up rather than strand a half-open one.
        assertFalse(ime.isLive)
        assertTrue(ime.state.value.error != null || !ime.state.value.isLive)
    }

    @Test
    fun `stopping an idle controller is harmless`() = runTest(StandardTestDispatcher()) {
        val ime = controller(this, RecordingGate(granted = true))
        ime.stop()
        advanceUntilIdle()
        assertFalse(ime.isLive)
    }

    // The permission answer arrives long after start() returned. Without a
    // resume the panel just sat at Inactive after the user granted it.
    @Test
    fun `a granted microphone opens the conversation that was deferred`() = runTest(
        StandardTestDispatcher(),
    ) {
        var opened = 0
        val gate = RecordingGate(granted = false)
        val ime = ImeVoiceAgentController(
            scope = this,
            controllerFactory = {
                opened++
                VoiceAgentController(ConnectionProfile.Local)
            },
            audioCapture = silentCapture,
            micPermission = gate,
        )

        ime.start(provider = "deepgram")
        advanceUntilIdle()
        assertEquals(0, opened)

        ime.onMicPermissionResult(true)
        advanceUntilIdle()
        assertEquals(1, opened)
    }

    @Test
    fun `a denied microphone explains itself`() = runTest(StandardTestDispatcher()) {
        val ime = controller(this, RecordingGate(granted = false))

        ime.start()
        advanceUntilIdle()
        ime.onMicPermissionResult(false)
        advanceUntilIdle()

        assertEquals(ImeVoiceAgentController.ERROR_MIC_DENIED, ime.state.value.errorCode)
        assertFalse(ime.isLive)
    }

    // Both controllers on a keyboard surface hear every permission result; the
    // one that did not ask must not open a session off someone else's grant.
    @Test
    fun `a result nobody waited for is ignored`() = runTest(StandardTestDispatcher()) {
        var opened = 0
        val ime = ImeVoiceAgentController(
            scope = this,
            controllerFactory = {
                opened++
                VoiceAgentController(ConnectionProfile.Local)
            },
            audioCapture = silentCapture,
            micPermission = RecordingGate(granted = true),
        )

        ime.onMicPermissionResult(true)
        advanceUntilIdle()

        assertEquals(0, opened)
    }

    @Test
    fun `one hold-to-talk turn streams capture and receives the agent answer`() = runTest(
        StandardTestDispatcher(),
    ) {
        val driver = FakeDriver()
        val capture = AudioCapture { flowOf(byteArrayOf(1, 2, 3, 4)) }
        val ime = ImeVoiceAgentController(
            scope = this,
            controllerFactory = { driver },
            audioCapture = capture,
            micPermission = RecordingGate(granted = true),
        )

        ime.start(provider = "deepgram")
        advanceUntilIdle()
        assertTrue(ime.isLive)
        assertEquals("deepgram", driver.startedProvider)

        ime.beginTurn()
        advanceUntilIdle()
        assertTrue(driver.sentAudio.size() > 0)

        ime.endTurn()
        advanceUntilIdle()
        assertTrue(driver.endTurnCalls == 1)
        assertEquals("hello there", ime.state.value.agentText)
        assertEquals(
            byteArrayOf(9, 8, 7, 6).toList(),
            consumeFirstAudio(ime).toList(),
        )
        ime.stop()
        advanceUntilIdle()
        assertFalse(ime.isLive)
    }

    private class FakeDriver(private val startFailure: Throwable? = null) : VoiceAgentSessionDriver {
        val sentAudio = ByteArrayOutputStream()
        var endTurnCalls = 0
        var startedProvider: String? = null
        var stopped = false
        private val events = Channel<VoiceAgentEvent>(Channel.UNLIMITED)
        private val _state = MutableStateFlow(VoiceAgentUiState())
        override val state: StateFlow<VoiceAgentUiState> = _state.asStateFlow()

        override suspend fun start(options: VoiceAgentStartFrame): Flow<VoiceAgentEvent> {
            startFailure?.let { throw it }
            startedProvider = options.provider
            _state.value = VoiceAgentUiState(phase = VoiceAgentUiState.Phase.Listening)
            return events.receiveAsFlow()
        }

        override fun accept(event: VoiceAgentEvent) {
            when (event) {
                is VoiceAgentEvent.Transcript -> if (!event.input) {
                    _state.value = _state.value.copy(agentText = event.text)
                }
                is VoiceAgentEvent.State -> _state.value =
                    _state.value.copy(phase = VoiceAgentUiState.Phase.Speaking)
                else -> Unit
            }
        }

        override suspend fun sendAudio(pcm: ByteArray) {
            sentAudio.write(pcm)
        }

        override suspend fun endTurn() {
            endTurnCalls += 1
            events.send(VoiceAgentEvent.Transcript(input = false, text = "hello there", done = true))
            events.send(VoiceAgentEvent.Audio(byteArrayOf(9, 8, 7, 6)))
            events.send(VoiceAgentEvent.State("speaking"))
        }

        override suspend fun stop() {
            stopped = true
            events.close()
        }

        suspend fun audio(pcm: ByteArray) { events.send(VoiceAgentEvent.Audio(pcm)) }
    }

    // A missing server and a dead server produced the same blank panel; the
    // code is what lets the surface tell them apart.
    @Test
    fun `a profile without a server is reported as such`() = runTest(StandardTestDispatcher()) {
        val ime = controller(this, RecordingGate(granted = true))

        ime.start()
        advanceUntilIdle()

        assertEquals(ImeVoiceAgentController.ERROR_NO_SERVER, ime.state.value.errorCode)
        assertEquals(VoiceAgentUiState.Phase.Inactive, ime.state.value.phase)
    }

    @Test
    fun `remote setup failures retain their code instead of claiming no server`() = runTest(StandardTestDispatcher()) {
        listOf(VoiceAgentWsClient.SETUP_TIMEOUT_CODE, "auth_expired", "provider_unavailable").forEach { code ->
            val driver = FakeDriver(VoiceAgentSetupException(code))
            val ime = ImeVoiceAgentController(this, { driver }, silentCapture, RecordingGate(true))
            ime.start()
            advanceUntilIdle()
            assertEquals(code, ime.state.value.errorCode)
            assertEquals(VoiceAgentUiState.Phase.Ended, ime.state.value.phase)
            assertFalse(ime.isLive)
            assertTrue(driver.stopped)
        }
    }

    @Test
    fun `playback capacity includes the active frame and stops instead of losing audio`() = runTest(StandardTestDispatcher()) {
        val driver = FakeDriver()
        val ime = ImeVoiceAgentController(this, { driver }, silentCapture, RecordingGate(true))
        val started = CompletableDeferred<Unit>()
        val blocked = CompletableDeferred<Unit>()
        var flushed = false
        backgroundScope.launch { ime.consumeAudio(flush = { flushed = true }) {
            started.complete(Unit)
            blocked.await()
        } }
        ime.start()
        runCurrent()
        driver.audio(ByteArray(48_000))
        runCurrent()
        started.await()
        driver.audio(ByteArray(48_000))
        driver.audio(ByteArray(480))
        runCurrent()
        assertEquals(VoiceAgentWsClient.OVERFLOW_CODE, ime.state.value.errorCode)
        assertFalse(ime.isLive)
        assertTrue(driver.stopped)
        assertTrue(flushed)
    }

    @Test
    fun `playback driver failure is a terminal outcome with resource cleanup`() = runTest(StandardTestDispatcher()) {
        val driver = FakeDriver()
        val ime = ImeVoiceAgentController(this, { driver }, silentCapture, RecordingGate(true))
        var released = false
        val consumer = backgroundScope.launch { ime.consumeAudio(flush = { released = true }) {
            throw PcmPlaybackException("playback_write_failed")
        } }
        ime.start()
        runCurrent()
        driver.audio(ByteArray(480))
        consumer.join()
        runCurrent()
        assertEquals("playback_write_failed", ime.state.value.errorCode)
        assertEquals(VoiceAgentUiState.Phase.Ended, ime.state.value.phase)
        assertFalse(ime.isLive)
        assertTrue(driver.stopped)
        assertTrue(released)
    }

    private suspend fun TestScope.consumeFirstAudio(ime: ImeVoiceAgentController): ByteArray {
        val played = CompletableDeferred<ByteArray>()
        val consumer = backgroundScope.launch { ime.consumeAudio { played.complete(it) } }
        return try { played.await() } finally { consumer.cancel() }
    }

    private fun AudioCapture(frames: () -> Flow<ByteArray>): AudioCapture =
        object : AudioCapture {
            override fun frames(): Flow<ByteArray> = frames()
        }
}
