package io.kombify.speechkit.ime.ui

import io.kombify.speechkit.net.VoiceAgentUiState
import io.kombify.speechkit.voiceui.VoiceAuraState
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Test

/**
 * The orb adapter is the only mapping from session phases onto visual states.
 * A finished conversation must settle rather than go inactive so the orb stays
 * in the same closing motion on the keyboard panel and the in-app test surface.
 */
class VoiceAgentAuraTest {

    @Test
    fun `ended conversations settle instead of going inactive`() {
        assertEquals(VoiceAuraState.SETTLING, VoiceAgentUiState.Phase.Ended.toAuraState())
        assertEquals(VoiceAuraState.LISTENING, VoiceAgentUiState.Phase.Listening.toAuraState())
        assertEquals(VoiceAuraState.INACTIVE, VoiceAgentUiState.Phase.Inactive.toAuraState())
    }
}
