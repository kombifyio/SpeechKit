package io.kombify.speechkit.app.companion

import io.kombify.speechkit.domain.ConnectionProfile
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.util.concurrent.Executor
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicReference

class CompanionSessionCacheTest {

    @Test
    fun aPinnedExpiryRefreshesWhenThatTimeIsReached() {
        var now = 1_000L
        val cache = CompanionSessionCache(nowMs = { now }, ttlMs = 60_000)
        val session = ConnectionProfile.Server("https://api.kombify.io/v1/speechkit", "user-jwt")
        cache.offer(session, expiresAtEpochMs = 5_000)
        now = 4_999
        assertFalse(cache.needsRefresh())
        now = 5_000
        assertTrue(cache.needsRefresh())
        assertEquals(session, cache.current())
    }

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

    // Companion pins the SHA-256 of the certificate that signs this app and
    // throws SecurityException("coinstall_caller_rejected") for anything else.
    // Folding that into Unavailable is what made the failure unreadable: the
    // user got a vague "could not connect" and no way to know that the two
    // installed builds simply do not match.
    @Test
    fun aRefusedSignatureIsReportedAsRejectedNotAsUnavailable() {
        val provisioner = CompanionProvisioner(
            installed = { true },
            binder = { CompanionProvision.Rejected },
            executor = direct,
        )
        assertEquals(CompanionProvision.Rejected, provisioner.provisionNow())
    }

    // A rejection says nothing about the user's session, so a previously
    // provisioned one must survive it. Only an explicit Empty clears.
    @Test
    fun aRejectionDoesNotDiscardAnExistingSession() {
        val session = ConnectionProfile.Server("https://api.kombify.io/v1/speechkit", "user-jwt")
        val outcome = AtomicReference<CompanionProvision>(CompanionProvision.Session(session))
        val provisioner = CompanionProvisioner(
            installed = { true },
            binder = { outcome.get() },
            executor = direct,
        )
        provisioner.provisionNow()
        outcome.set(CompanionProvision.Rejected)
        provisioner.provisionNow()
        assertEquals(session, provisioner.currentSession())
    }

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
    fun aProvisionedExpiryIsHonouredInsteadOfTheLocalTtl() {
        var now = 0L
        val session = ConnectionProfile.Server("https://api.kombify.io/v1/speechkit", "user-jwt")
        val calls = AtomicInteger()
        val provisioner = CompanionProvisioner(
            installed = { true },
            binder = {
                calls.incrementAndGet()
                CompanionProvision.Session(session, expiresAtEpochMs = 50)
            },
            cache = CompanionSessionCache(nowMs = { now }, ttlMs = 10),
            executor = direct,
        )
        provisioner.provisionNow()
        now = 20
        assertEquals(session, provisioner.currentSession())
        assertEquals(1, calls.get())
    }

    @Test
    fun unauthorizedCloudCallReprovisionsAndDoesNotKeepTheDeadBearer() {
        val dead = ConnectionProfile.Server("https://api.kombify.io/v1/speechkit", "dead-jwt")
        val fresh = ConnectionProfile.Server("https://api.kombify.io/v1/speechkit", "fresh-jwt")
        val outcome = AtomicReference<CompanionProvision>(CompanionProvision.Session(dead))
        val provisioner = CompanionProvisioner(
            installed = { true },
            binder = { outcome.get() },
            executor = direct,
        )
        provisioner.provisionNow()
        outcome.set(CompanionProvision.Session(fresh))
        assertEquals(CompanionProvision.Session(fresh), provisioner.recoverFromUnauthorized(dead))
        assertEquals(fresh, provisioner.currentSession())
    }

    @Test
    fun unauthorizedCloudCallClearsWhenCompanionDoesNotReturnASession() {
        val dead = ConnectionProfile.Server("https://api.kombify.io/v1/speechkit", "dead-jwt")
        val outcome = AtomicReference<CompanionProvision>(CompanionProvision.Session(dead))
        val provisioner = CompanionProvisioner(
            installed = { true },
            binder = { outcome.get() },
            executor = direct,
        )
        provisioner.provisionNow()
        outcome.set(CompanionProvision.Empty)
        assertEquals(CompanionProvision.Empty, provisioner.recoverFromUnauthorized(dead))
        assertNull(provisioner.currentSession())

        outcome.set(CompanionProvision.Session(dead))
        provisioner.provisionNow()
        outcome.set(CompanionProvision.Rejected)
        assertEquals(CompanionProvision.Rejected, provisioner.recoverFromUnauthorized(dead))
        assertNull(provisioner.currentSession())

        outcome.set(CompanionProvision.Session(dead))
        provisioner.provisionNow()
        outcome.set(CompanionProvision.Unavailable)
        assertEquals(CompanionProvision.Unavailable, provisioner.recoverFromUnauthorized(dead))
        assertNull(provisioner.currentSession())
    }

    @Test
    fun aSelfHostUnauthorizedDoesNotDropTheCompanionSession() {
        val cloud = ConnectionProfile.Server("https://api.kombify.io/v1/speechkit", "user-jwt")
        val selfHost = ConnectionProfile.Server("http://192.168.1.20:8080", "sk-server-x")
        val provisioner = CompanionProvisioner(
            installed = { true },
            binder = { CompanionProvision.Session(cloud) },
            executor = direct,
        )
        provisioner.provisionNow()
        assertEquals(CompanionProvision.Unavailable, provisioner.recoverFromUnauthorized(selfHost))
        assertEquals(cloud, provisioner.currentSession())
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
