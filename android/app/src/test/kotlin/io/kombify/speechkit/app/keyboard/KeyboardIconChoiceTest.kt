package io.kombify.speechkit.app.keyboard

import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

/**
 * Each toolbar key offers glyphs that match that mode. A shared row of
 * waveform/bars/spark for every provider was the screenshot complaint.
 */
class KeyboardIconChoiceTest {

    @Test
    fun `each mode offers its own glyphs instead of the same shared row`() {
        val device = iconChoicesFor("SPEECHKIT_DICTATE_DEVICE")
        val server = iconChoicesFor("SPEECHKIT_DICTATE_SERVER")
        val deepgram = iconChoicesFor("SPEECHKIT_AGENT_DEEPGRAM")
        val assembly = iconChoicesFor("SPEECHKIT_AGENT_ASSEMBLYAI")
        val gpt = iconChoicesFor("SPEECHKIT_AGENT_GPT")
        val companion = iconChoicesFor("SPEECHKIT_COMPANION")

        assertTrue(device.contains(KeyboardIconChoice.Phone))
        assertTrue(device.contains(KeyboardIconChoice.Chip))
        assertTrue(server.contains(KeyboardIconChoice.Cloud))
        assertTrue(server.contains(KeyboardIconChoice.Upload))
        assertTrue(deepgram.contains(KeyboardIconChoice.Live))
        assertTrue(deepgram.contains(KeyboardIconChoice.Wave))
        assertTrue(assembly.contains(KeyboardIconChoice.Transcript))
        assertTrue(assembly.contains(KeyboardIconChoice.Captions))
        assertTrue(gpt.contains(KeyboardIconChoice.Spark))
        assertTrue(gpt.contains(KeyboardIconChoice.Chat))
        assertTrue(companion.contains(KeyboardIconChoice.Nodes))

        assertFalse(deepgram.contains(KeyboardIconChoice.Chat))
        assertFalse(gpt.contains(KeyboardIconChoice.Live))
        assertFalse(device.contains(KeyboardIconChoice.Cloud))
        assertFalse(server.contains(KeyboardIconChoice.Phone))
    }
}
