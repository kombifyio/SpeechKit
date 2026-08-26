package io.kombify.speechkit.net

import android.content.Context
import io.kombify.speechkit.domain.ConnectionMode
import io.kombify.speechkit.domain.ConnectionProfile
import io.kombify.speechkit.domain.inferConnectionMode

/**
 * Read-only view of the server profile the app persists today — the
 * `speechkit_config` shared prefs the Settings card writes.
 */
object StoredServerProfile {
    const val PREFS_NAME = "speechkit_config"
    const val KEY_SERVER_URL = "server_url"
    const val KEY_SERVER_TOKEN = "server_token"
    const val KEY_CONNECTION_MODE = "connection_mode"

    fun load(context: Context): ConnectionProfile.Server? {
        val prefs = context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
        val url = prefs.getString(KEY_SERVER_URL, null)?.trim().orEmpty()
        if (url.isEmpty()) return null
        val token = prefs.getString(KEY_SERVER_TOKEN, null)?.trim()?.ifEmpty { null }
        return ConnectionProfile.Server(url, token)
    }

    fun loadMode(context: Context): ConnectionMode? {
        val prefs = context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
        return ConnectionMode.fromWire(prefs.getString(KEY_CONNECTION_MODE, null))
    }

    fun resolvedMode(
        context: Context,
        stored: ConnectionProfile.Server?,
        shipped: ConnectionProfile.Server?,
    ): ConnectionMode = loadMode(context) ?: inferConnectionMode(stored, shipped)

    fun saveMode(context: Context, mode: ConnectionMode) {
        context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
            .edit()
            .putString(KEY_CONNECTION_MODE, mode.wire)
            .apply()
    }
}
