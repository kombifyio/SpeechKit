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

    fun current(): ConnectionProfile.Server? = value

    fun needsRefresh(): Boolean {
        val snapshot = value
        return snapshot == null || nowMs() - atMs >= ttlMs
    }

    fun offer(session: ConnectionProfile.Server) {
        value = session
        atMs = nowMs()
    }

    fun clear() {
        value = null
        atMs = nowMs()
    }
}
