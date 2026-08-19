package io.kombify.speechkit.ime

import io.kombify.speechkit.net.ConnectionProfile
import io.kombify.speechkit.net.VoiceAgentController
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runTest
import org.junit.jupiter.api.Assertions.assertFalse
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

    @Test
    fun `audio hand-off clears once consumed`() = runTest(StandardTestDispatcher()) {
        val ime = controller(this, RecordingGate(granted = true))
        ime.consumeAudio()
        assertTrue(ime.audio.value == null)
    }

    private fun AudioCapture(frames: () -> Flow<ByteArray>): AudioCapture =
        object : AudioCapture {
            override fun frames(): Flow<ByteArray> = frames()
        }
}
