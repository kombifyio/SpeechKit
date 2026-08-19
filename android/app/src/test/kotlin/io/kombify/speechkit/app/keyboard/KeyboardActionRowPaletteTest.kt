package io.kombify.speechkit.app.keyboard

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

/**
 * The action row sits outside the keyboard frame the fork colours, so nothing
 * in the view tree tells it what the keys look like. Composed under a plain
 * MaterialTheme it painted the default light palette on top of every dark
 * keyboard theme — a white strip over black keys. This pins the rule that
 * replaced it: whatever the keyboard is painted in decides the strip.
 */
class KeyboardActionRowPaletteTest {

    @Test
    fun `a dark keyboard gets light content`() {
        val palette = keyboardActionRowPalette(BLACK_KEYBOARD, nightMode = false)
        assertTrue(palette.dark)
        assertEquals(BLACK_KEYBOARD, palette.surface)
        assertTrue(luminance(palette.content) > luminance(palette.surface))
    }

    @Test
    fun `a light keyboard gets dark content`() {
        val palette = keyboardActionRowPalette(WHITE_KEYBOARD, nightMode = true)
        assertFalse(palette.dark)
        assertEquals(WHITE_KEYBOARD, palette.surface)
        assertTrue(luminance(palette.content) < luminance(palette.surface))
    }

    // The keyboard paints its background on the input view's first layout,
    // which is after the hook that mounts the row, so the very first sample is
    // always missing. Night mode stands in for it rather than defaulting to
    // white.
    @Test
    fun `with no sample yet the system night mode decides`() {
        assertTrue(keyboardActionRowPalette(null, nightMode = true).dark)
        assertFalse(keyboardActionRowPalette(null, nightMode = false).dark)
    }

    // Mid grey is the case a naive threshold gets wrong: dark text on it is
    // the more legible of the two, and a "not pure white, so treat as dark"
    // rule would pick the other one.
    @Test
    fun `mid grey keeps the more legible content colour`() {
        val palette = keyboardActionRowPalette(MID_GREY_KEYBOARD, nightMode = true)
        assertFalse(palette.dark)
        assertTrue(luminance(palette.content) < luminance(palette.surface))
    }

    private fun luminance(color: Int): Double {
        fun channel(shift: Int): Double {
            val v = (color shr shift and 0xFF) / 255.0
            return if (v <= 0.03928) v / 12.92 else Math.pow((v + 0.055) / 1.055, 2.4)
        }
        return 0.2126 * channel(16) + 0.7152 * channel(8) + 0.0722 * channel(0)
    }

    private companion object {
        val BLACK_KEYBOARD = 0xFF121212.toInt()
        val WHITE_KEYBOARD = 0xFFFFFFFF.toInt()
        val MID_GREY_KEYBOARD = 0xFF808080.toInt()
    }
}
