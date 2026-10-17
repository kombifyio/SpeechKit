package io.kombify.speechkit.net

import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class CloudTokenLifecycleTest {

    private class MemoryStore(var session: CloudSession?) : CloudSessionStore {
        var signedOut = false
        override fun load() = session
        override fun save(session: CloudSession) {
            this.session = session
        }
        override fun signOut() {
            session = null
            signedOut = true
        }
    }

    private val now = 1_000_000L

    private fun expiring(refresh: String? = "old-rt") =
        CloudSession("old-at", refresh, expiresAtEpochMs = now + 30_000L)

    @Test
    fun `an expiring session is renewed once and keeps the rotated refresh token`() = runBlocking<Unit> {
        val store = MemoryStore(expiring())
        var calls = 0
        val lifecycle = CloudTokenLifecycle(
            store = store,
            refresher = { refreshToken ->
                calls++
                assertEquals("old-rt", refreshToken)
                CloudRefresh.Refreshed(KombifyCloudAuth.Tokens("new-at", "new-rt", 900))
            },
            nowMs = { now },
        )

        assertEquals("new-at", lifecycle.bearerFor("old-at"))
        assertEquals(CloudSession("new-at", "new-rt", now + 900_000L), store.session)

        // A request object built before the rotation still carries the old
        // token; it is answered with the new one, not a second refresh.
        assertEquals("new-at", lifecycle.bearerFor("old-at"))
        assertEquals("new-at", lifecycle.recoverRejected("old-at"))
        assertEquals(1, calls)
    }

    @Test
    fun `a caller cancelled after the server rotated the refresh token still persists the rotation`() =
        runBlocking<Unit> {
            val store = MemoryStore(expiring())
            // The identity provider accepts each refresh token once.
            var serverRefreshToken = "old-rt"
            val rotated = CompletableDeferred<Unit>()
            val replyArrives = CompletableDeferred<Unit>()
            val lifecycle = CloudTokenLifecycle(
                store = store,
                refresher = { refreshToken ->
                    if (refreshToken != serverRefreshToken) {
                        CloudRefresh.Rejected
                    } else {
                        serverRefreshToken = "new-rt"
                        rotated.complete(Unit)
                        replyArrives.await()
                        CloudRefresh.Refreshed(KombifyCloudAuth.Tokens("new-at", "new-rt", 900))
                    }
                },
                nowMs = { now },
            )

            // The keyboard closes while the exchange is on the wire.
            val caller = launch { lifecycle.bearerFor("old-at") }
            rotated.await()
            caller.cancel()
            replyArrives.complete(Unit)
            caller.join()

            assertEquals(CloudSession("new-at", "new-rt", now + 900_000L), store.session)
            assertFalse(store.signedOut)
            // The next caller keeps working with the rotated session.
            assertEquals("new-at", lifecycle.bearerFor("old-at"))
        }

    @Test
    fun `a refused refresh token signs the session out`() = runBlocking<Unit> {
        val store = MemoryStore(expiring())
        val lifecycle = CloudTokenLifecycle(
            store = store,
            refresher = { CloudRefresh.Rejected },
            nowMs = { now },
        )

        assertEquals("old-at", lifecycle.bearerFor("old-at"))
        assertTrue(store.signedOut)
        assertNull(store.session)
    }

    @Test
    fun `a voice session mint renews a token with less than ten minutes left`() = runBlocking<Unit> {
        // Five minutes left: fine for a REST call, too short to open a hosted
        // voice session that the Gateway would cut when the token expires.
        val store = MemoryStore(CloudSession("old-at", "old-rt", expiresAtEpochMs = now + 5 * 60_000L))
        var calls = 0
        val lifecycle = CloudTokenLifecycle(
            store = store,
            refresher = {
                calls++
                CloudRefresh.Refreshed(KombifyCloudAuth.Tokens("new-at", "new-rt", 900))
            },
            nowMs = { now },
        )

        assertEquals("old-at", lifecycle.bearerFor("old-at"))
        assertEquals(0, calls)

        assertEquals(
            "new-at",
            lifecycle.bearerFor("old-at", CloudTokenLifecycle.SESSION_MIN_REMAINING_MS),
        )
        assertEquals(1, calls)
    }

    @Test
    fun `network trouble keeps the session, pauses refreshing and other bearers are never refreshed`() =
        runBlocking<Unit> {
            val store = MemoryStore(expiring())
            var calls = 0
            var clock = now
            val lifecycle = CloudTokenLifecycle(
                store = store,
                refresher = {
                    calls++
                    CloudRefresh.Unavailable
                },
                nowMs = { clock },
            )

            assertEquals("old-at", lifecycle.bearerFor("old-at"))
            assertEquals(1, calls)

            // An outage does not stall every request behind another round trip.
            assertNull(lifecycle.recoverRejected("old-at"))
            assertEquals("old-at", lifecycle.bearerFor("old-at"))
            assertEquals(1, calls)

            clock += CloudTokenLifecycle.UNAVAILABLE_BACKOFF_MS
            assertNull(lifecycle.recoverRejected("old-at"))
            assertEquals(2, calls)
            assertFalse(store.signedOut)
            assertEquals(expiring(), store.session)

            // A self-hosted or Companion bearer is not this device's session.
            assertEquals("sk-server-x", lifecycle.bearerFor("sk-server-x"))
            assertNull(lifecycle.recoverRejected("sk-server-x"))
            assertEquals(2, calls)
        }
}
