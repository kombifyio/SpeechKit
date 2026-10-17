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
)

/** Where the independent Cloud session lives. */
interface CloudSessionStore {
    fun load(): CloudSession?

    /**
     * Replaces the tokens of the current session; never creates one. A store
     * whose session was disconnected meanwhile ignores the call, so a rotation
     * that lands late cannot bring a signed-out session back.
     */
    fun save(session: CloudSession)

    /** The session is gone: the next screen must offer Connect again. */
    fun signOut()
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
    @Volatile private var replaced: List<String> = emptyList()

    // Guarded by [mutex]: no refresh attempt before this time (after Unavailable).
    private var pausedUntilMs = 0L

    /**
     * The bearer to put on a request that was built with [current]. Refreshes
     * first when the stored session has less than [minRemainingMs] of life
     * left; returns [current] unchanged when it is not this device's Cloud
     * session or when the refresh could not run.
     *
     * Ordinary REST calls use the default skew. A request that mints a
     * hosted voice session passes [SESSION_MIN_REMAINING_MS]: the Gateway
     * reserves such a session only for the rest of the token's life.
     */
    suspend fun bearerFor(current: String?, minRemainingMs: Long = refreshSkewMs): String? {
        val token = current?.takeIf { it.isNotBlank() } ?: return current
        val session = ownSession() ?: return current
        if (token != session.accessToken) {
            return if (token in replaced) session.accessToken else current
        }
        if (!expiresWithin(session, minRemainingMs)) return current
        return refreshLocked(token) ?: current
    }

    /**
     * The server rejected [rejected]. Returns the replacement bearer, or null
     * when this is not the device's Cloud session or the session could not be
     * renewed (a definitive refusal also signs the session out).
     */
    suspend fun recoverRejected(rejected: String?): String? {
        val token = rejected?.takeIf { it.isNotBlank() } ?: return null
        val session = ownSession() ?: return null
        if (token != session.accessToken) {
            return if (token in replaced) session.accessToken else null
        }
        return refreshLocked(token)
    }

    private fun ownSession(): CloudSession? =
        store.load()?.takeIf { !it.refreshToken.isNullOrBlank() }

    private fun expiresWithin(session: CloudSession, windowMs: Long): Boolean {
        val expiry = session.expiresAtEpochMs.takeIf { it > 0L }
            ?: jwtExpiryEpochMs(session.accessToken)
            ?: return false
        return nowMs() >= expiry - windowMs
    }

    // Cancellation can only stop a caller waiting for the lock. Once the
    // exchange started, the token endpoint has rotated the refresh token and
    // the answer must reach the store, or the spent token is reused and the
    // user is signed out.
    private suspend fun refreshLocked(stale: String): String? = mutex.withLock {
        withContext(NonCancellable) { refreshExclusive(stale) }
    }

    private suspend fun refreshExclusive(stale: String): String? {
        val session = store.load() ?: return null
        if (session.accessToken != stale) {
            // Another caller refreshed while this one waited for the lock.
            return if (stale in replaced) session.accessToken else null
        }
        val refreshToken = session.refreshToken?.takeIf { it.isNotBlank() } ?: return null
        if (nowMs() < pausedUntilMs) return null
        return when (val outcome = refresher(refreshToken)) {
            is CloudRefresh.Refreshed -> {
                pausedUntilMs = 0L
                // A Disconnect or a new sign-in during the call wins.
                if (store.load()?.accessToken != stale) return null
                val tokens = outcome.tokens
                store.save(
                    CloudSession(
                        accessToken = tokens.accessToken,
                        refreshToken = tokens.refreshToken ?: refreshToken,
                        expiresAtEpochMs = nowMs() + tokens.expiresInSec * 1000L,
                    ),
                )
                replaced = (replaced + stale).takeLast(MAX_REPLACED)
                tokens.accessToken
            }

            CloudRefresh.Rejected -> {
                pausedUntilMs = 0L
                if (store.load()?.accessToken == stale) store.signOut()
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
         * Life a token must still have when it mints a hosted voice session
         * (Voice Agent, dictation stream): the Gateway ends the session when
         * the token that opened it expires.
         */
        const val SESSION_MIN_REMAINING_MS: Long = 10 * 60_000L

        /** How long refresh attempts are skipped after the token endpoint was unreachable. */
        const val UNAVAILABLE_BACKOFF_MS: Long = 30_000L

        private const val MAX_REPLACED = 8
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

    suspend fun fresh(current: String?): String? = lifecycle?.bearerFor(current) ?: current

    /** Like [fresh], for a request that mints a long-lived hosted voice session. */
    suspend fun freshForSession(current: String?): String? =
        lifecycle?.bearerFor(current, CloudTokenLifecycle.SESSION_MIN_REMAINING_MS) ?: current

    suspend fun recoverRejected(rejected: String?): String? = lifecycle?.recoverRejected(rejected)
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
