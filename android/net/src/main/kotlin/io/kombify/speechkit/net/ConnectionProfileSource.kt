package io.kombify.speechkit.net

import android.content.Context
import io.kombify.speechkit.domain.ConnectionMode
import io.kombify.speechkit.domain.ConnectionProfile
import io.kombify.speechkit.domain.inferConnectionMode
import io.kombify.speechkit.log.VoiceLog
import java.util.UUID

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
    const val KEY_CLOUD_OWNED = "cloud_owned"
    private const val KEY_CLOUD_GENERATION = "cloud_session_generation"

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

    fun cloudSelected(context: Context): Boolean = loadMode(context) == ConnectionMode.KOMBIFY_CLOUD

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

    fun loadCloud(context: Context): ConnectionProfile.Server? = synchronized(cloudLock) {
        val prefs = context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
        val url = prefs.getString(KEY_CLOUD_URL, null)?.trim().orEmpty()
        if (url.isEmpty()) return@synchronized null
        val token = prefs.getString(KEY_CLOUD_TOKEN, null)?.trim()?.ifEmpty { null }
        val owned = prefs.getBoolean(KEY_CLOUD_OWNED, false) ||
            !prefs.getString(KEY_CLOUD_REFRESH, null).isNullOrBlank()
        val generation = if (owned) cloudGeneration(prefs) else null
        ConnectionProfile.Server(url, token, generation)
    }

    // Legacy stored sessions receive one identity before their first profile is returned.
    private fun cloudGeneration(prefs: android.content.SharedPreferences): String {
        prefs.getString(KEY_CLOUD_GENERATION, null)?.takeIf { it.isNotBlank() }?.let { return it }
        val generation = UUID.randomUUID().toString()
        prefs.edit().putString(KEY_CLOUD_GENERATION, generation).apply()
        return generation
    }

    fun loadCloudRefreshToken(context: Context): String? =
        context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
            .getString(KEY_CLOUD_REFRESH, null)?.trim()?.ifEmpty { null }

    /** Ownership survives an access-only login; refresh presence is only a legacy migration hint. */
    fun hasOwnCloudSession(context: Context): Boolean = synchronized(cloudLock) {
        val prefs = context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
        !prefs.getString(KEY_CLOUD_URL, null).isNullOrBlank() &&
            (prefs.getBoolean(KEY_CLOUD_OWNED, false) ||
                !prefs.getString(KEY_CLOUD_REFRESH, null).isNullOrBlank())
    }

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
            .putBoolean(KEY_CLOUD_OWNED, true)
            .putString(KEY_CLOUD_GENERATION, UUID.randomUUID().toString())
            .putString(KEY_CONNECTION_MODE, ConnectionMode.KOMBIFY_CLOUD.wire)
            .apply()
        AccountVocabulary.syncInBackground(context)
    }

    /**
     * Updates an optional Companion session without replacing this device's
     * independently signed-in account.
     */
    fun replaceCloudAccess(
        context: Context,
        url: String,
        token: String,
        expiresAtEpochMs: Long = 0L,
    ) {
        saveCompanionCloud(context, url, token, expiresAtEpochMs)
    }

    /**
     * Companion may bootstrap an unowned session, but never mix its access
     * token with an independent login's refresh token, including when that
     * login finishes while provisioning is on the wire. Recovery also checks
     * the rejected token so a late reply cannot resurrect a disconnected or
     * newly connected session. Recovery updates cached credentials without
     * changing the selected execution mode.
     */
    fun saveCompanionCloud(
        context: Context,
        url: String,
        token: String,
        expiresAtEpochMs: Long = 0L,
        expectedAccessToken: String? = null,
    ): Boolean = synchronized(cloudLock) {
        val prefs = context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
        if (prefs.getBoolean(KEY_CLOUD_OWNED, false) ||
            !prefs.getString(KEY_CLOUD_REFRESH, null).isNullOrBlank()
        ) return@synchronized false
        if (expectedAccessToken != null &&
            (prefs.getString(KEY_CLOUD_URL, null).isNullOrBlank() ||
                prefs.getString(KEY_CLOUD_TOKEN, null) != expectedAccessToken)
        ) return@synchronized false
        val edit = prefs.edit()
            .putString(KEY_CLOUD_URL, url.trim())
            .putString(KEY_CLOUD_TOKEN, token.trim())
            .putLong(KEY_CLOUD_EXPIRES_AT, expiresAtEpochMs)
        if (expectedAccessToken == null) {
            edit.putString(KEY_CONNECTION_MODE, ConnectionMode.KOMBIFY_CLOUD.wire)
        }
        edit.commit()
    }

    internal fun loadCloudSession(context: Context): CloudSession? = synchronized(cloudLock) {
        val prefs = context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
        if (prefs.getString(KEY_CLOUD_URL, null).isNullOrBlank()) return@synchronized null
        val access = prefs.getString(KEY_CLOUD_TOKEN, null)?.trim()?.ifEmpty { null }
            ?: return@synchronized null
        CloudSession(
            accessToken = access,
            refreshToken = prefs.getString(KEY_CLOUD_REFRESH, null)?.trim()?.ifEmpty { null },
            expiresAtEpochMs = prefs.getLong(KEY_CLOUD_EXPIRES_AT, 0L),
            generation = if (hasOwnCloudSession(context)) cloudGeneration(prefs) else null,
        )
    }

    internal fun saveCloudTokensIfCurrent(
        context: Context,
        expected: CloudSession,
        replacement: CloudSession,
    ): Boolean = synchronized(cloudLock) {
        if (loadCloudSession(context) != expected) return@synchronized false
        // The old refresh token is spent, so wait for the rotated one to reach disk.
        val persisted = context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE).edit()
            .putString(KEY_CLOUD_TOKEN, replacement.accessToken.trim())
            .putString(KEY_CLOUD_REFRESH, replacement.refreshToken?.trim().orEmpty())
            .putLong(KEY_CLOUD_EXPIRES_AT, replacement.expiresAtEpochMs)
            .commit()
        // commit updates memory even when disk persistence fails. The CAS did
        // succeed in memory; do not misclassify the replacement as another login.
        if (!persisted) VoiceLog.w(VoiceLog.NET, "could not persist rotated Cloud credentials")
        true
    }

    internal fun clearCloudIfCurrent(context: Context, expected: CloudSession): Boolean = synchronized(cloudLock) {
        if (loadCloudSession(context) != expected) return@synchronized false
        clearCloud(context)
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
            .remove(KEY_CLOUD_OWNED)
            .remove(KEY_CLOUD_GENERATION)
            .apply()
        // Every sign-out passes here (Disconnect and a refused refresh token).
        AccountVocabulary.clear(context)
    }

    // One lock for every writer of the Cloud session keys.
    private val cloudLock = Any()
}

/** [CloudSessionStore] over the `speechkit_config` prefs the Connect flow writes. */
class StoredCloudSession(private val context: Context) : CloudSessionStore {
    override fun load(): CloudSession? = StoredServerProfile.loadCloudSession(context)

    override fun saveIfCurrent(expected: CloudSession, replacement: CloudSession): Boolean =
        StoredServerProfile.saveCloudTokensIfCurrent(context, expected, replacement)

    override fun signOutIfCurrent(expected: CloudSession): Boolean =
        StoredServerProfile.clearCloudIfCurrent(context, expected)
}
