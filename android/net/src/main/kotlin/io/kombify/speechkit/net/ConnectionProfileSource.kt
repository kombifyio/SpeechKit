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
    const val KEY_CLOUD_URL = "cloud_server_url"
    const val KEY_CLOUD_TOKEN = "cloud_server_token"
    const val KEY_CLOUD_REFRESH = "cloud_refresh_token"
    const val KEY_CLOUD_EXPIRES_AT = "cloud_access_expires_at"

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

    fun saveSelfHost(context: Context, url: String, token: String?) {
        context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
            .edit()
            .putString(KEY_SERVER_URL, url.trim())
            .putString(KEY_SERVER_TOKEN, token?.trim().orEmpty())
            .putString(KEY_CONNECTION_MODE, ConnectionMode.SELF_HOST.wire)
            .apply()
    }

    fun loadCloud(context: Context): ConnectionProfile.Server? {
        val prefs = context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
        val url = prefs.getString(KEY_CLOUD_URL, null)?.trim().orEmpty()
        if (url.isEmpty()) return null
        val token = prefs.getString(KEY_CLOUD_TOKEN, null)?.trim()?.ifEmpty { null }
        return ConnectionProfile.Server(url, token)
    }

    fun loadCloudRefreshToken(context: Context): String? =
        context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
            .getString(KEY_CLOUD_REFRESH, null)?.trim()?.ifEmpty { null }

    /**
     * @param expiresAtEpochMs when the access token stops being valid; 0 when
     *   the source did not say (Companion hands out a bearer with no expiry).
     */
    fun saveKombifyCloud(
        context: Context,
        url: String,
        token: String?,
        refreshToken: String? = null,
        expiresAtEpochMs: Long = 0L,
    ) = synchronized(cloudLock) {
        context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
            .edit()
            .putString(KEY_CLOUD_URL, url.trim())
            .putString(KEY_CLOUD_TOKEN, token?.trim().orEmpty())
            .putString(KEY_CLOUD_REFRESH, refreshToken?.trim().orEmpty())
            .putLong(KEY_CLOUD_EXPIRES_AT, expiresAtEpochMs)
            .putString(KEY_CONNECTION_MODE, ConnectionMode.KOMBIFY_CLOUD.wire)
            .apply()
    }

    /**
     * A renewed access token from Companion replaces the stored one; this
     * device's own refresh token stays, so its own sign-in keeps working
     * if Companion later has nothing to give.
     */
    fun replaceCloudAccess(
        context: Context,
        url: String,
        token: String,
        expiresAtEpochMs: Long = 0L,
    ) = synchronized(cloudLock) {
        context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
            .edit()
            .putString(KEY_CLOUD_URL, url.trim())
            .putString(KEY_CLOUD_TOKEN, token.trim())
            .putLong(KEY_CLOUD_EXPIRES_AT, expiresAtEpochMs)
            .putString(KEY_CONNECTION_MODE, ConnectionMode.KOMBIFY_CLOUD.wire)
            .apply()
    }

    /**
     * Stores the tokens of a refresh, but only while a Cloud session is still
     * connected: the URL key is the proof. A rotation that finishes after a
     * Disconnect must not bring tokens back that have no URL. Shares its lock
     * with [clearCloud], so the check and the write cannot interleave with it.
     * Returns false when nothing was written.
     */
    internal fun saveCloudTokensIfConnected(
        context: Context,
        accessToken: String,
        refreshToken: String?,
        expiresAtEpochMs: Long,
    ): Boolean = synchronized(cloudLock) {
        val prefs = context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
        if (prefs.getString(KEY_CLOUD_URL, null).isNullOrBlank()) return@synchronized false
        val edit = prefs.edit()
            .putString(KEY_CLOUD_TOKEN, accessToken.trim())
            .putLong(KEY_CLOUD_EXPIRES_AT, expiresAtEpochMs)
        refreshToken?.trim()?.takeIf { it.isNotEmpty() }?.let {
            edit.putString(KEY_CLOUD_REFRESH, it)
        }
        // commit(): a process death right after a rotation must not lose the
        // new refresh token, the old one is already spent.
        edit.commit()
        true
    }

    /** Signs the Cloud session out. The mode stays, so Settings offers Connect again. */
    fun clearCloud(context: Context) = synchronized(cloudLock) {
        context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
            .edit()
            .remove(KEY_CLOUD_URL)
            .remove(KEY_CLOUD_TOKEN)
            .remove(KEY_CLOUD_REFRESH)
            .remove(KEY_CLOUD_EXPIRES_AT)
            .apply()
    }

    // One lock for every writer of the Cloud session keys.
    private val cloudLock = Any()
}

/** [CloudSessionStore] over the `speechkit_config` prefs the Connect flow writes. */
class StoredCloudSession(private val context: Context) : CloudSessionStore {
    private val prefs
        get() = context.getSharedPreferences(StoredServerProfile.PREFS_NAME, Context.MODE_PRIVATE)

    override fun load(): CloudSession? {
        val profile = StoredServerProfile.loadCloud(context) ?: return null
        val access = profile.bearerToken ?: return null
        return CloudSession(
            accessToken = access,
            refreshToken = StoredServerProfile.loadCloudRefreshToken(context),
            expiresAtEpochMs = prefs.getLong(StoredServerProfile.KEY_CLOUD_EXPIRES_AT, 0L),
        )
    }

    override fun save(session: CloudSession) {
        StoredServerProfile.saveCloudTokensIfConnected(
            context,
            accessToken = session.accessToken,
            refreshToken = session.refreshToken,
            expiresAtEpochMs = session.expiresAtEpochMs,
        )
    }

    override fun signOut() {
        StoredServerProfile.clearCloud(context)
    }
}
