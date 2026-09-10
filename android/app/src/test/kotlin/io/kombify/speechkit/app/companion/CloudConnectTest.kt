package io.kombify.speechkit.app.companion

import io.kombify.speechkit.domain.ConnectionMode
import io.kombify.speechkit.domain.ConnectionProfile
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNull
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
    fun aCloudUnauthorizedRefreshesTheStoredGatewayBearer() {
        val dead = ConnectionProfile.Server("https://api.kombify.io/v1/speechkit", "dead-jwt")
        val recovered = recoverIndependentCloud(
            failed = dead,
            stored = dead,
            refreshToken = "rt",
            refresh = { "fresh-jwt" },
        )
        assertEquals(
            ConnectionProfile.Server("https://api.kombify.io/v1/speechkit", "fresh-jwt"),
            recovered,
        )
    }

    @Test
    fun aSelfHostUnauthorizedDoesNotUseTheCloudRefreshPath() {
        val selfHost = ConnectionProfile.Server("http://192.168.1.20:8080", "sk-server-x")
        val cloud = ConnectionProfile.Server("https://api.kombify.io/v1/speechkit", "user-jwt")
        assertNull(
            recoverIndependentCloud(
                failed = selfHost,
                stored = cloud,
                refreshToken = "rt",
                refresh = { "fresh-jwt" },
            ),
        )
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
}
