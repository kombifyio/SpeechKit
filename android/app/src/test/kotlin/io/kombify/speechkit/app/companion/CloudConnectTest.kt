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
}
