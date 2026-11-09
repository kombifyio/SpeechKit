package io.kombify.speechkit.net

import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import java.util.Base64

/** The cloud session SpeechKit signed in to itself, as persisted on the device. */
data class CloudSession(
    val accessToken: String,
    val refreshToken: String?,
    /** Epoch milliseconds the access token stops being valid; 0 when unknown. */
    val expiresAtEpochMs: Long = 0L,
    val generation: String? = null,
)

/** Where the independent Cloud session lives. */
interface CloudSessionStore {
    fun load(): CloudSession?

    /** Atomically replaces the session only if its credentials still match. */
    fun saveIfCurrent(expected: CloudSession, replacement: CloudSession): Boolean

    /** Atomically signs out only the session whose credentials were rejected. */
    fun signOutIfCurrent(expected: CloudSession): Boolean
}

/** Outcome of exchanging a refresh token. */
sealed interface CloudRefresh {
    data class Refreshed(val tokens: KombifyCloudAuth.Tokens) : CloudRefresh

    /** The refresh token is dead (revoked, expired, rotated away, refused by policy): sign in again. */
    data object Rejected : CloudRefresh

    /** Network or server trouble: the session is intact, try again later. */
    data object Unavailable : CloudRefresh
}

/**
 * Keeps the stored Cloud bearer usable.
 *
 * The access token of the Gateway API lives about fifteen minutes. This rule
 * refreshes it shortly before it expires, persists the rotated refresh token
 * the token endpoint returns (an old one stops working), and signs the session
 * out when the refresh token itself is refused. Only a session this device
 * signed in to (one that has a refresh token) is touched: a bearer from
 * Companion, a tester build or a self-hosted server passes through unchanged.
 *
 * Calls are serialized, so a burst of requests rotates the refresh token once.
 * The exchange and the save that follows it run to the end even when the caller
 * is cancelled: the token endpoint has already spent the old refresh token by
 * then. After network or server trouble the lifecycle stays quiet for
 * [UNAVAILABLE_BACKOFF_MS] instead of stalling every request on a dead endpoint.
 */
