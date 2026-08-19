package io.kombify.speechkit.stt

import io.kombify.speechkit.stt.system.SystemSpeechRecognizerSession
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

/**
 * The multilanguage contract, as a gate.
 *
 * SpeechKit does not narrow speech to one language: a pinned language breaks
 * switching language mid-conversation and speaking different languages with
 * different people in one session, and it is a silent data-loss bug (the same
 * English audio yields a zero-length transcript when pinned to German, with
 * HTTP 200 and no error).
 *
 * This has regressed repeatedly, so it is pinned by tests rather than by
 * intent. Both directions are covered: an unconfigured request must carry
 * multilanguage, and a deliberate user pin must still survive — a gate that
 * only forbade the field would break the supported override.
 */
class MultilanguageContractTest {

    @Test
    fun `an unconfigured request is multilanguage`() {
        assertEquals(LANGUAGE_MULTI, TranscribeOpts().language)
        assertTrue(isMultilanguage(TranscribeOpts().language))
    }

    @Test
    fun `no locale is ever inferred as a default`() {
        // Guards the exact regression: TranscribeOpts once defaulted to "de",
        // so every call site that omitted the argument silently pinned German.
        val default = TranscribeOpts().language
        assertFalse(
            default.equals("de", ignoreCase = true) || default.startsWith("de-", ignoreCase = true),
            "default language must not be a locale, got '$default'",
        )
    }

    @Test
    fun `an explicit pin survives`() {
        assertEquals("de", TranscribeOpts(language = "de").language)
        assertFalse(isMultilanguage("de"))
    }

    @Test
    fun `blank and auto count as multilanguage`() {
        assertTrue(isMultilanguage(null))
        assertTrue(isMultilanguage(""))
        assertTrue(isMultilanguage("  "))
        assertTrue(isMultilanguage("auto"))
        assertTrue(isMultilanguage("MULTI"))
    }

    /**
     * Per-provider translation, not one global string. The Android platform
     * recognizer expresses multilanguage by not pinning a tag at all; passing
     * the literal sentinel through would set an invalid BCP-47 value.
     */
    @Test
    fun `the platform recognizer expresses multilanguage as no language tag`() {
        assertNull(SystemSpeechRecognizerSession.toLanguageTag(LANGUAGE_MULTI))
        assertNull(SystemSpeechRecognizerSession.toLanguageTag(null))
        assertNull(SystemSpeechRecognizerSession.toLanguageTag("auto"))
        assertNull(SystemSpeechRecognizerSession.toLanguageTag(""))
    }

    @Test
    fun `the platform recognizer still expands an explicit pin`() {
        assertEquals("de-DE", SystemSpeechRecognizerSession.toLanguageTag("de"))
        assertEquals("en-US", SystemSpeechRecognizerSession.toLanguageTag("en"))
        assertEquals("fr-CA", SystemSpeechRecognizerSession.toLanguageTag("fr-CA"))
        assertEquals("pt-BR", SystemSpeechRecognizerSession.toLanguageTag("pt_BR"))
    }
}
