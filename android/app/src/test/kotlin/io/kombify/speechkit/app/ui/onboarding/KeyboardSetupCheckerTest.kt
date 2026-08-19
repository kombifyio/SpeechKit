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

    // Android stores the component in its short form when the manifest
    // declares the service with a relative name. A substring check against the
    // fully-qualified class name answers false for this exact string.
    @Test
    fun `recognises the short component form the system actually stores`() {
        assertTrue(
            KeyboardSetupChecker.isSpeechKitIme(
                ownPackage,
                "io.kombify.speechkit/.ime.SpeechKitVoiceImeService",
            ),
        )
    }

    @Test
    fun `recognises the fully qualified form too`() {
        assertTrue(
            KeyboardSetupChecker.isSpeechKitIme(
                ownPackage,
                "io.kombify.speechkit/io.kombify.speechkit.ime.SpeechKitVoiceImeService",
            ),
        )
    }

    // The OSS flavour ships under its own application id; its keyboard is
    // still its own keyboard.
    @Test
    fun `recognises the oss flavour under its own application id`() {
        assertTrue(
            KeyboardSetupChecker.isSpeechKitIme(
                "io.kombify.speechkit.oss",
                "io.kombify.speechkit.oss/.ime.SpeechKitVoiceImeService",
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
}
