package io.kombify.speechkit.net

import io.kombify.speechkit.domain.ConnectionProfile
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

/**
 * The controller is what both Android surfaces render from, so these pin the
 * folding rules rather than the transport: a surface must never be able to
 * show a live conversation as ended, or an interrupted answer as complete.
 */
class VoiceAgentControllerTest {

    private fun controller() = VoiceAgentController(ConnectionProfile.Local)

    @Test
    fun `starts inactive and reports not live`() {
        val state = controller().state.value
        assertEquals(VoiceAgentUiState.Phase.Inactive, state.phase)
        assertFalse(state.isLive)
    }

    @Test
    fun `maps every session state onto a render phase`() {
        val c = controller()
        val expected = mapOf(
            VoiceAgentStates.CONNECTING to VoiceAgentUiState.Phase.Connecting,
            VoiceAgentStates.LISTENING to VoiceAgentUiState.Phase.Listening,
            VoiceAgentStates.PROCESSING to VoiceAgentUiState.Phase.Processing,
            VoiceAgentStates.SPEAKING to VoiceAgentUiState.Phase.Speaking,
            VoiceAgentStates.DEACTIVATING to VoiceAgentUiState.Phase.Ended,
            VoiceAgentStates.INACTIVE to VoiceAgentUiState.Phase.Ended,
        )
        expected.forEach { (state, phase) ->
            c.accept(VoiceAgentEvent.State(state))
            assertEquals(phase, c.state.value.phase, state)
        }
    }

    // A reconnecting session is still a session. Rendering it as ended would
    // tell the user the conversation died while the server is bringing it back.
    @Test
    fun `recovering stays live`() {
        val c = controller()
        c.accept(VoiceAgentEvent.State(VoiceAgentStates.RECOVERING))
        assertTrue(c.state.value.isLive)
    }

    @Test
    fun `unknown states hold the current phase instead of resetting it`() {
        val c = controller()
        c.accept(VoiceAgentEvent.State(VoiceAgentStates.LISTENING))
        c.accept(VoiceAgentEvent.State("a_state_from_a_newer_server"))
        assertEquals(VoiceAgentUiState.Phase.Listening, c.state.value.phase)
    }

    @Test
    fun `keeps the two sides of the conversation apart`() {
        val c = controller()
        c.accept(VoiceAgentEvent.Transcript(input = true, text = "wie spät", done = false))
        c.accept(VoiceAgentEvent.Transcript(input = false, text = "es ist drei", done = false))
        assertEquals("wie spät", c.state.value.userText)
        assertEquals("es ist drei", c.state.value.agentText)
    }

    @Test
    fun `an interrupted answer does not bleed into the next one`() {
        val c = controller()
        c.accept(VoiceAgentEvent.Transcript(input = false, text = "der Wetter", done = false))
        c.accept(VoiceAgentEvent.Interrupted)
        assertEquals("", c.state.value.agentText)
    }

    @Test
    fun `a failure is surfaced without ending the conversation`() {
        val c = controller()
        c.accept(VoiceAgentEvent.State(VoiceAgentStates.LISTENING))
        c.accept(VoiceAgentEvent.Failure("provider_hiccup", "transient"))
        assertEquals("transient", c.state.value.error)
        assertTrue(c.state.value.isLive)
    }

    @Test
    fun `closing ends the conversation`() {
        val c = controller()
        c.accept(VoiceAgentEvent.State(VoiceAgentStates.SPEAKING))
        c.accept(VoiceAgentEvent.Closed(VoiceAgentEndReasons.IDLE))
        assertEquals(VoiceAgentUiState.Phase.Ended, c.state.value.phase)
        assertFalse(c.state.value.isLive)
    }

    @Test
    fun `audio and tool calls leave the render state untouched`() {
        val c = controller()
        c.accept(VoiceAgentEvent.State(VoiceAgentStates.SPEAKING))
        val before = c.state.value
        c.accept(VoiceAgentEvent.Audio(byteArrayOf(1, 2, 3)))
        c.accept(VoiceAgentEvent.ToolCall("t1", "weather", emptyMap()))
        assertEquals(before, c.state.value)
        assertNull(c.state.value.error)
    }
}
