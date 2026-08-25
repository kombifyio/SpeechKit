package io.kombify.speechkit.app.keyboard

import io.kombify.speechkit.net.ConnectionProfile
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

/**
 * The row is the only thing standing between a user and an action that cannot
 * work: the Voice Agent throws for any profile without a server before a frame
 * moves, and the oss flavor never resolves one. A button that looks live and
 * then fails reads as a broken keyboard, so which profile unlocks what is
 * worth pinning.
 */
class KeyboardActionRowTest {

    private fun blocker(profile: ConnectionProfile, action: KeyboardAction) =
        keyboardActionRowItems(profile).single { it.action == action }.blocker

    @Test
    fun `on-device dictation is offered on every profile`() {
        val profiles = listOf(
            ConnectionProfile.SystemOnDevice(),
            ConnectionProfile.Server("https://speechkit.example.com"),
            ConnectionProfile.Byok,
            ConnectionProfile.Local,
        )
        profiles.forEach { profile ->
            assertNull(
                blocker(profile, KeyboardAction.OnDeviceDictation),
                profile.toString(),
            )
        }
    }

    @Test
    fun `a paired server unlocks server dictation and every agent`() {
        val profile = ConnectionProfile.Server("https://speechkit.example.com", "token")
        val live = keyboardActionRowItems(profile).filter { it.enabled }.map { it.action }
        assertEquals(
            listOf(
                KeyboardAction.OnDeviceDictation,
                KeyboardAction.ServerDictation,
                KeyboardAction.AgentDeepgram,
                KeyboardAction.AgentAssemblyAi,
                KeyboardAction.AgentOpenAi,
            ),
            live,
        )
    }

    @Test
    fun `without a server every network action states why it is blocked`() {
        listOf(ConnectionProfile.SystemOnDevice(), ConnectionProfile.Byok, ConnectionProfile.Local)
            .forEach { profile ->
                listOf(
                    KeyboardAction.ServerDictation,
                    KeyboardAction.AgentDeepgram,
                    KeyboardAction.AgentAssemblyAi,
                    KeyboardAction.AgentOpenAi,
                ).forEach { action ->
                    assertEquals(
                        KeyboardActionBlocker.NoServer,
                        blocker(profile, action),
                        "$profile / $action",
                    )
                }
            }
    }

    // The hand-off is a written contract and a fixture today: no AIDL, no
    // binding, no package visibility. The button says so rather than pretending.
    @Test
    fun `Companion stays blocked even with a server paired`() {
        val profile = ConnectionProfile.Server("https://speechkit.example.com")
        assertEquals(
            KeyboardActionBlocker.NoCompanion,
            blocker(profile, KeyboardAction.CompanionApp),
        )
    }

    // The strip is one line high. Rendering a sentence per distinct reason put
    // two of them under the chips whenever no server was paired, which is the
    // state every fresh install starts in.
    @Test
    fun `the row states one blocking reason at a time`() {
        val unpaired = keyboardActionRowItems(ConnectionProfile.Local)
        assertEquals(2, unpaired.mapNotNull { it.blocker }.distinct().size)
        assertEquals(KeyboardActionBlocker.NoServer, keyboardActionRowBlocker(unpaired))
    }

    // Pairing a server is what the user can act on, so it outranks a hand-off
    // that is not built yet; once it is paired the remaining reason is the
    // Companion one.
    @Test
    fun `a paired server leaves only the Companion reason`() {
        val paired = keyboardActionRowItems(ConnectionProfile.Server("https://speechkit.example.com"))
        assertEquals(KeyboardActionBlocker.NoCompanion, keyboardActionRowBlocker(paired))
    }

    @Test
    fun `a row with nothing blocked says nothing`() {
        assertNull(keyboardActionRowBlocker(listOf(KeyboardActionItem(KeyboardAction.OnDeviceDictation))))
    }

    // The names the server normalises. A typo here becomes a runtime factory
    // failure on the server, not a validation error, so it would only show up
    // as "the agent does not answer".
    @Test
    fun `voice agent strip keys open the in-IME panel`() {
        assertTrue(KeyboardAction.AgentDeepgram.opensVoiceAgentInIme())
        assertTrue(KeyboardAction.AgentAssemblyAi.opensVoiceAgentInIme())
        assertTrue(KeyboardAction.AgentOpenAi.opensVoiceAgentInIme())
        assertFalse(KeyboardAction.OnDeviceDictation.opensVoiceAgentInIme())
        assertFalse(KeyboardAction.ServerDictation.opensVoiceAgentInIme())
        assertFalse(KeyboardAction.CompanionApp.opensVoiceAgentInIme())
    }

    @Test
    fun `each agent action carries the provider name it requests`() {
        assertEquals("deepgram", KeyboardAction.AgentDeepgram.agentProvider)
        assertEquals("assemblyai", KeyboardAction.AgentAssemblyAi.agentProvider)
        assertEquals("openai", KeyboardAction.AgentOpenAi.agentProvider)
        assertTrue(
            listOf(
                KeyboardAction.OnDeviceDictation,
                KeyboardAction.ServerDictation,
                KeyboardAction.CompanionApp,
            ).all { it.agentProvider == null },
        )
    }
}