class CloudTokenLifecycle(
    private val store: CloudSessionStore,
    private val refresher: suspend (refreshToken: String) -> CloudRefresh,
    private val nowMs: () -> Long = System::currentTimeMillis,
    private val refreshSkewMs: Long = DEFAULT_REFRESH_SKEW_MS,
) {
    private val mutex = Mutex()

    // Access tokens this lifecycle replaced. A request object built before the
    // rotation still carries one; it is answered with the current token
    // instead of the stale one.
    private var replaced: List<String> = emptyList()
    private var observedToken: String? = null
    private var retired: List<String> = emptyList()

    // Guarded by [mutex]: no refresh attempt before this time (after Unavailable).
    private var pausedUntilMs = 0L

    /**
     * The bearer to put on a request that was built with [current]. Refreshes
     * first when the stored session has less than [minRemainingMs] of life
     * left; returns [current] unchanged when it is not this device's Cloud
     * session or when the refresh could not run.
     *
     * Ordinary REST calls use the default skew. A request that mints a
     * hosted voice session passes [SESSION_MIN_REMAINING_MS] to avoid
     * starting with a bearer close to expiry.
     */
    suspend fun bearerFor(
        current: String?,
        minRemainingMs: Long = refreshSkewMs,
        generation: String? = null,
    ): String? {
        val token = current?.takeIf { it.isNotBlank() } ?: return current
        return mutex.withLock {
            if (generation != null) {
                val owned = store.load()?.takeIf { it.generation == generation } ?: return@withLock null
                if (owned.refreshToken.isNullOrBlank()) return@withLock owned.accessToken
                val session = observeSession()?.takeIf { it.generation == generation } ?: return@withLock null
                if (!expiresWithin(session, minRemainingMs)) return@withLock session.accessToken
                val refreshed = withContext(NonCancellable) { refreshExclusive(session) }
                return@withLock refreshed ?: store.load()?.takeIf { it.generation == generation }?.accessToken
            }
            val wasOwned = token == observedToken || token in replaced
            val session = observeSession()
            if (token in retired && token != session?.accessToken) return@withLock null
            if (session == null) return@withLock if (wasOwned) null else current
            if (token != session.accessToken && token !in replaced) {
                return@withLock if (wasOwned) null else current
            }
            val activeToken = session.accessToken
            if (!expiresWithin(session, minRemainingMs)) return@withLock activeToken
            val refreshed = withContext(NonCancellable) { refreshExclusive(session) }
            refreshed ?: observeSession()?.takeIf { it.accessToken == activeToken }?.accessToken
        }
    }

    /** Returns a replacement for a rejected bearer, only for the current device session. */
    suspend fun recoverRejected(rejected: String?, generation: String? = null): String? {
        val token = rejected?.takeIf { it.isNotBlank() } ?: return null
        return mutex.withLock {
            val session = observeSession() ?: return@withLock null
            if (generation != null && generation != session.generation) return@withLock null
            if (token != session.accessToken) {
                return@withLock if (token in replaced) session.accessToken else null
            }
            withContext(NonCancellable) { refreshExclusive(session) }
        }
    }

    // Every change not made by this lifecycle starts a new rotation history.
    // The lock serializes these observations with refresh and token replacement.
    private fun observeSession(): CloudSession? {
        val session = store.load()?.takeIf { !it.refreshToken.isNullOrBlank() }
        if (observedToken != session?.accessToken) {
            retired = (retired + replaced + listOfNotNull(observedToken)).takeLast(MAX_RETIRED)
            replaced = emptyList()
            pausedUntilMs = 0L
            observedToken = session?.accessToken
        }
        return session
    }

    private fun expiresWithin(session: CloudSession, windowMs: Long): Boolean {
        val expiry = session.expiresAtEpochMs.takeIf { it > 0L }
            ?: jwtExpiryEpochMs(session.accessToken)
            ?: return false
        return nowMs() >= expiry - windowMs
    }

    // Once the exchange starts, cancellation must not discard a rotated token.
    // The caller holds mutex and runs this part in NonCancellable.
    private suspend fun refreshExclusive(session: CloudSession): String? {
        val refreshToken = session.refreshToken?.takeIf { it.isNotBlank() } ?: return null
        if (nowMs() < pausedUntilMs) return null
        return when (val outcome = refresher(refreshToken)) {
            is CloudRefresh.Refreshed -> {
                pausedUntilMs = 0L
                val tokens = outcome.tokens
                val replacement = CloudSession(
                    accessToken = tokens.accessToken,
                    refreshToken = tokens.refreshToken ?: refreshToken,
                    expiresAtEpochMs = nowMs() + tokens.expiresInSec * 1000L,
                    generation = session.generation,
                )
                if (!store.saveIfCurrent(session, replacement)) return null
                replaced = (replaced + session.accessToken).takeLast(MAX_REPLACED)
                observedToken = replacement.accessToken
                replacement.accessToken
            }

            CloudRefresh.Rejected -> {
                pausedUntilMs = 0L
                store.signOutIfCurrent(session)
                null
            }

            CloudRefresh.Unavailable -> {
                pausedUntilMs = nowMs() + UNAVAILABLE_BACKOFF_MS
                null
            }
        }
    }

    companion object {
        const val DEFAULT_REFRESH_SKEW_MS: Long = 90_000L

        /**
         * Minimum remaining token life requested when opening a hosted voice
         * session (Voice Agent or dictation stream).
         */
        const val SESSION_MIN_REMAINING_MS: Long = 10 * 60_000L

        /** How long refresh attempts are skipped after the token endpoint was unreachable. */
        const val UNAVAILABLE_BACKOFF_MS: Long = 30_000L

        private const val MAX_REPLACED = 8
        private const val MAX_RETIRED = 32
    }
}

/**
 * Process-wide hook so every Cloud request path asks the same lifecycle. The
 * app installs it once at startup; without one (OSS builds, unit tests) tokens
 * pass through untouched.
 */
object CloudBearer {
    @Volatile private var lifecycle: CloudTokenLifecycle? = null

    fun install(lifecycle: CloudTokenLifecycle?) {
        this.lifecycle = lifecycle
    }

    suspend fun fresh(current: String?, generation: String? = null): String? =
        lifecycle.let { installed ->
            if (installed == null) current.takeIf { generation == null }
            else installed.bearerFor(current, generation = generation)
        }

    /** Like [fresh], for a request that mints a long-lived hosted voice session. */
    suspend fun freshForSession(current: String?, generation: String? = null): String? =
        lifecycle.let { installed ->
            if (installed == null) current.takeIf { generation == null }
            else installed.bearerFor(current, CloudTokenLifecycle.SESSION_MIN_REMAINING_MS, generation)
        }

    suspend fun recoverRejected(rejected: String?, generation: String? = null): String? =
        lifecycle?.recoverRejected(rejected, generation)

}

private val EXP_CLAIM = Regex("\"exp\"\\s*:\\s*(\\d+)")

/** The `exp` claim of a JWT access token in epoch milliseconds, or null when it is not readable. */
internal fun jwtExpiryEpochMs(jwt: String): Long? {
    val payload = jwt.split('.').getOrNull(1)?.takeIf { it.isNotEmpty() } ?: return null
    val json = runCatching { String(Base64.getUrlDecoder().decode(payload), Charsets.UTF_8) }
        .getOrNull() ?: return null
    val seconds = EXP_CLAIM.find(json)?.groupValues?.get(1)?.toLongOrNull() ?: return null
    return seconds * 1000L
}
