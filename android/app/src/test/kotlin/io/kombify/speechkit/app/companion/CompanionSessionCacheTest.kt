package io.kombify.speechkit.app.companion

import io.kombify.speechkit.domain.ConnectionProfile
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.util.concurrent.Executor
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicReference

class CompanionSessionCacheTest {

    @Test
    fun lastKnownSessionStaysWhileARefreshIsDue() {
        var now = 0L
        val cache = CompanionSessionCache(nowMs = { now }, ttlMs = 10)
        val session = ConnectionProfile.Server("https://api.kombify.io/v1/speechkit", "user-jwt")
        cache.offer(session)
        now = 11
        assertTrue(cache.needsRefresh())
        assertEquals(session, cache.current())
    }

    @Test
    fun aReachedCompanionWithNoUserClearsTheSession() {
        val cache = CompanionSessionCache()
        cache.offer(ConnectionProfile.Server("https://api.kombify.io/v1/speechkit", "user-jwt"))
        cache.clear()
        assertNull(cache.current())
        assertTrue(cache.needsRefresh())
    }
}

class CompanionProvisionerTest {

    private val direct = Executor { it.run() }

    @Test
    fun keyboardReadsTheLastSessionWithoutWaitingOnABind() {
        val session = ConnectionProfile.Server("https://api.kombify.io/v1/speechkit", "user-jwt")
        val provisioner = CompanionProvisioner(
            installed = { true },
            binder = { CompanionProvision.Session(session) },
            executor = direct,
        )
        provisioner.warm()
        assertEquals(session, provisioner.currentSession())
    }

    @Test
    fun aMissedBindKeepsThePreviousUserSession() {
        val session = ConnectionProfile.Server("https://api.kombify.io/v1/speechkit", "user-jwt")
        val outcome = AtomicReference<CompanionProvision>(CompanionProvision.Session(session))
        val calls = AtomicInteger()
        val provisioner = CompanionProvisioner(
            installed = { true },
            binder = {
                calls.incrementAndGet()
                outcome.get()
            },
            cache = CompanionSessionCache(ttlMs = 0),
            executor = direct,
        )
        provisioner.warm()
        outcome.set(CompanionProvision.Unavailable)
        assertEquals(session, provisioner.currentSession())
        assertTrue(calls.get() >= 2)
    }

    @Test
    fun explicitConnectStoresTheUserSessionImmediately() {
        val session = ConnectionProfile.Server("https://api.kombify.io/v1/speechkit", "user-jwt")
        val provisioner = CompanionProvisioner(
            installed = { true },
            binder = { CompanionProvision.Session(session) },
            executor = direct,
        )
        assertEquals(CompanionProvision.Session(session), provisioner.provisionNow())
        assertEquals(session, provisioner.currentSession())
    }

    @Test
    fun signOutDropsTheUserSessionSoTheTesterDefaultCanTakeOver() {
        val session = ConnectionProfile.Server("https://api.kombify.io/v1/speechkit", "user-jwt")
        val outcome = AtomicReference<CompanionProvision>(CompanionProvision.Session(session))
        val provisioner = CompanionProvisioner(
            installed = { true },
            binder = { outcome.get() },
            cache = CompanionSessionCache(ttlMs = 0),
            executor = direct,
        )
        provisioner.warm()
        outcome.set(CompanionProvision.Empty)
        assertNull(provisioner.currentSession())
    }
}
