package io.kombify.speechkit.app.companion

import io.kombify.speechkit.domain.ConnectionProfile
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNotEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class CloudConnectUiTest {

    @Test
    fun aSignedOutCompanionStaysOnTheSpeechKitScreen() {
        val ui = cloudConnectUi(CompanionProvision.Empty, companionInstalled = true)
        assertFalse(ui.openCompanion)
    }

    @Test
    fun aMissingCompanionIsTheOnlyOutcomeThatLeavesToInstall() {
        assertTrue(cloudConnectUi(CompanionProvision.Unavailable, companionInstalled = false).openCompanion)
        assertFalse(cloudConnectUi(CompanionProvision.Unavailable, companionInstalled = true).openCompanion)
        assertFalse(cloudConnectUi(CompanionProvision.Rejected, companionInstalled = true).openCompanion)
        assertFalse(
            cloudConnectUi(
                CompanionProvision.Session(ConnectionProfile.Server("https://api.kombify.io/v1/speechkit", "jwt")),
                companionInstalled = true,
            ).openCompanion,
        )
    }

    @Test
    fun aRejectedCallerIsNotReportedAsSignedOut() {
        val rejected = cloudConnectUi(CompanionProvision.Rejected, companionInstalled = true)
        val signedOut = cloudConnectUi(CompanionProvision.Empty, companionInstalled = true)
        assertNotEquals(rejected.messageRes, signedOut.messageRes)
    }
}
