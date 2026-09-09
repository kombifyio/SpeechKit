package io.kombify.speechkit.app.companion

import io.kombify.speechkit.domain.ConnectionProfile

/**
 * Last Companion session the keyboard may use without waiting on a bind.
 *
 * A reached-but-signed-out Companion clears the cache. A bind that never
 * completed leaves the previous session in place.
 */
internal class CompanionSessionCache(
    private val nowMs: () -> Long = { System.currentTimeMillis() },
    private val ttlMs: Long = 60_000L,
) {
    @Volatile private var value: ConnectionProfile.Server? = null
    @Volatile private var atMs: Long = 0
    @Volatile private var expiresAtEpochMs: Long = 0

    fun current(): ConnectionProfile.Server? = value

    fun needsRefresh(): Boolean {
        if (value == null) return true
        val expiry = expiresAtEpochMs
        if (expiry > 0L) return nowMs() >= expiry
        return nowMs() - atMs >= ttlMs
    }

    fun offer(session: ConnectionProfile.Server, expiresAtEpochMs: Long = 0L) {
        value = session
        atMs = nowMs()
        this.expiresAtEpochMs = expiresAtEpochMs
    }

    fun clear() {
        value = null
        atMs = nowMs()
        expiresAtEpochMs = 0L
    }
}
