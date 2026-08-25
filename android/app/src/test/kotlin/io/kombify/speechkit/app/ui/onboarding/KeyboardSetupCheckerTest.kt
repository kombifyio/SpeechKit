package io.kombify.speechkit.app.ui.onboarding

import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

/**
 * The component strings below are copied verbatim from a running Pixel 8
 * emulator (`settings get secure default_input_method` and
 * `enabled_input_methods`), because the defect this covers was invisible in
 * every other kind of test: the code compiled, the logic read correctly, and
 * onboarding still could not be completed on a device.
 */
class KeyboardSetupCheckerTest {

    private val ownPackage = "io.kombify.speechkit"

    @Test
    fun `recognises the HeliBoard keyboard this APK ships`() {
        assertTrue(
            KeyboardSetupChecker.isSpeechKitIme(
                ownPackage,
                "io.kombify.speechkit/helium314.keyboard.latin.LatinIME",
            ),
        )
    }

    @Test
    fun `recognises the oss flavour under its own application id`() {
        assertTrue(
            KeyboardSetupChecker.isSpeechKitIme(
                "io.kombify.speechkit.oss",
                "io.kombify.speechkit.oss/helium314.keyboard.latin.LatinIME",
            ),
        )
    }

    @Test
    fun `does not treat the voice-only IME as the typing keyboard`() {
        assertFalse(
            KeyboardSetupChecker.isSpeechKitIme(
                ownPackage,
                "io.kombify.speechkit/.ime.SpeechKitVoiceImeService",
            ),
        )
        assertFalse(
            KeyboardSetupChecker.isSpeechKitIme(
                ownPackage,
                "io.kombify.speechkit/io.kombify.speechkit.ime.SpeechKitVoiceImeService",
            ),
        )
    }

    // Keeps the earlier defect fixed: onboarding used to report success for
    // anyone who merely had some other keyboard installed.
    @Test
    fun `does not accept a keyboard from another application`() {
        assertFalse(
            KeyboardSetupChecker.isSpeechKitIme(
                ownPackage,
                "helium314.keyboard/.latin.LatinIME",
            ),
        )
        assertFalse(
            KeyboardSetupChecker.isSpeechKitIme(
                ownPackage,
                "com.google.android.inputmethod.latin/com.android.inputmethod.latin.LatinIME",
            ),
        )
    }

    @Test
    fun `treats a missing or unparseable setting as not selected`() {
        assertFalse(KeyboardSetupChecker.isSpeechKitIme(ownPackage, null))
        assertFalse(KeyboardSetupChecker.isSpeechKitIme(ownPackage, ""))
        assertFalse(KeyboardSetupChecker.isSpeechKitIme(ownPackage, "io.kombify.speechkit"))
    }

    @Test
    fun `setup is complete once the typing keyboard is enabled and selected`() {
        val latin = "io.kombify.speechkit/helium314.keyboard.latin.LatinIME"
        assertTrue(
            KeyboardSetupChecker.isSetupComplete(
                ownPackage,
                listOf(latin),
                latin,
            ),
        )
    }

    @Test
    fun `setup complete does not depend on the system assistant role`() {
        val latin = "io.kombify.speechkit/helium314.keyboard.latin.LatinIME"
        // Completeness takes only IME ids. There is no assistant argument: if
        // setup still required ROLE_ASSISTANT, this call would not compile or
        // would return false without an extra held-role flag.
        assertTrue(
            KeyboardSetupChecker.isSetupComplete(
                ownPackage,
                enabledInputMethodIds = listOf(latin),
                selectedInputMethodId = latin,
            ),
        )
        assertFalse(
            KeyboardSetupChecker.isSetupComplete(
                ownPackage,
                enabledInputMethodIds = listOf(
                    "io.kombify.speechkit/.ime.SpeechKitVoiceImeService",
                ),
                selectedInputMethodId = "io.kombify.speechkit/.ime.SpeechKitVoiceImeService",
            ),
        )
    }

    @Test
    fun `setup is incomplete when another keyboard is selected`() {
        val latin = "io.kombify.speechkit/helium314.keyboard.latin.LatinIME"
        assertFalse(
            KeyboardSetupChecker.isSetupComplete(
                ownPackage,
                listOf(latin),
                "com.google.android.inputmethod.latin/com.android.inputmethod.latin.LatinIME",
            ),
        )
    }
}
