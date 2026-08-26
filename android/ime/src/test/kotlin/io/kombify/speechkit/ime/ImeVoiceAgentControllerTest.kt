package io.kombify.speechkit.ime

import io.kombify.speechkit.audio.AudioCapture
import io.kombify.speechkit.domain.ConnectionProfile
import io.kombify.speechkit.net.VoiceAgentController
import io.kombify.speechkit.net.VoiceAgentUiState
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.withTimeoutOrNull
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

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

    // Barge-in drops what is queued behind the speaker. Nothing was ever
    // queued here, so the drop has to be a no-op rather than leave a frame
    // behind for the next conversation to play.
    @Test
    fun `a controller that never spoke queues no agent audio`() = runTest(
        StandardTestDispatcher(),
    ) {
        val ime = controller(this, RecordingGate(granted = true))
        ime.discardPendingAudio()
        assertNull(withTimeoutOrNull(1_000) { ime.audio.first() })
    }

    private fun AudioCapture(frames: () -> Flow<ByteArray>): AudioCapture =
        object : AudioCapture {
            override fun frames(): Flow<ByteArray> = frames()
        }
}
