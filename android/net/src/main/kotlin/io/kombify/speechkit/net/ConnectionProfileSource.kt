package io.kombify.speechkit.net

import android.content.Context

/**
 * Resolves the [ConnectionProfile] dictation consumers (Voice IME, later the
 * assistant) should use right now. Bound per product flavor (B-M2b):
 *
 * - `oss`: always [ConnectionProfile.SystemOnDevice] — the zero-config floor.
 * - `kombify`: the stored server profile when one is configured/paired,
 *   [ConnectionProfile.SystemOnDevice] otherwise.
 *
 * Deliberately a function, not a captured value: the profile is re-resolved
 * on every session open, so a server configured while the IME service stays
 * alive wins without a rebind. B-M4's SettingsRepository replaces the
 * prefs-backed resolution; this seam stays.
 */
fun interface ConnectionProfileSource {
    fun currentProfile(): ConnectionProfile
}

/**
 * kombify product order: Companion (logged-in user token) beats a typed
 * override, which beats the tester build's hosted SpeechKit, which beats
 * on-device. OSS never calls this — it stays on-device.
 */
fun resolveConnectionProfile(
    companion: ConnectionProfile.Server?,
    stored: ConnectionProfile.Server?,
    shipped: ConnectionProfile.Server?,
): ConnectionProfile = companion ?: stored ?: shipped ?: ConnectionProfile.SystemOnDevice()

/**
 * Read-only view of the server profile the app persists today — the
 * `speechkit_config` shared prefs the dev DictationTestScreen writes. B-M2b
 * persists nothing new; B-M4's SettingsRepository will own these keys.
 */
object StoredServerProfile {
    const val PREFS_NAME = "speechkit_config"
    const val KEY_SERVER_URL = "server_url"
    const val KEY_SERVER_TOKEN = "server_token"

    /** The configured server profile, or null when none is stored. */
    fun load(context: Context): ConnectionProfile.Server? {
        val prefs = context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
        val url = prefs.getString(KEY_SERVER_URL, null)?.trim().orEmpty()
        if (url.isEmpty()) return null
        val token = prefs.getString(KEY_SERVER_TOKEN, null)?.trim()?.ifEmpty { null }
        return ConnectionProfile.Server(url, token)
    }
}
