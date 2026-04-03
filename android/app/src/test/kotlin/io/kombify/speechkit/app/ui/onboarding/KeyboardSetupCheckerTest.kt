package io.kombify.speechkit.app.ui.onboarding

import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class KeyboardSetupCheckerTest {

    @Test
    fun `detects speechkit ime ids`() {
        assertTrue(
            KeyboardSetupChecker.isSpeechKitImeId(
                "io.kombify.speechkit/helium314.keyboard.latin.LatinIME",
            ),
        )
    }

    @Test
    fun `rejects non speechkit ime ids`() {
        assertFalse(
            KeyboardSetupChecker.isSpeechKitImeId(
                "com.example.keyboard/.ExampleIme",
            ),
        )
    }

    @Test
    fun `detects configured speechkit assistant component`() {
        assertTrue(
            KeyboardSetupChecker.isSpeechKitAssistantComponent(
                "io.kombify.speechkit/io.kombify.speechkit.assistant.service.SpeechKitAssistant",
            ),
        )
    }

    @Test
    fun `rejects unrelated assistant component`() {
        assertFalse(
            KeyboardSetupChecker.isSpeechKitAssistantComponent(
                "com.example/.AssistantService",
            ),
        )
    }
}
