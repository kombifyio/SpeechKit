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
 * behaves before anyone touches the setting. Each mode offers Default plus a
 * couple of glyphs that match that mode, not the same generic row for all.
 */
enum class KeyboardIconChoice(
    @DrawableRes val drawable: Int?,
    @StringRes val label: Int,
) {
    Default(null, R.string.settings_icon_default),
    Phone(R.drawable.ic_glyph_phone, R.string.settings_icon_phone),
    Chip(R.drawable.ic_glyph_chip, R.string.settings_icon_chip),
    Cloud(R.drawable.ic_glyph_cloud, R.string.settings_icon_cloud),
    Upload(R.drawable.ic_glyph_upload, R.string.settings_icon_upload),
    Wave(R.drawable.ic_glyph_wave, R.string.settings_icon_wave),
    Live(R.drawable.ic_glyph_live, R.string.settings_icon_live),
    Bars(R.drawable.ic_glyph_bars, R.string.settings_icon_bars),
    Transcript(R.drawable.ic_glyph_transcript, R.string.settings_icon_transcript),
    Captions(R.drawable.ic_glyph_captions, R.string.settings_icon_captions),
    Spark(R.drawable.ic_glyph_spark, R.string.settings_icon_spark),
    Chat(R.drawable.ic_glyph_chat, R.string.settings_icon_chat),
    Dot(R.drawable.ic_glyph_dot, R.string.settings_icon_dot),
    Ring(R.drawable.ic_glyph_ring, R.string.settings_icon_ring),
    Bolt(R.drawable.ic_glyph_bolt, R.string.settings_icon_bolt),
    Nodes(R.drawable.ic_glyph_nodes, R.string.settings_icon_nodes),
}

/** Default plus the glyphs that belong to this toolbar key. */
fun iconChoicesFor(action: String, current: KeyboardIconChoice = KeyboardIconChoice.Default): List<KeyboardIconChoice> {
    val offered = listOf(KeyboardIconChoice.Default) + when (action) {
        "SPEECHKIT_DICTATE_DEVICE" -> listOf(KeyboardIconChoice.Phone, KeyboardIconChoice.Chip)
        "SPEECHKIT_DICTATE_SERVER" -> listOf(KeyboardIconChoice.Cloud, KeyboardIconChoice.Upload)
        "SPEECHKIT_AGENT_DEEPGRAM" -> listOf(KeyboardIconChoice.Live, KeyboardIconChoice.Wave)
        "SPEECHKIT_AGENT_ASSEMBLYAI" -> listOf(KeyboardIconChoice.Transcript, KeyboardIconChoice.Captions)
        "SPEECHKIT_AGENT_GPT" -> listOf(KeyboardIconChoice.Spark, KeyboardIconChoice.Chat)
        "SPEECHKIT_COMPANION" -> listOf(KeyboardIconChoice.Nodes, KeyboardIconChoice.Bolt)
        else -> listOf(KeyboardIconChoice.Wave, KeyboardIconChoice.Spark)
    }
    return if (current in offered) offered else listOf(current) + offered
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
