package io.kombify.speechkit.net

import io.kombify.speechkit.domain.ExecutionMode
import io.kombify.speechkit.domain.ExecutionPolicy
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock

import android.content.Context
import io.kombify.speechkit.domain.ConnectionMode
import io.kombify.speechkit.domain.ConnectionProfile
import io.kombify.speechkit.domain.inferConnectionMode
import io.kombify.speechkit.domain.storageScope
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
    const val KEY_SERVER_TOKEN_ID = "server_token_custody_id"
    const val KEY_CONNECTION_MODE = "connection_mode"
    const val KEY_CLOUD_URL = "cloud_server_url"
    const val KEY_CLOUD_TOKEN = "cloud_server_token"
    const val KEY_CLOUD_REFRESH = "cloud_refresh_token"
    const val KEY_CLOUD_EXPIRES_AT = "cloud_access_expires_at"
    const val KEY_CLOUD_OWNED = "cloud_owned"
    private const val KEY_CLOUD_GENERATION = "cloud_session_generation"
    private const val KEY_CLOUD_CUSTODY = "cloud_custody_id"

    fun load(context: Context): ConnectionProfile.Server? {
        val prefs = context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
        val url = prefs.getString(KEY_SERVER_URL, null)?.trim().orEmpty()
        if (url.isEmpty()) return null
        val id = prefs.getString(KEY_SERVER_TOKEN_ID, null)
        val recovered = if (id != null) runCatching { SecureProviderKeys(context.applicationContext).load(id) } else null
        val token = if (id != null) recovered?.getOrNull()
            else prefs.getString(KEY_SERVER_TOKEN, null)?.trim()?.ifEmpty { null }
        return ConnectionProfile.Server(url, token, credentialAvailable = recovered?.isSuccess != false)
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

    fun loadCloud(context: Context): ConnectionProfile.Server? = synchronized(cloudLock) {
        val prefs = context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
        val url = prefs.getString(KEY_CLOUD_URL, null)?.trim().orEmpty()
        if (url.isEmpty()) return@synchronized null
        val token = prefs.getString(KEY_CLOUD_TOKEN, null)?.trim()?.ifEmpty { null }
        val owned = prefs.getBoolean(KEY_CLOUD_OWNED, false) ||
            !prefs.getString(KEY_CLOUD_REFRESH, null).isNullOrBlank()
        val generation = if (owned) cloudGeneration(prefs) else null
        val custody = custodyId(prefs)
        ConnectionProfile.Server(url, token, generation, custody)
    }

    private fun custodyId(prefs: android.content.SharedPreferences): String {
        prefs.getString(KEY_CLOUD_CUSTODY, null)?.takeIf { it.isNotBlank() }?.let { return it }
        val id = UUID.randomUUID().toString()
        check(prefs.edit().putString(KEY_CLOUD_CUSTODY, id).commit()) { "Could not persist account custody" }
        return id
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
        if (loadCloud(context) != null) clearCloud(context)
        context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
            .edit()
            .putString(KEY_CLOUD_URL, url.trim())
            .putString(KEY_CLOUD_TOKEN, token?.trim().orEmpty())
            .putString(KEY_CLOUD_REFRESH, refreshToken?.trim().orEmpty())
            .putLong(KEY_CLOUD_EXPIRES_AT, expiresAtEpochMs)
            .putBoolean(KEY_CLOUD_OWNED, true)
            .putString(KEY_CLOUD_GENERATION, UUID.randomUUID().toString())
            .putString(KEY_CLOUD_CUSTODY, UUID.randomUUID().toString())
            .apply()
        AccountVocabulary.syncInBackground(context)
    }

    /** Called under the selection lock after all active network work has drained. */
    internal fun stageCloudSelection(
        context: Context,
        edit: android.content.SharedPreferences.Editor,
        url: String,
        token: String?,
        refreshToken: String?,
        expiresAtEpochMs: Long,
        owned: Boolean,
    ): Boolean = synchronized(cloudLock) {
        val prefs = context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
        if (!owned && (prefs.getBoolean(KEY_CLOUD_OWNED, false) ||
                !prefs.getString(KEY_CLOUD_REFRESH, null).isNullOrBlank())) return@synchronized false
        val previous = loadCloud(context)
        if (previous != null) clearCloud(context)
        edit.putString(KEY_CLOUD_URL, url.trim())
            .putString(KEY_CLOUD_TOKEN, token?.trim().orEmpty())
            .putString(KEY_CLOUD_REFRESH, refreshToken?.trim().orEmpty())
            .putLong(KEY_CLOUD_EXPIRES_AT, expiresAtEpochMs)
            .putBoolean(KEY_CLOUD_OWNED, owned)
            .putString(KEY_CLOUD_CUSTODY, UUID.randomUUID().toString())
        if (owned) edit.putString(KEY_CLOUD_GENERATION, UUID.randomUUID().toString())
        else edit.remove(KEY_CLOUD_GENERATION)
        true
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
        expectedCustodyId: String? = null,
    ): Boolean = synchronized(cloudLock) {
        val prefs = context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
        if (prefs.getBoolean(KEY_CLOUD_OWNED, false) ||
            !prefs.getString(KEY_CLOUD_REFRESH, null).isNullOrBlank()
        ) return@synchronized false
        if (expectedAccessToken != null &&
            (expectedCustodyId.isNullOrBlank() || prefs.getString(KEY_CLOUD_CUSTODY, null) != expectedCustodyId ||
                prefs.getString(KEY_CLOUD_URL, null).isNullOrBlank() ||
                prefs.getString(KEY_CLOUD_URL, null)?.trim()?.trimEnd('/') != url.trim().trimEnd('/') ||
                prefs.getString(KEY_CLOUD_TOKEN, null) != expectedAccessToken)
        ) return@synchronized false
        if (expectedAccessToken == null && !prefs.getString(KEY_CLOUD_URL, null).isNullOrBlank() &&
            (prefs.getString(KEY_CLOUD_TOKEN, null) != token.trim() || prefs.getString(KEY_CLOUD_URL, null) != url.trim())
        ) clearCloud(context)
        val edit = prefs.edit()
            .putString(KEY_CLOUD_URL, url.trim())
            .putString(KEY_CLOUD_TOKEN, token.trim())
            .putLong(KEY_CLOUD_EXPIRES_AT, expiresAtEpochMs)
        if (expectedAccessToken == null) {
            if (prefs.getString(KEY_CLOUD_URL, null).isNullOrBlank()) edit.putString(KEY_CLOUD_CUSTODY, UUID.randomUUID().toString())
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

    /** Execute a small local preference update only while this captured account remains current. */
    fun withActiveProfile(context: Context, profile: ConnectionProfile.Server, action: () -> Unit): Boolean = synchronized(cloudLock) {
        val current = if (profile.custodyId != null) loadCloud(context) else null
        if (profile.custodyId != null && (current?.custodyId != profile.custodyId || current?.normalizedBaseUrl != profile.normalizedBaseUrl)) return@synchronized false
        action()
        true
    }

    /** Atomic readback: never attach a recovered bearer to a different login. */
    fun currentCloudIfCaptured(context: Context, profile: ConnectionProfile.Server): ConnectionProfile.Server? = synchronized(cloudLock) {
        val current = loadCloud(context) ?: return@synchronized null
        if (profile.custodyId.isNullOrBlank() || current.custodyId != profile.custodyId ||
            current.cloudSessionGeneration != profile.cloudSessionGeneration || current.normalizedBaseUrl != profile.normalizedBaseUrl
        ) null else current.copy(selectionRevision = profile.selectionRevision)
    }

    fun registerActiveJob(context: Context, profile: ConnectionProfile.Server, job: kotlinx.coroutines.Job): Boolean =
        withActiveProfile(context, profile) { ScopedRemoteJobs.register(profile.storageScope(), job) }

    /** Signs the Cloud session out. The mode stays, so Settings offers Connect again. */
    fun clearCloud(context: Context) = synchronized(cloudLock) {
        val departing = loadCloud(context)
        if (departing != null) {
            val scope = departing.storageScope()
            ScopedRemoteJobs.cancel(scope)
            kotlinx.coroutines.runBlocking(kotlinx.coroutines.Dispatchers.IO) {
                val store = io.kombify.speechkit.store.RoomStore(context)
                try { store.revokeScope(scope) } finally { store.close() }
            }
            check(context.getSharedPreferences("speechkit_hybrid_drafts", Context.MODE_PRIVATE).edit().remove(scope).commit()) { "Could not clear account draft" }
            StoredVoiceAgentTarget(context).clear(departing)
        }
        context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
            .edit()
            .remove(KEY_CLOUD_URL)
            .remove(KEY_CLOUD_TOKEN)
            .remove(KEY_CLOUD_REFRESH)
            .remove(KEY_CLOUD_EXPIRES_AT)
            .remove(KEY_CLOUD_OWNED)
            .remove(KEY_CLOUD_GENERATION)
            .remove(KEY_CLOUD_CUSTODY)
            .commit()
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

/** One authority for route, destination permission and the captured-route generation. */
class StoredConnectionSelection(context: Context) {
    private val context = context.applicationContext
    private val prefs = this.context.getSharedPreferences(StoredServerProfile.PREFS_NAME, Context.MODE_PRIVATE)
    private val keys = SecureProviderKeys(this.context)

    data class Snapshot(val mode: ConnectionMode, val profile: ConnectionProfile, val policy: ExecutionPolicy, val revision: Long)

    fun revision(): Long = synchronized(stateLock) { prefs.getLong(REVISION, 0L) }

    fun isCurrent(profile: ConnectionProfile): Boolean {
        val captured = when (profile) {
            is ConnectionProfile.Server -> profile.selectionRevision
            is ConnectionProfile.Byok -> profile.selectionRevision
            is ConnectionProfile.Firebase -> profile.selectionRevision
            else -> null
        }
        if (profile is ConnectionProfile.Local || profile is ConnectionProfile.SystemOnDevice) {
            return snapshot().mode == ConnectionMode.ON_DEVICE
        }
        if (captured == null || captured != revision()) return false
        val mode = StoredServerProfile.loadMode(context)
            ?: inferConnectionMode(StoredServerProfile.load(context), null)
        return when (profile) {
            is ConnectionProfile.Byok -> mode == ConnectionMode.BYOK && prefs.getString(KEY_ID, null) == profile.credentialId
            is ConnectionProfile.Firebase -> firebaseEnabled && mode == ConnectionMode.FIREBASE && prefs.getString(FIREBASE_PROJECT, null) == profile.projectId
            is ConnectionProfile.Server -> mode in setOf(ConnectionMode.SELF_HOST, ConnectionMode.SPEECHKIT_ORIGIN, ConnectionMode.KOMBIFY_CLOUD)
            else -> false
        }
    }

    fun requireCurrent(profile: ConnectionProfile) {
        check(profile !is ConnectionProfile.Server || profile.credentialAvailable) {
            "The saved server credential is unavailable. Enter it again in Settings."
        }
        check(isCurrent(profile)) { "Connection changed. Start a new session using the current connection." }
    }

    /** Admit a captured route atomically with respect to selection commits. */
    fun <T> withCurrent(profile: ConnectionProfile, action: () -> T): T = synchronized(stateLock) {
        requireCurrent(profile)
        action()
    }

    fun snapshot(shipped: ConnectionProfile.Server? = null, companion: ConnectionProfile.Server? = null): Snapshot = synchronized(stateLock) {
        val selected = ConnectionMode.fromWire(prefs.getString(StoredServerProfile.KEY_CONNECTION_MODE, null))
        val stored = if (selected == null || selected == ConnectionMode.SELF_HOST ||
            (selected == ConnectionMode.SPEECHKIT_ORIGIN && shipped == null)) StoredServerProfile.load(context) else null
        // Shipped endpoints are available as an explicit choice, not a fresh-install default.
        val mode = selected ?: inferConnectionMode(stored, null)
        val revision = prefs.getLong(REVISION, 0L)
        val profile: ConnectionProfile = when (mode) {
            ConnectionMode.ON_DEVICE -> OnDeviceEngine.profile(context)
            ConnectionMode.SELF_HOST -> stored?.copy(selectionRevision = revision) ?: OnDeviceEngine.profile(context)
            ConnectionMode.SPEECHKIT_ORIGIN -> (shipped ?: stored)?.copy(selectionRevision = revision) ?: OnDeviceEngine.profile(context)
            ConnectionMode.KOMBIFY_CLOUD -> (StoredServerProfile.loadCloud(context) ?: companion)?.copy(selectionRevision = revision) ?: OnDeviceEngine.profile(context)
            ConnectionMode.BYOK -> {
                val id = prefs.getString(KEY_ID, null).orEmpty()
                val apiKey = runCatching { keys.load(id) }.getOrDefault("")
                ConnectionProfile.Byok(apiKey, prefs.getString(KEY_MODEL, null) ?: "gemini-3.8-live", prefs.getString(KEY_VOICE, null) ?: "Kore", id, selectionRevision = revision)
            }
            ConnectionMode.FIREBASE -> if (!firebaseEnabled) OnDeviceEngine.profile(context) else ConnectionProfile.Firebase(
                applicationId = prefs.getString(FIREBASE_APP, "").orEmpty(),
                projectId = prefs.getString(FIREBASE_PROJECT, "").orEmpty(),
                apiKey = prefs.getString(FIREBASE_CLIENT_KEY, "").orEmpty(),
                cloudModel = prefs.getString(FIREBASE_TEXT_MODEL, "gemini-3.5-flash-lite").orEmpty(),
                liveModel = prefs.getString(FIREBASE_LIVE_MODEL, "gemini-3.8-live").orEmpty(),
                voice = prefs.getString(FIREBASE_VOICE, "Kore").orEmpty(),
                debugAppCheck = prefs.getBoolean(FIREBASE_DEBUG, false),
                selectionRevision = revision,
            )
        }
        val remote = profile is ConnectionProfile.Server || profile is ConnectionProfile.Byok || profile is ConnectionProfile.Firebase
        val legacyFirebase = legacyFirebaseEnabled && selected == null && mode == ConnectionMode.ON_DEVICE
        val policy = if (legacyFirebase) persistedPolicy(profile)
            else if (!remote || mode == ConnectionMode.ON_DEVICE) ExecutionPolicy(ExecutionMode.DEVICE_ONLY) else persistedPolicy(profile)
        // A caller without the shipped/Companion route cannot infer the active
        // remote permission from its local placeholder. Do not close a guard
        // initialized by the host that did supply the route.
        if (remote || mode == ConnectionMode.ON_DEVICE || mode == ConnectionMode.FIREBASE) {
            NetworkUseGuard.initialize(policy.allowNetwork && policy.mode != ExecutionMode.DEVICE_ONLY)
        } else {
            NetworkUseGuard.initializeMissingRoute()
        }
        Snapshot(mode, profile, policy, revision)
    }

    internal fun policyFor(profile: ConnectionProfile): ExecutionPolicy {
        val server = profile as? ConnectionProfile.Server
        val mode = StoredServerProfile.loadMode(context)
        val current = snapshot(
            shipped = server.takeIf { mode == ConnectionMode.SPEECHKIT_ORIGIN },
            companion = server.takeIf { mode == ConnectionMode.KOMBIFY_CLOUD },
        )
        if (profile is ConnectionProfile.Server && profile.selectionRevision != null && !isCurrent(profile)) return ExecutionPolicy(ExecutionMode.DEVICE_ONLY)
        if (profile is ConnectionProfile.Byok && !isCurrent(profile)) return ExecutionPolicy(ExecutionMode.DEVICE_ONLY)
        if (profile is ConnectionProfile.Firebase && !isCurrent(profile)) return ExecutionPolicy(ExecutionMode.DEVICE_ONLY)
        return current.policy
    }

    private fun persistedPolicy(profile: ConnectionProfile): ExecutionPolicy {
        val current = prefs.getString(POLICY_MODE, null)
        if (current != null) {
            val mode = ExecutionMode.entries.firstOrNull { it.name == current } ?: return ExecutionPolicy(ExecutionMode.DEVICE_ONLY)
            return ExecutionPolicy(mode, prefs.getBoolean(POLICY_NETWORK, false))
        }
        // Migrate only a remote route, never a stale network preference into local mode.
        val legacy = context.getSharedPreferences("speechkit_execution_policy", Context.MODE_PRIVATE)
        val mode = ExecutionMode.entries.firstOrNull { it.name == legacy.getString("mode", null) }
        return if (mode != null) ExecutionPolicy(mode, legacy.getBoolean("allow_network", false)) else ExecutionPolicy.fromLegacy(profile)
    }

    suspend fun selectLocal() { change(ConnectionMode.ON_DEVICE, ExecutionPolicy(ExecutionMode.DEVICE_ONLY)) }

    suspend fun selectFirebase(profile: ConnectionProfile.Firebase, preference: ExecutionMode = ExecutionMode.PREFER_ENDPOINT) {
        check(firebaseEnabled) { "Firebase cloud is not enabled in this build" }
        require(profile.applicationId.isNotBlank() && profile.projectId.isNotBlank() && profile.apiKey.isNotBlank()) { "Firebase project is incomplete" }
        require(profile.cloudModel.isNotBlank() && profile.liveModel.isNotBlank() && profile.voice.isNotBlank()) { "Firebase model configuration is incomplete" }
        change(ConnectionMode.FIREBASE, remotePolicy(preference)) { edit ->
            edit.putString(FIREBASE_APP, profile.applicationId).putString(FIREBASE_PROJECT, profile.projectId)
                .putString(FIREBASE_CLIENT_KEY, profile.apiKey).putString(FIREBASE_TEXT_MODEL, profile.cloudModel)
                .putString(FIREBASE_LIVE_MODEL, profile.liveModel).putString(FIREBASE_VOICE, profile.voice)
                .putBoolean(FIREBASE_DEBUG, profile.debugAppCheck)
        }
    }

    suspend fun selectServer(url: String, token: String?, preference: ExecutionMode = ExecutionMode.PREFER_ENDPOINT) {
        validateServerUrl(url)
        val id = token?.trim()?.takeIf { it.isNotEmpty() }?.let { keys.save(it) }
        var oldId: String? = null
        try {
            change(ConnectionMode.SELF_HOST, remotePolicy(preference)) { edit ->
                oldId = prefs.getString(StoredServerProfile.KEY_SERVER_TOKEN_ID, null)
                edit.putString(StoredServerProfile.KEY_SERVER_URL, url.trim())
                    .remove(StoredServerProfile.KEY_SERVER_TOKEN)
                    .putString(StoredServerProfile.KEY_SERVER_TOKEN_ID, id)
            }
            keys.delete(oldId)
        } catch (error: Throwable) {
            if (prefs.getString(StoredServerProfile.KEY_SERVER_TOKEN_ID, null) != id) keys.delete(id)
            throw error
        }
    }

    suspend fun clearServer() {
        while (true) {
            val current = snapshot()
            val mode = if (current.mode == ConnectionMode.SELF_HOST) ConnectionMode.ON_DEVICE else current.mode
            val policy = if (mode == ConnectionMode.ON_DEVICE) ExecutionPolicy(ExecutionMode.DEVICE_ONLY) else current.policy
            var oldId: String? = null
            if (change(mode, policy, current.revision) { edit ->
                    oldId = prefs.getString(StoredServerProfile.KEY_SERVER_TOKEN_ID, null)
                    edit.remove(StoredServerProfile.KEY_SERVER_URL).remove(StoredServerProfile.KEY_SERVER_TOKEN)
                        .remove(StoredServerProfile.KEY_SERVER_TOKEN_ID)
                }) {
                keys.delete(oldId)
                return
            }
        }
    }

    suspend fun selectByok(apiKey: String, model: String = "gemini-3.8-live", voice: String = "Kore", preference: ExecutionMode = ExecutionMode.PREFER_ENDPOINT) {
        require(model.isNotBlank() && voice.isNotBlank()) { "Enter a Gemini model and voice" }
        val id = keys.save(apiKey)
        var oldId: String? = null
        try {
            change(ConnectionMode.BYOK, remotePolicy(preference)) { edit ->
                oldId = prefs.getString(KEY_ID, null)
                edit.putString(KEY_ID, id).putString(KEY_MODEL, model.trim()).putString(KEY_VOICE, voice.trim())
            }
            keys.delete(oldId)
        } catch (error: Throwable) {
            if (prefs.getString(KEY_ID, null) != id) keys.delete(id)
            throw error
        }
    }

    fun hasSavedByok(): Boolean {
        val id = prefs.getString(KEY_ID, null) ?: return false
        return runCatching { keys.load(id).isNotBlank() }.getOrDefault(false)
    }

    suspend fun restoreByok(preference: ExecutionMode = ExecutionMode.PREFER_ENDPOINT): Boolean {
        if (!hasSavedByok()) return false
        return change(ConnectionMode.BYOK, remotePolicy(preference))
    }

    suspend fun connectOwnedCloud(
        url: String,
        accessToken: String?,
        refreshToken: String? = null,
        expiresAtEpochMs: Long = 0L,
        expectedRevision: Long,
    ): Boolean = connectCloud(url, accessToken, refreshToken, expiresAtEpochMs, expectedRevision, true)

    suspend fun connectCompanionCloud(
        url: String,
        accessToken: String,
        expiresAtEpochMs: Long = 0L,
        expectedRevision: Long,
    ): Boolean = connectCloud(url, accessToken, null, expiresAtEpochMs, expectedRevision, false)

    private suspend fun connectCloud(
        url: String, token: String?, refreshToken: String?, expiresAtEpochMs: Long, expectedRevision: Long, owned: Boolean,
    ): Boolean {
        require(url.isNotBlank() && !token.isNullOrBlank()) { "Cloud account credentials are incomplete" }
        val result = try {
            change(ConnectionMode.KOMBIFY_CLOUD, remotePolicy(ExecutionMode.PREFER_ENDPOINT), expectedRevision) { edit ->
                if (!StoredServerProfile.stageCloudSelection(context, edit, url, token, refreshToken, expiresAtEpochMs, owned))
                    throw CloudSelectionRejected()
            }
        } catch (_: CloudSelectionRejected) { false }
        if (result) AccountVocabulary.syncInBackground(context)
        return result
    }

    private class CloudSelectionRejected : Exception()

    suspend fun selectCloud(expectedRevision: Long?, preference: ExecutionMode = ExecutionMode.PREFER_ENDPOINT): Boolean =
        selectMode(ConnectionMode.KOMBIFY_CLOUD, expectedRevision, preference)

    suspend fun selectMode(mode: ConnectionMode, expectedRevision: Long? = null, preference: ExecutionMode = ExecutionMode.PREFER_ENDPOINT): Boolean {
        require(mode != ConnectionMode.BYOK && mode != ConnectionMode.FIREBASE) { "Configure the provider connection first" }
        val policy = if (mode == ConnectionMode.ON_DEVICE) ExecutionPolicy(ExecutionMode.DEVICE_ONLY) else remotePolicy(preference)
        if (mode == ConnectionMode.KOMBIFY_CLOUD && StoredServerProfile.loadCloud(context)?.bearerToken.isNullOrBlank()) return false
        return try {
            change(mode, policy, expectedRevision = expectedRevision) {
                if (mode == ConnectionMode.KOMBIFY_CLOUD && StoredServerProfile.loadCloud(context)?.bearerToken.isNullOrBlank()) throw CloudSelectionRejected()
            }
        } catch (_: CloudSelectionRejected) { false }
    }

    suspend fun updatePreference(mode: ExecutionMode) {
        while (true) {
            val snapshot = snapshot()
            val policy = if (snapshot.mode == ConnectionMode.ON_DEVICE) ExecutionPolicy(ExecutionMode.DEVICE_ONLY) else remotePolicy(mode)
            if (change(snapshot.mode, policy, expectedRevision = snapshot.revision)) return
        }
    }

    internal suspend fun writePolicy(policy: ExecutionPolicy) {
        while (true) {
            val snapshot = snapshot()
            val legacyFirebase = legacyFirebaseEnabled && snapshot.mode == ConnectionMode.ON_DEVICE &&
                !prefs.contains(StoredServerProfile.KEY_CONNECTION_MODE)
            val effective = if (snapshot.mode == ConnectionMode.ON_DEVICE && !legacyFirebase) ExecutionPolicy(ExecutionMode.DEVICE_ONLY) else policy
            if (change(snapshot.mode, effective, expectedRevision = snapshot.revision, persistMode = !legacyFirebase)) return
        }
    }

    private suspend fun change(mode: ConnectionMode, policy: ExecutionPolicy, expectedRevision: Long? = null, persistMode: Boolean = true, extra: (android.content.SharedPreferences.Editor) -> Unit = {}): Boolean = switchLock.withLock {
        if (expectedRevision != null && expectedRevision != revision()) return@withLock false
        NetworkUseGuard.setAllowed(false)
        synchronized(stateLock) {
            val edit = prefs.edit()
            if (persistMode) edit.putString(StoredServerProfile.KEY_CONNECTION_MODE, mode.wire)
            edit.putLong(REVISION, prefs.getLong(REVISION, 0L) + 1)
                .putString(POLICY_MODE, policy.mode.name).putBoolean(POLICY_NETWORK, policy.allowNetwork)
            extra(edit)
            check(edit.commit()) { "Could not save connection selection" }
            if (policy.allowNetwork && policy.mode != ExecutionMode.DEVICE_ONLY) NetworkUseGuard.enable()
        }
        true
    }

    private fun validateServerUrl(value: String) {
        val uri = runCatching { java.net.URI(value.trim()) }.getOrNull()
        require(uri != null && uri.host != null && uri.userInfo == null && uri.rawQuery == null && uri.rawFragment == null &&
            (uri.scheme.equals("https", true) || uri.scheme.equals("http", true))) { "Enter a valid HTTP(S) SpeechKit endpoint without credentials, query or fragment" }
        if (uri.scheme.equals("http", true)) {
            val host = uri.host.lowercase().removePrefix("[").removeSuffix("]")
            val numeric = host.contains(':') || host.matches(Regex("[0-9.]+"))
            val address = if (numeric) runCatching { java.net.InetAddress.getByName(host) }.getOrNull() else null
            require(host == "localhost" || host.endsWith(".local") || address?.let { it.isLoopbackAddress || it.isSiteLocalAddress || it.isLinkLocalAddress } == true) {
                "Unencrypted HTTP endpoints must be on the local network"
            }
        }
    }

    private fun remotePolicy(preference: ExecutionMode): ExecutionPolicy = ExecutionPolicy(preference, preference != ExecutionMode.DEVICE_ONLY)

    companion object {
        @Volatile private var legacyFirebaseEnabled = false
        @Volatile private var firebaseEnabled = false

        /** Each host explicitly admits Firebase for its current build; stored settings cannot override it. */
        fun configureFirebase(enabled: Boolean) { firebaseEnabled = enabled }

        /** Enable only for the legacy hybrid feature when the connection setup flag is off. */
        fun configureLegacyFirebase(enabled: Boolean) { legacyFirebaseEnabled = enabled }

        private val stateLock = Any()
        private val switchLock = Mutex()
        private const val REVISION = "connection_selection_revision"
        private const val POLICY_MODE = "connection_execution_mode"
        private const val POLICY_NETWORK = "connection_allow_network"
        private const val KEY_ID = "gemini_key_id"
        private const val KEY_MODEL = "gemini_model"
        private const val KEY_VOICE = "gemini_voice"
        private const val FIREBASE_APP = "firebase_application_id"
        private const val FIREBASE_PROJECT = "firebase_project_id"
        private const val FIREBASE_CLIENT_KEY = "firebase_client_key"
        private const val FIREBASE_TEXT_MODEL = "firebase_text_model"
        private const val FIREBASE_LIVE_MODEL = "firebase_live_model"
        private const val FIREBASE_VOICE = "firebase_voice"
        private const val FIREBASE_DEBUG = "firebase_debug_app_check"
    }
}
