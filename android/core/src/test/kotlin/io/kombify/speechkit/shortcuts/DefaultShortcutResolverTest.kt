package io.kombify.speechkit.shortcuts

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNotNull
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Test

class DefaultShortcutResolverTest {

    private val resolver = DefaultShortcutResolver()

    @Test
    fun `resolves german quick note prefix`() {
        val result = resolver.resolve("Schnelle Notiz einkaufen gehen")
        assertNotNull(result)
        assertEquals(ShortcutAction.QUICK_NOTE, result?.action)
        assertEquals("einkaufen gehen", result?.remainingText)
    }

    @Test
    fun `resolves english quick note prefix`() {
        val result = resolver.resolve("Quick Note buy groceries")
        assertNotNull(result)
        assertEquals(ShortcutAction.QUICK_NOTE, result?.action)
        assertEquals("buy groceries", result?.remainingText)
    }

    @Test
    fun `resolves copy last`() {
        val result = resolver.resolve("copy last")
        assertNotNull(result)
        assertEquals(ShortcutAction.COPY_LAST, result?.action)
        assertEquals("", result?.remainingText)
    }

    @Test
    fun `resolves summarize with remaining text`() {
        val result = resolver.resolve("Zusammenfassen den letzten Absatz")
        assertNotNull(result)
        assertEquals(ShortcutAction.SUMMARIZE, result?.action)
        assertEquals("den letzten Absatz", result?.remainingText)
    }

    @Test
    fun `returns null for unknown text`() {
        val result = resolver.resolve("Hallo Welt wie geht es dir")
        assertNull(result)
    }

    @Test
    fun `handles whitespace gracefully`() {
        val result = resolver.resolve("  Notiz  etwas Wichtiges  ")
        assertNotNull(result)
        assertEquals(ShortcutAction.QUICK_NOTE, result?.action)
        assertEquals("etwas Wichtiges", result?.remainingText)
    }

    @Test
    fun `case insensitive matching`() {
        val result = resolver.resolve("QUICK NOTE important thing")
        assertNotNull(result)
        assertEquals(ShortcutAction.QUICK_NOTE, result?.action)
    }

    @Test
    fun `resolves open home`() {
        val result = resolver.resolve("open home")
        assertNotNull(result)
        assertEquals(ShortcutAction.OPEN_HOME, result?.action)
    }

    @Test
    fun `resolves open library`() {
        val result = resolver.resolve("oeffne bibliothek")
        assertNotNull(result)
        assertEquals(ShortcutAction.OPEN_LIBRARY, result?.action)
    }
}
