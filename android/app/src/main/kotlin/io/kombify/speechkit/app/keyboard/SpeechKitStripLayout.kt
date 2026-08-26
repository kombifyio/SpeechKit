package io.kombify.speechkit.app.keyboard

import android.content.Context
import helium314.keyboard.latin.settings.Settings
import helium314.keyboard.latin.utils.defaultPinnedToolbarPref
import helium314.keyboard.latin.utils.prefs

/**
 * Applies the Gboard-shaped strip to HeliBoard's pinned-toolbar prefs.
 *
 * Existing tester installs already wrote a six-chip pinned list. Upgrade
 * would add new keys as disabled and leave the old ones on, so this writes
 * the two-key layout once per [LAYOUT_VERSION] instead of merging.
 */
object SpeechKitStripLayout {

    const val PREF_LAYOUT_VERSION: String = "speechkit_strip_layout"
    const val LAYOUT_VERSION: Int = 3

    fun apply(context: Context) {
        val prefs = context.prefs()
        if (prefs.getInt(PREF_LAYOUT_VERSION, 0) >= LAYOUT_VERSION) return
        prefs.edit()
            .putString(Settings.PREF_PINNED_TOOLBAR_KEYS, defaultPinnedToolbarPref)
            .putInt(PREF_LAYOUT_VERSION, LAYOUT_VERSION)
            .apply()
    }
}
