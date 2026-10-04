package io.kombify.speechkit.app.companion

import io.kombify.speechkit.coinstall.voiceagent.v1.VoiceAgentContract
import io.kombify.speechkit.coinstall.voiceagent.v1.VoiceAgentSessionRequest
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertThrows
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

/** Prevent an explicit native backend from disappearing across the attested Binder boundary. */
class CoinstallVoiceAgentOptionsTest {
    @Test
    fun `native selection survives service options and older peers refuse it`() {
        for (provider in listOf("assemblyai", "deepgram")) {
            val request = VoiceAgentSessionRequest().apply {
                mediaProvider = provider
                locale = "de-DE"
            }
            val start = request.voiceAgentStartOptions()
            assertEquals(provider, start.mediaProvider)
            assertEquals("de-DE", start.locale)
            assertFalse(VoiceAgentContract.supportsMediaProvider(2, provider))
            assertTrue(VoiceAgentContract.supportsMediaProvider(3, provider))
        }
        val legacy = VoiceAgentSessionRequest()
        assertEquals(null, legacy.voiceAgentStartOptions().mediaProvider)
        assertTrue(VoiceAgentContract.supportsMediaProvider(2, null))
        val unavailable = VoiceAgentSessionRequest().apply { mediaProvider = "unknown" }
        assertThrows(IllegalArgumentException::class.java) { unavailable.voiceAgentStartOptions() }
    }
}
