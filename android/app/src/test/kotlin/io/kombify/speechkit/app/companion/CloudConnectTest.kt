package io.kombify.speechkit.app.companion

import android.content.Context
import android.content.SharedPreferences
import io.kombify.speechkit.domain.ConnectionMode
import io.kombify.speechkit.domain.ConnectionProfile
import io.kombify.speechkit.net.StoredServerProfile
import io.mockk.every
import io.mockk.mockk
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class CloudConnectTest {

    private val session = CompanionProvision.Session(
        ConnectionProfile.Server("https://api.kombify.io/v1/speechkit", "user-jwt"),
    )

    @Test
    fun `a provisioned Companion session enters kombify cloud`() {
        assertEquals(ConnectionMode.KOMBIFY_CLOUD, cloudModeAfterProvision(session))
    }

    @Test
    fun `empty rejected and unavailable never enter cloud or fall back to a tester bearer`() {
        assertNull(cloudModeAfterProvision(CompanionProvision.Empty))
        assertNull(cloudModeAfterProvision(CompanionProvision.Rejected))
        assertNull(cloudModeAfterProvision(CompanionProvision.Unavailable))
    }

    @Test
    fun resumeDoesNotRestartConnectAfterCloudIsConnected() {
        assertEquals(
            false,
            shouldStartConnectOnResume(requested = true, alreadyConnected = true, connecting = false),
        )
        assertEquals(
            true,
            shouldStartConnectOnResume(requested = true, alreadyConnected = false, connecting = false),
        )
        assertEquals(
            false,
            shouldStartConnectOnResume(requested = true, alreadyConnected = false, connecting = true),
        )
    }

    @Test
    fun independentAccountRecoveryNeverSwitchesToACompanionAccount() {
        val own = ConnectionProfile.Server(session.profile.baseUrl, "own-account-renewed")
        for (renewed in listOf(own, null)) {
            assertEquals(
                renewed,
                recoverCloudSession(
                    hasOwnSession = true,
                    recoverOwn = { renewed },
                    recoverCompanion = { error("Must not switch to Companion's different account") },
                    saveCompanion = { error("Must not replace an independently owned session") },
                ),
            )
        }
    }

    @Test
    fun companionRecoveryStillWorksWithoutAnIndependentLogin() {
        var persisted: ConnectionProfile.Server? = null
        assertEquals(
            session.profile,
            recoverCloudSession(
                hasOwnSession = false,
                recoverOwn = { error("There is no independent login") },
                recoverCompanion = { session },
                saveCompanion = { persisted = it.profile; true },
            ),
        )
        assertEquals(session.profile, persisted)
    }

    @Test
    fun lateCompanionRecoveryCannotMixItsBearerWithANewIndependentRefreshToken() {
        val context = mockk<Context>()
        val prefs = mockk<SharedPreferences>()
        val editor = mockk<SharedPreferences.Editor>()
        val values = mutableMapOf(
            StoredServerProfile.KEY_CLOUD_URL to session.profile.baseUrl,
            StoredServerProfile.KEY_CLOUD_TOKEN to "rejected-companion-bearer",
        )
        every { context.getSharedPreferences(StoredServerProfile.PREFS_NAME, Context.MODE_PRIVATE) } returns prefs
        every { prefs.getString(any(), any()) } answers { values[firstArg()] ?: secondArg() }
        every { prefs.getBoolean(any(), any()) } answers { secondArg() }
        every { prefs.edit() } returns editor
        every { editor.putString(any(), any()) } answers { values[firstArg()] = secondArg(); editor }
        every { editor.putLong(any(), any()) } returns editor
        every { editor.commit() } returns true

        val recovered = recoverCloudSession(
            hasOwnSession = false,
            recoverOwn = { error("The independent login started after recovery") },
            recoverCompanion = {
                values[StoredServerProfile.KEY_CLOUD_TOKEN] = "new-independent-bearer"
                values[StoredServerProfile.KEY_CLOUD_REFRESH] = "new-independent-refresh"
                session
            },
            saveCompanion = {
                StoredServerProfile.saveCompanionCloud(
                    context,
                    it.profile.baseUrl,
                    it.profile.bearerToken.orEmpty(),
                    expectedAccessToken = "rejected-companion-bearer",
                )
            },
        )
        assertNull(recovered)
        assertEquals("new-independent-bearer", values[StoredServerProfile.KEY_CLOUD_TOKEN])
        assertEquals("new-independent-refresh", values[StoredServerProfile.KEY_CLOUD_REFRESH])
        assertFalse(
            StoredServerProfile.saveCompanionCloud(context, session.profile.baseUrl, "other-account-bearer"),
        )
        assertEquals("new-independent-bearer", values[StoredServerProfile.KEY_CLOUD_TOKEN])
    }

    @Test
    fun lateCompanionRecoveryPreservesTheSelectedSelfHostedMode() {
        val context = mockk<Context>()
        val prefs = mockk<SharedPreferences>()
        val editor = mockk<SharedPreferences.Editor>()
        val values = mutableMapOf(
            StoredServerProfile.KEY_CLOUD_URL to session.profile.baseUrl,
            StoredServerProfile.KEY_CLOUD_TOKEN to "rejected-companion-bearer",
            StoredServerProfile.KEY_CONNECTION_MODE to ConnectionMode.SELF_HOST.wire,
        )
        every { context.getSharedPreferences(StoredServerProfile.PREFS_NAME, Context.MODE_PRIVATE) } returns prefs
        every { prefs.getString(any(), any()) } answers { values[firstArg()] ?: secondArg() }
        every { prefs.getBoolean(any(), any()) } answers { secondArg() }
        every { prefs.edit() } returns editor
        every { editor.putString(any(), any()) } answers { values[firstArg()] = secondArg(); editor }
        every { editor.putLong(any(), any()) } returns editor
        every { editor.commit() } returns true

        assertTrue(
            StoredServerProfile.saveCompanionCloud(
                context,
                session.profile.baseUrl,
                "renewed-companion-bearer",
                expectedAccessToken = "rejected-companion-bearer",
            ),
        )
        assertEquals("renewed-companion-bearer", values[StoredServerProfile.KEY_CLOUD_TOKEN])
        assertEquals(ConnectionMode.SELF_HOST.wire, values[StoredServerProfile.KEY_CONNECTION_MODE])
    }

    @Test
    fun accessOnlyIndependentLoginCannotFallBackToAnotherCompanionAccount() {
        val context = mockk<Context>()
        val prefs = mockk<SharedPreferences>()
        val editor = mockk<SharedPreferences.Editor>()
        val values = mutableMapOf<String, Any?>()
        every { context.getSharedPreferences(StoredServerProfile.PREFS_NAME, Context.MODE_PRIVATE) } returns prefs
        every { prefs.getString(any(), any()) } answers { values[firstArg()] as? String ?: secondArg() }
        every { prefs.getBoolean(any(), any()) } answers { values[firstArg()] as? Boolean ?: secondArg() }
        every { prefs.edit() } returns editor
        every { editor.putString(any(), any()) } answers { values[firstArg()] = secondArg<String?>(); editor }
        every { editor.putBoolean(any(), any()) } answers { values[firstArg()] = secondArg<Boolean>(); editor }
        every { editor.putLong(any(), any()) } answers { values[firstArg()] = secondArg<Long>(); editor }
        every { editor.apply() } returns Unit
        every { editor.commit() } returns true

        StoredServerProfile.saveKombifyCloud(
            context,
            session.profile.baseUrl,
            "independent-access-only",
            refreshToken = null,
        )
        assertTrue(StoredServerProfile.hasOwnCloudSession(context))
        assertNull(
            recoverCloudSession(
                hasOwnSession = StoredServerProfile.hasOwnCloudSession(context),
                recoverOwn = { null },
                recoverCompanion = { error("An access-only login still owns its account") },
                saveCompanion = { error("Companion must not replace the independent account") },
            ),
        )
        assertFalse(
            StoredServerProfile.saveCompanionCloud(context, session.profile.baseUrl, "other-account-bearer"),
        )
        assertEquals("independent-access-only", values[StoredServerProfile.KEY_CLOUD_TOKEN])
    }
}
