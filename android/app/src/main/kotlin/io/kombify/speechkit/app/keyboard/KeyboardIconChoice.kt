package io.kombify.speechkit.app.keyboard

import android.content.Context
import androidx.annotation.DrawableRes
import androidx.annotation.StringRes
import io.kombify.speechkit.R

/**
 * The glyph a SpeechKit toolbar key is drawn with.
 *
 * Only SpeechKit's own symbols are offered. Provider logos were considered and
 * deliberately left out: the keyboard APK is GPL-3.0 as a whole, and shipping
 * third-party marks inside it is a trademark question rather than a design one.
 *
 * [Default] means "whatever the fork ships for this key", which is how a key
 * behaves before anyone touches the setting.
 */
enum class KeyboardIconChoice(
    @DrawableRes val drawable: Int?,
    @StringRes val label: Int,
) {
    Default(null, R.string.settings_icon_default),
    Wave(R.drawable.ic_glyph_wave, R.string.settings_icon_wave),
    Bars(R.drawable.ic_glyph_bars, R.string.settings_icon_bars),
    Spark(R.drawable.ic_glyph_spark, R.string.settings_icon_spark),
    Dot(R.drawable.ic_glyph_dot, R.string.settings_icon_dot),
    Ring(R.drawable.ic_glyph_ring, R.string.settings_icon_ring),
    Bolt(R.drawable.ic_glyph_bolt, R.string.settings_icon_bolt),
}

/**
 * Per-key glyph choices, stored as plain strings so an unknown value from a
 * future or downgraded build reads back as [KeyboardIconChoice.Default] rather
 * than crashing the keyboard on a key build.
 */
object KeyboardIconPreferences {

    const val PREFS_NAME: String = "speechkit_keyboard_icons"

    fun choice(context: Context, action: String): KeyboardIconChoice {
        val stored = context
            .getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
            .getString(action, null)
            ?: return KeyboardIconChoice.Default
        return KeyboardIconChoice.entries.firstOrNull { it.name == stored }
            ?: KeyboardIconChoice.Default
    }

    fun setChoice(context: Context, action: String, choice: KeyboardIconChoice) {
        context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
            .edit()
            .putString(action, choice.name)
            .apply()
    }
}
